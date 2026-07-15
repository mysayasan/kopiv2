package main

// A simulated site is more than one device on the Modbus network, addressed by UNIT ID:
//
//	unit 1 — the hybrid inverter (SunSpec chain: Common+103+123+124+203)   -> plant.go
//	unit 2 — a standalone SunSpec revenue meter (SunSpec chain: Common+203)
//	unit 3 — a NON-SunSpec vendor inverter (a raw, hand-documented register map, no "SunS")
//
// That mix is the point: the myiotsan solar "workspace" must bind devices that speak DIFFERENT
// register conventions into one logical system. Units 1 and 2 exercise the driver's SunSpec
// auto-discovery path; unit 3 exercises the manual register-map path a non-compliant device needs.
// Both normalise to the same telemetry, which is exactly what the workspace composes.

import (
	"fmt"
	"math"
)

// Device is one Modbus unit the server can route to.
type Device interface {
	unit() byte
	bank() *Bank
	update(dt float64)
	status() string
	label() string
	describe(addr int) string // human name for a written register, or "" if not a control
}

// --- unit 2: standalone SunSpec meter -------------------------------------------------------

// MeterDevice is a revenue/grid meter that exposes only Common + model 203. It has no PV or battery
// of its own; it simply reports the site's net grid flow, swinging between import and export over
// the day. It proves the driver walks a DIFFERENT SunSpec chain (fewer models) on a second unit.
type MeterDevice struct {
	unitID       byte
	b            *Bank
	common, mtr  *Model
	vnom, fnom   float64
	tod          float64
	whImp, whExp float64
	lastW        float64
}

func buildMeter(unit byte, base int, serial string) *MeterDevice {
	b := newBank(base + 200)
	b.set(base, sunsHi)
	b.set(base+1, sunsLo)
	cur := base + 2
	common, cur := place(b, cur, 1, commonPoints())
	mtr, cur := place(b, cur, 203, meterPoints())
	b.set(cur, endID)
	b.set(cur+1, 0)

	d := &MeterDevice{unitID: unit, b: b, common: common, mtr: mtr, vnom: 230, fnom: 50, tod: 6.0}
	common.setStr(b, "Mn", "SunSpec Sim")
	common.setStr(b, "Md", "GridMeter-3P")
	common.setStr(b, "Vr", "1.0.0")
	common.setStr(b, "SN", serial)
	common.setU16(b, "DA", uint16(unit))
	mtr.setSF(b, "A_SF", sfCurrent)
	mtr.setSF(b, "V_SF", sfVolt)
	mtr.setSF(b, "W_SF", sfPower)
	mtr.setSF(b, "Hz_SF", sfFreq)
	mtr.setSF(b, "VA_SF", sfPower)
	mtr.setSF(b, "VAR_SF", sfPower)
	mtr.setSF(b, "PF_SF", sfPF)
	mtr.setSF(b, "TotWh_SF", sfEnergy)
	return d
}

func (d *MeterDevice) unit() byte  { return d.unitID }
func (d *MeterDevice) bank() *Bank { return d.b }
func (d *MeterDevice) label() string {
	return fmt.Sprintf("unit %d  standalone SunSpec meter", d.unitID)
}
func (d *MeterDevice) describe(addr int) string { return "" } // read-only device

func (d *MeterDevice) update(dt float64) {
	d.b.mu.Lock()
	defer d.b.mu.Unlock()
	d.tod = math.Mod(d.tod+dt, 24)

	// Net grid: a base draw plus an evening peak, minus a midday solar dip that pushes to export.
	base := 1200 + 900*gauss(d.tod, 19.5, 2.0)
	solar := 3500 * math.Max(0, math.Sin(math.Pi*(d.tod-6)/12))
	w := base - solar + noise(40) // + import, - export
	d.lastW = w
	d.whImp += math.Max(0, w) * dt
	d.whExp += math.Max(0, -w) * dt

	v, f := d.vnom+noise(0.4), d.fnom+noise(0.02)
	mag := math.Abs(w)
	d.mtr.setScaledI16(d.b, "W", w, sfPower)
	d.mtr.setScaledI16(d.b, "VA", mag, sfPower)
	d.mtr.setScaledI16(d.b, "PF", 1.0, sfPF)
	d.mtr.setScaledU16(d.b, "Hz", f, sfFreq)
	d.mtr.setScaledU16(d.b, "A", mag/math.Max(v*3, 1), sfCurrent)
	d.mtr.setScaledU16(d.b, "PhV", v, sfVolt)
	d.mtr.setScaledU32(d.b, "TotWhImp", d.whImp, sfEnergy)
	d.mtr.setScaledU32(d.b, "TotWhExp", d.whExp, sfEnergy)
}

