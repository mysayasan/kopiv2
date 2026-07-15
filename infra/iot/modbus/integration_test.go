package modbus

import (
	"os"
	"testing"
	"time"

	"github.com/mysayasan/kopiv2/infra/iot/codec"
)

// TestLiveSimulator drives the driver against a running tools/sunspec-sim over real Modbus TCP. It
// is skipped unless MODBUS_SIM_ADDR is set, so the unit tests stay hermetic:
//
//	go run ./tools/sunspec-sim &                       # unit1 SunSpec hybrid, unit2 meter, unit3 vendor
//	MODBUS_SIM_ADDR=127.0.0.1:1502 go test ./infra/iot/modbus/ -run Live -v
//
// It proves the three "mixed protocol" cases the workspace must compose: a SunSpec hybrid inverter,
// a standalone SunSpec meter on a second unit, and a NON-SunSpec vendor inverter read by a manual
// register map on a third — all normalising to the same codec.Sample stream. And it exercises the
// guarded write-with-read-back on the curtailment control.
func TestLiveSimulator(t *testing.T) {
	addr := os.Getenv("MODBUS_SIM_ADDR")
	if addr == "" {
		t.Skip("set MODBUS_SIM_ADDR (e.g. 127.0.0.1:1502) to run against tools/sunspec-sim")
	}

	// unit 1 — SunSpec hybrid inverter (Common + 103 + 123 + 124 + 203).
	s1, common, err := PollOnce(DeviceConf{Endpoint: addr, Unit: 1, Mode: ModeSunSpec})
	if err != nil {
		t.Fatalf("unit1 sunspec poll: %v", err)
	}
	m1 := index(s1)
	t.Logf("unit1 %s %s: inv_ac_power=%.0fW batt_soc=%.0f%% grid_ac_power=%.0fW (%d samples)",
		common.Mfg, common.Model, m1["inv_ac_power"], m1["batt_soc"], m1["grid_ac_power"], len(s1))
	for _, k := range []string{"inv_ac_power", "inv_ac_energy", "batt_soc", "grid_ac_power", "ctl_w_max_lim_pct"} {
		if _, ok := m1[k]; !ok {
			t.Errorf("unit1 missing expected key %q", k)
		}
	}
	if common.Model == "" {
		t.Error("unit1 nameplate model is empty")
	}

	// unit 2 — standalone SunSpec meter (Common + 203 only). A DIFFERENT chain on another unit.
	s2, _, err := PollOnce(DeviceConf{Endpoint: addr, Unit: 2, Mode: ModeSunSpec})
	if err != nil {
		t.Fatalf("unit2 sunspec poll: %v", err)
	}
	m2 := index(s2)
	t.Logf("unit2 meter: grid_ac_power=%.0fW imported=%.2fkWh", m2["grid_ac_power"], m2["grid_energy_imported"]/1000)
	if _, ok := m2["grid_ac_power"]; !ok {
		t.Error("unit2 missing grid_ac_power")
	}
	if _, ok := m2["inv_ac_power"]; ok {
		t.Error("unit2 should have no inverter block")
	}

	// unit 3 — NON-SunSpec vendor inverter, read by a manual register map that mirrors the vendor
	// contract documented in tools/sunspec-sim/devices.go.
	vendor := RegisterMap{Points: []Point{
		{Key: "pv_power", Register: 3, Type: PU32, Scale: 0.1, Unit: "W"},
		{Key: "ac_power", Register: 7, Type: PU32, Scale: 0.1, Unit: "W"},
		{Key: "soc", Register: 13, Type: PU16, Scale: 1, Unit: "%"},
		{Key: "batt_power", Register: 14, Type: PI16, Scale: 1, Unit: "W"},
		{Key: "temperature", Register: 12, Type: PU16, Scale: 0.1, Unit: "C"},
	}}
	s3, _, err := PollOnce(DeviceConf{Endpoint: addr, Unit: 3, Mode: ModeRegMap, Map: vendor})
	if err != nil {
		t.Fatalf("unit3 regmap poll: %v", err)
	}
	m3 := index(s3)
	t.Logf("unit3 vendor: pv_power=%.0fW ac_power=%.0fW soc=%.0f%%", m3["pv_power"], m3["ac_power"], m3["soc"])
	if _, ok := m3["ac_power"]; !ok {
		t.Error("unit3 missing ac_power")
	}

	// Guarded control: enable a 40% export cap on unit 1, then confirm by read-back. Model 123 on
	// this base sits at 40122; WMaxLimPct at data offset 3 (40127), WMaxLim_Ena at offset 7 (40131).
	const wMaxLimPct, wMaxLimEna = 40127, 40131
	c, err := Dial(addr, 1, 3*time.Second)
	if err != nil {
		t.Fatalf("dial unit1: %v", err)
	}
	if err := c.WriteSingle(wMaxLimEna, 1); err != nil {
		t.Fatalf("enable curtailment: %v", err)
	}
	c.Close()
	if err := WriteConfirm(DeviceConf{Endpoint: addr, Unit: 1}, wMaxLimPct, 40, 3*time.Second); err != nil {
		t.Fatalf("guarded curtailment write not confirmed: %v", err)
	}
	t.Log("guarded write confirmed: WMaxLimPct=40 read back from the device")
}

func index(s []codec.Sample) map[string]float64 {
	m := map[string]float64{}
	for _, x := range s {
		m[x.Key] = x.Num
	}
	return m
}
