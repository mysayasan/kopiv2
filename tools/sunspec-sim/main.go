// Command sunspec-sim is a SunSpec-over-Modbus-TCP solar system simulator.
//
// It stands up a Modbus TCP server that serves a real SunSpec model chain — Common, Inverter
// (103), Immediate Controls (123), Storage (124) and Meter (203) — populated by a live physics
// loop that simulates a PV array, a hybrid inverter, a battery and a grid meter over a compressed
// day. Production rises to a noon peak, the battery charges on surplus and carries the evening
// load, and the meter swings between import and export.
//
// It exists so myiotsan's forthcoming Modbus/SunSpec driver and the solar "system workspace" can
// be built and exercised WITHOUT real hardware — the same way the -deaf relay simulator let the
// actuation path be verified against a device that obeys but never acknowledges. Control writes
// are honoured: enable WMaxLimPct to watch the inverter curtail, clear a StorCtl_Mod bit to watch
// the battery hold — the read-back a guarded write confirms against actually changes.
//
// Usage:
//
//	go run ./tools/sunspec-sim -addr :1502 -scenario sunny -speed 1800
//
// then point a Modbus client at 127.0.0.1:1502 and read holding registers from the base (40000).
package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	addr := flag.String("addr", ":1502", "Modbus TCP listen address (real SunSpec uses 502; 1502 avoids needing root)")
	base := flag.Int("base", 40000, "base holding-register address of the SunS marker (40000, 50000 or 0)")
	speed := flag.Float64("speed", 1800, "simulated seconds per real second (1800 = a 24h day in 48s; 1 = realtime)")
	tickMs := flag.Int("tick", 1000, "physics tick interval, milliseconds")
	scenario := flag.String("scenario", "sunny", "weather/grid scenario: sunny | cloudy | storm | night | outage")
	pv := flag.Float64("pv", 10000, "PV array / inverter nameplate, W")
	load := flag.Float64("load", 600, "baseline house load, W")
	batt := flag.Float64("batt", 10000, "usable battery energy, Wh")
	soc := flag.Float64("soc", 50, "initial battery state of charge, %")
	rsv := flag.Float64("reserve", 20, "battery reserve floor (will not discharge below), %")
	vnom := flag.Float64("volt", 230, "nominal per-phase voltage, V")
	fnom := flag.Float64("freq", 50, "nominal frequency, Hz")
	mfg := flag.String("mfg", "SunSpec Sim", "device manufacturer (Common model)")
	model := flag.String("model", "Hybrid-10K", "device model (Common model)")
	serial := flag.String("serial", "SIM-0001", "device serial number (Common model)")
	single := flag.Bool("single", false, "serve only the hybrid inverter (unit 1); omit for the full 3-device site")
	quiet := flag.Bool("quiet", false, "suppress the per-tick status line")
	verbose := flag.Bool("v", false, "log every Modbus request")
	flag.Parse()

	switch *scenario {
	case "sunny", "cloudy", "storm", "night", "outage":
	default:
		log.Fatalf("unknown scenario %q (want sunny|cloudy|storm|night|outage)", *scenario)
	}

	cfg := Config{
		PVPeakW: *pv, LoadBaseW: *load, BattWh: *batt, InitSoC: *soc, MinRsvPct: *rsv,
		Vnom: *vnom, Fnom: *fnom, Scenario: *scenario,
		Common: CommonInfo{Mfg: *mfg, Model: *model, Version: "1.0.0", Serial: *serial},
	}

	// A simulated site is several Modbus units. Unit 1 is the hybrid inverter (full SunSpec chain);
	// unit 2 a standalone SunSpec meter; unit 3 a generic non-SunSpec vendor inverter (raw register
	// map); unit 4 a Huawei SUN2000 — the world's most-installed inverter — whose battery and meter
	// blocks sit far from its inverter block, exercising the driver's clustered register reads and
	// backing the shipped huawei-sun2000 builtin profile. That mix is what the workspace binds into
	// one system.
	devices := []Device{buildPlant(1, *base, cfg)}
	if !*single {
		devices = append(devices,
			buildMeter(2, *base, "MTR-0002"),
			buildVendor(3, *pv*0.5),
			buildHuawei(4, *pv, *load, *batt, *soc))
	}
	byUnit := map[byte]Device{}
	for _, d := range devices {
		byUnit[d.unit()] = d
	}

	log.Printf("SunSpec solar simulator — %d device(s) on %s", len(devices), *addr)
	for _, d := range devices {
		log.Printf("  %s", d.label())
	}
	log.Printf("  hybrid SunSpec chain: base %d, spans %d..%d (models 1/103/123/124/203)", *base, *base, chainTop(*base))
	log.Printf("  scenario: %s   speed x%.0f", *scenario, *speed)
	log.Printf("  writable: unit1 123.WMaxLimPct/WMaxLim_Ena (curtailment), 124.StorCtl_Mod (batt mode)")

	srv := &Server{
		devices: byUnit,
		verbose: *verbose,
		onWrite: func(d Device, a int, v uint16) {
			if name := d.describe(a); name != "" {
				log.Printf("CONTROL WRITE  unit %d  %s = %d  (reg %d)", d.unit(), name, v, a)
			}
		},
	}
	go func() {
		if err := srv.listenAndServe(*addr); err != nil {
			log.Fatalf("modbus server: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	dtHours := *speed * float64(*tickMs) / 1000 / 3600
	ticker := time.NewTicker(time.Duration(*tickMs) * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			for _, d := range devices {
				d.update(dtHours)
			}
			if !*quiet {
				for _, d := range devices {
					log.Printf("u%d %s", d.unit(), d.status())
				}
			}
		case <-stop:
			log.Print("shutting down")
			return
		}
	}
}