func (d *MeterDevice) status() string {
	return fmt.Sprintf("%02d:%02d  Grid %+6.0fW  (imp %.2fkWh / exp %.2fkWh)",
		int(d.tod), int(math.Mod(d.tod*60, 60)), d.lastW, d.whImp/1000, d.whExp/1000)
}

// --- unit 3: non-SunSpec vendor inverter ----------------------------------------------------

// VendorInverter is a small string inverter that does NOT follow SunSpec: no "SunS" marker, no
// model chain — just a flat block of holding registers at fixed addresses, each with a vendor scale
// factor a reader must be TOLD about (there is nothing self-describing to discover). This is the
// common reality of cheaper Chinese hybrids, and the case the driver's manual register-map path
// exists for. The map below is the contract; the driver test binds to the SAME addresses.
//
//	reg  0  Status        u16   0=waiting 1=normal 3=fault
//	reg  1  Vpv1          u16   0.1 V
//	reg  2  Ipv1          u16   0.1 A
//	reg  3  Ppv           u32   0.1 W    (hi word first)
//	reg  5  Vac           u16   0.1 V
//	reg  6  Fac           u16   0.01 Hz
//	reg  7  Pac           u32   0.1 W    (hi word first)
//	reg  9  Etoday        u16   0.1 kWh
//	reg 10  Etotal        u32   0.1 kWh  (hi word first)
//	reg 12  Temp          u16   0.1 C
//	reg 13  BatSoc        u16   1 %
//	reg 14  BatPower      i16   1 W      (+ charging, - discharging)
type VendorInverter struct {
	unitID    byte
	b         *Bank
	pvPeak    float64
	tod       float64
	etodayWh  float64
	etotalWh  float64
	lastPac   float64
	lastSoc   float64
}

// Vendor register offsets (holding registers, base 0).
const (
	vStatus   = 0
	vVpv1     = 1
	vIpv1     = 2
	vPpv      = 3 // u32
	vVac      = 5
	vFac      = 6
	vPac      = 7 // u32
	vEtoday   = 9
	vEtotal   = 10 // u32
	vTemp     = 12
	vBatSoc   = 13
	vBatPower = 14
)

func buildVendor(unit byte, pvPeak float64) *VendorInverter {
	return &VendorInverter{unitID: unit, b: newBank(32), pvPeak: pvPeak, tod: 6.0, lastSoc: 40}
}

func (d *VendorInverter) unit() byte           { return d.unitID }
func (d *VendorInverter) bank() *Bank          { return d.b }
func (d *VendorInverter) label() string        { return fmt.Sprintf("unit %d  vendor inverter (non-SunSpec register map)", d.unitID) }
func (d *VendorInverter) describe(addr int) string { return "" }

func (d *VendorInverter) update(dt float64) {
	d.b.mu.Lock()
	defer d.b.mu.Unlock()
	d.tod = math.Mod(d.tod+dt, 24)

	pv := d.pvPeak * math.Max(0, math.Sin(math.Pi*(d.tod-6)/12))
	pac := pv * 0.96
	d.lastPac = pac
	d.etodayWh += pac * dt
	d.etotalWh += pac * dt
	// A cheap battery that just drifts with production, for variety.
	d.lastSoc = clampF(d.lastSoc+(pv/d.pvPeak-0.4)*dt*15, 5, 100)

	status := uint16(1)
	if pv < 5 {
		status = 0 // waiting (dark)
	}
	set16 := func(addr int, actual float64, scale float64) {
		d.b.set(addr, uint16(clampU16(int64(actual/scale+0.5))))
	}
	set32 := func(addr int, actual float64, scale float64) {
		v := uint32(math.Max(0, actual/scale+0.5))
		d.b.set(addr, uint16(v>>16))
		d.b.set(addr+1, uint16(v&0xFFFF))
	}
	d.b.set(vStatus, status)
	set16(vVpv1, 360+noise(6), 0.1)
	set16(vIpv1, pv/360, 0.1)
	set32(vPpv, pv, 0.1)
	set16(vVac, 230+noise(0.5), 0.1)
	set16(vFac, 50+noise(0.02), 0.01)
	set32(vPac, pac, 0.1)
	set16(vEtoday, d.etodayWh/1000, 0.1) // kWh at 0.1
	set32(vEtotal, d.etotalWh/1000, 0.1)
	set16(vTemp, 30+pac/d.pvPeak*20+noise(0.5), 0.1)
	d.b.set(vBatSoc, uint16(d.lastSoc+0.5))
	d.b.set(vBatPower, uint16(int16(noise(200))))
}

func (d *VendorInverter) status() string {
	return fmt.Sprintf("%02d:%02d  Pac %5.0fW  SoC %4.1f%%  (etoday %.2fkWh)",
		int(d.tod), int(math.Mod(d.tod*60, 60)), d.lastPac, d.lastSoc, d.etodayWh/1000)
}

// --- unit 4: Huawei SUN2000 (vendor register map — the world's most-installed inverter) ----------
//
// The SUN2000's inverter telemetry sits in a SunSpec-ish block around register 32000, but its LUNA
// battery (SOC, charge/discharge power) and its built-in power meter live in Huawei's OWN registers
// up around 37000 — far from the inverter block and from each other. That spread is the whole point
// of this persona: it forces the driver's register map to read in CLUSTERS (one bounded request per
// block) instead of one impossible 5,700-register span. Addresses and scales are Huawei's published
// "Solar Inverter Modbus Interface Definitions"; the huawei-sun2000 builtin profile binds the SAME
// addresses, so myiotsan reads this device end to end without hardware.
//
//	32016 PV1 voltage   i16 0.1 V     32080 active power    i32 1 W
//	32017 PV1 current   i16 0.01 A    32085 grid frequency  u16 0.01 Hz
//	32018 PV2 voltage   i16 0.1 V     32087 temperature     i16 0.1 C
//	32019 PV2 current   i16 0.01 A    32089 device status   u16 enum
//	32064 PV input pwr  i32 1 W       32106 lifetime yield  u32 0.01 kWh
//	32069 phase A volts u16 0.1 V     37760 battery SOC     u16 0.1 %
//	                                  37765 battery power   i32 1 W (+charge/-discharge)
//	                                  37113 meter power     i32 1 W (+import/-export)
type HuaweiInverter struct {
	unitID   byte
	b        *Bank
	pvPeak   float64
	loadBase float64
	battWh   float64
	tod      float64
	soc      float64
	yieldWh  float64
	lastPac  float64
	lastGrid float64
	lastBatt float64
}

// Huawei register addresses (holding registers, absolute).
const (
	hPV1V   = 32016
	hPV1I   = 32017
	hPV2V   = 32018
	hPV2I   = 32019
	hPin    = 32064 // i32 PV input power
	hPhVA   = 32069
	hPac    = 32080 // i32 active power
	hFac    = 32085
	hTemp   = 32087
	hStatus = 32089
	hYield  = 32106 // u32 lifetime yield
	hMeterP = 37113 // i32 grid meter power (+import/-export)
	hSoc    = 37760
	hBattP  = 37765 // i32 battery power (+charge/-discharge)
	hTopReg = hBattP + 1
)

func buildHuawei(unit byte, pvPeak, loadBase, battWh, initSoC float64) *HuaweiInverter {
	return &HuaweiInverter{
		unitID: unit, b: newBank(hTopReg), pvPeak: pvPeak, loadBase: loadBase,
		battWh: battWh, tod: 6.0, soc: initSoC,
	}
}

func (d *HuaweiInverter) unit() byte  { return d.unitID }
func (d *HuaweiInverter) bank() *Bank { return d.b }
func (d *HuaweiInverter) label() string {
	return fmt.Sprintf("unit %d  Huawei SUN2000 (vendor register map, spread inverter/battery/meter blocks)", d.unitID)
}
func (d *HuaweiInverter) describe(addr int) string { return "" } // read-only in this sim

// setU16/setI16 write a scaled value into one register; setU32/setI32 write hi-word-first (Huawei's
// order), the i32 form carrying the two's-complement negative that a battery discharge or a grid
// export produces.
func (d *HuaweiInverter) setU16(addr int, actual, scale float64) {
	d.b.set(addr, uint16(clampU16(roundToI64(actual/scale))))
}
func (d *HuaweiInverter) setI16(addr int, actual, scale float64) {
	d.b.set(addr, uint16(int16(clampI16(roundToI64(actual/scale)))))
}
func (d *HuaweiInverter) setU32(addr int, actual, scale float64) {
	v := uint32(math.Max(0, actual/scale+0.5))
	d.b.set(addr, uint16(v>>16))
	d.b.set(addr+1, uint16(v&0xFFFF))
}
func (d *HuaweiInverter) setI32(addr int, actual, scale float64) {
	v := uint32(int32(roundToI64(actual / scale)))
	d.b.set(addr, uint16(v>>16))
	d.b.set(addr+1, uint16(v&0xFFFF))
}

func (d *HuaweiInverter) update(dt float64) {
	d.b.mu.Lock()
	defer d.b.mu.Unlock()
	d.tod = math.Mod(d.tod+dt, 24)

	pv := d.pvPeak * math.Max(0, math.Sin(math.Pi*(d.tod-6)/12)) // bell curve, dark before 6 / after 18
	pac := pv * 0.98
	load := d.loadBase + 900*gauss(d.tod, 19.5, 2.0) // evening load peak

	// Battery: charge on surplus, discharge to cover a deficit, within SOC bounds. + is charging.
	surplus := pac - load
	batt := 0.0
	switch {
	case surplus > 0 && d.soc < 100:
		batt = math.Min(surplus, d.pvPeak*0.5)
	case surplus < 0 && d.soc > 15:
		batt = math.Max(surplus, -d.pvPeak*0.5)
	}
	d.soc = clampF(d.soc+batt*dt/d.battWh*100, 5, 100)
	grid := load - pac + batt // + import, - export

	d.yieldWh += pac * dt
	d.lastPac, d.lastGrid, d.lastBatt = pac, grid, batt

	status := uint16(512) // Huawei: on-grid
	if pv < 5 {
		status = 0 // standby (dark)
	}
	perStringI := pv / 720 // two ~360 V strings

	d.setI16(hPV1V, 360+noise(6), 0.1)
	d.setI16(hPV1I, perStringI, 0.01)
	d.setI16(hPV2V, 360+noise(6), 0.1)
	d.setI16(hPV2I, perStringI, 0.01)
	d.setI32(hPin, pv, 1)
	d.setU16(hPhVA, 230+noise(0.5), 0.1)
	d.setI32(hPac, pac, 1)
	d.setU16(hFac, 50+noise(0.02), 0.01)
	d.setI16(hTemp, 30+pac/d.pvPeak*25+noise(0.5), 0.1)
	d.b.set(hStatus, status)
	d.setU32(hYield, d.yieldWh/1000, 0.01) // kWh at 0.01
	d.setU16(hSoc, d.soc, 0.1)
	d.setI32(hBattP, batt, 1)
	d.setI32(hMeterP, grid, 1)
}

func (d *HuaweiInverter) status() string {
	return fmt.Sprintf("%02d:%02d  Pac %5.0fW  SoC %4.1f%%  Grid %+6.0fW  Batt %+6.0fW",
		int(d.tod), int(math.Mod(d.tod*60, 60)), d.lastPac, d.soc, d.lastGrid, d.lastBatt)
}

// roundToI64 rounds to nearest, away from zero, so a scaled negative (export/discharge) does not
// truncate toward zero and lose a watt of magnitude.
func roundToI64(f float64) int64 { return int64(math.Round(f)) }
