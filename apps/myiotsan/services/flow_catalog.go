package services

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/mysayasan/kopiv2/apps/myiotsan/entities"
)

// flow_catalog.go declares the SHIPPED sample flows, seeded at boot the same way device profiles
// are (profile_catalog.go): present-by-slug is skipped so a site never has a tuned flow overwritten,
// and a builtin can be used and copied but not deleted.
//
// A builtin flow is a TEMPLATE to learn from and adapt, not a turnkey install: its input nodes name
// devices by a placeholder key ("inverter-1"), because the real device keys are chosen at adoption.
// It is created ENABLED=false for the same reason an imported flow is — it should not try to act
// until an admin has rebound it to real devices and switched it on.

type builtinFlow struct {
	Slug        string
	Name        string
	Description string
	Category    string
	Graph       string
}

// mustGraph marshals a graph built from the typed structs, so a builtin is guaranteed to be valid
// JSON that parseGraph accepts. A failure here is a programming error caught at first boot.
func mustGraph(g flowGraph) string {
	b, err := json.Marshal(g)
	if err != nil {
		panic("flow catalog: " + err.Error())
	}
	return string(b)
}

func builtinFlows() []builtinFlow {
	return []builtinFlow{
		solarSystemFlow(),
		selfConsumptionFlow(),
		selfSufficiencyFlow(),
		batteryGuardFlow(),
		exportLimitFlow(),
		inverterHealthFlow(),
	}
}

// solarSystemFlow is the reusable "Solar system" sample the design calls for. It shows the one
// thing the data-flow model exists to do — COMBINE two telemetry streams into a derived value —
// alongside a simple threshold alert:
//
//	grid_ac_power ─┐
//	               ├─▶ [function: self_consumption] ─▶ [debug]
//	inv_ac_power ──┘
//	grid_ac_power ─▶ [threshold > 3000 W] ─▶ [notify: high grid import]
//
// The function node stashes each stream in the per-flow context and recomputes on every reading —
// the idiomatic way to join streams when each arrives as its own message.
func solarSystemFlow() builtinFlow {
	// A SLOT, not a concrete device: "$inverter" makes this a reusable TEMPLATE. Instantiating it
	// binds the slot to a real adopted inverter and stamps out a concrete flow (see Instantiate).
	const dev = "$inverter"
	g := flowGraph{
		Nodes: []flowNode{
			{Id: "in_grid", Type: nodeDeviceTelemetry, X: 40, Y: 60,
				Config: map[string]any{"deviceKey": dev, "key": "grid_ac_power"}},
			{Id: "in_pv", Type: nodeDeviceTelemetry, X: 40, Y: 180,
				Config: map[string]any{"deviceKey": dev, "key": "inv_ac_power"}},
			{Id: "fn_self", Type: nodeFunction, X: 280, Y: 120, Config: map[string]any{
				"code": "" +
					"if (msg.key === 'inv_ac_power') flow.set('pv', msg.payload);\n" +
					"if (msg.key === 'grid_ac_power') flow.set('grid', msg.payload);\n" +
					"var pv = flow.get('pv') || 0;\n" +
					"var grid = flow.get('grid') || 0;\n" +
					"// grid > 0 imports, grid < 0 exports; PV used on-site = pv minus what we export\n" +
					"var exporting = grid < 0 ? -grid : 0;\n" +
					"msg.payload = Math.max(0, pv - exporting);\n" +
					"msg.key = 'self_consumption';\n" +
					"return msg;",
			}},
			{Id: "dbg_self", Type: nodeDebug, X: 520, Y: 120,
				Config: map[string]any{"label": "self consumption (W)"}},
			{Id: "thr_import", Type: nodeThreshold, X: 280, Y: 300,
				Config: map[string]any{"op": ">", "value": 3000.0}},
			{Id: "notify_import", Type: nodeNotify, X: 520, Y: 300, Config: map[string]any{
				"title":    "High grid import",
				"severity": "warning",
				"body":     "The site is importing more than 3 kW from the grid.",
			}},
		},
		Wires: []flowWire{
			{From: flowPort{Node: "in_grid"}, To: flowPort{Node: "fn_self"}},
			{From: flowPort{Node: "in_pv"}, To: flowPort{Node: "fn_self"}},
			{From: flowPort{Node: "fn_self"}, To: flowPort{Node: "dbg_self"}},
			{From: flowPort{Node: "in_grid"}, To: flowPort{Node: "thr_import"}},
			{From: flowPort{Node: "thr_import"}, To: flowPort{Node: "notify_import"}},
		},
	}
	return builtinFlow{
		Slug:        "solar-system",
		Name:        "Solar system (sample)",
		Description: "Reusable template: derives on-site solar self-consumption from grid + PV power, and alerts on high grid import. Instantiate it — bind the $inverter slot to your adopted inverter — to stamp out a ready-to-enable flow.",
		Category:    "Solar",
		Graph:       mustGraph(g),
	}
}

// selfConsumptionFlow persists on-site solar self-consumption as its own telemetry series, so it
// rides the same rollups/charts as any real reading. Universal: works with any inverter profile that
// emits grid_ac_power (+import/-export) and inv_ac_power.
//
//	grid_ac_power ─┐
//	               ├─▶ [function: self_consumption] ─▶ [derived_metric]
//	inv_ac_power ──┘
func selfConsumptionFlow() builtinFlow {
	const dev = "$inverter"
	g := flowGraph{
		Nodes: []flowNode{
			{Id: "in_grid", Type: nodeDeviceTelemetry, X: 40, Y: 60, Config: map[string]any{"deviceKey": dev, "key": "grid_ac_power"}},
			{Id: "in_pv", Type: nodeDeviceTelemetry, X: 40, Y: 180, Config: map[string]any{"deviceKey": dev, "key": "inv_ac_power"}},
			{Id: "fn", Type: nodeFunction, X: 280, Y: 120, Config: map[string]any{
				"code": "" +
					"if (msg.key === 'inv_ac_power') flow.set('pv', msg.payload);\n" +
					"if (msg.key === 'grid_ac_power') flow.set('grid', msg.payload);\n" +
					"var pv = flow.get('pv') || 0;\n" +
					"var grid = flow.get('grid') || 0;\n" +
					"// grid > 0 imports, grid < 0 exports; solar used on-site = production minus export.\n" +
					"var exporting = grid < 0 ? -grid : 0;\n" +
					"msg.payload = Math.max(0, pv - exporting);\n" +
					"msg.key = 'self_consumption';\n" +
					"return msg;",
			}},
			{Id: "out", Type: nodeDerivedMetric, X: 520, Y: 120, Config: map[string]any{"deviceKey": dev, "key": "self_consumption"}},
		},
		Wires: []flowWire{
			{From: flowPort{Node: "in_grid"}, To: flowPort{Node: "fn"}},
			{From: flowPort{Node: "in_pv"}, To: flowPort{Node: "fn"}},
			{From: flowPort{Node: "fn"}, To: flowPort{Node: "out"}},
		},
	}
	return builtinFlow{
		Slug: "solar-self-consumption", Name: "Solar self-consumption (derived)",
		Description: "Reusable template: derives the watts of solar used on-site (production minus export) and persists it as a self_consumption series on the inverter, so it charts and rolls up like any reading. Bind $inverter to your adopted inverter.",
		Category:    "Solar", Graph: mustGraph(g),
	}
}

// selfSufficiencyFlow persists self-sufficiency % (solar share of consumption) from the lifetime
// energy counters. Uses inv_ac_energy (PV yield) + grid_energy_imported.
func selfSufficiencyFlow() builtinFlow {
	const dev = "$inverter"
	g := flowGraph{
		Nodes: []flowNode{
			{Id: "in_pv", Type: nodeDeviceTelemetry, X: 40, Y: 60, Config: map[string]any{"deviceKey": dev, "key": "inv_ac_energy"}},
			{Id: "in_imp", Type: nodeDeviceTelemetry, X: 40, Y: 180, Config: map[string]any{"deviceKey": dev, "key": "grid_energy_imported"}},
			{Id: "fn", Type: nodeFunction, X: 280, Y: 120, Config: map[string]any{
				"code": "" +
					"if (msg.key === 'inv_ac_energy') flow.set('pv', msg.payload);\n" +
					"if (msg.key === 'grid_energy_imported') flow.set('imp', msg.payload);\n" +
					"var pv = flow.get('pv') || 0;\n" +
					"var imp = flow.get('imp') || 0;\n" +
					"var total = pv + imp;\n" +
					"// Share of energy that came from solar rather than the grid.\n" +
					"msg.payload = total > 0 ? (pv / total) * 100 : 0;\n" +
					"msg.key = 'self_sufficiency';\n" +
					"return msg;",
			}},
			{Id: "out", Type: nodeDerivedMetric, X: 520, Y: 120, Config: map[string]any{"deviceKey": dev, "key": "self_sufficiency"}},
		},
		Wires: []flowWire{
			{From: flowPort{Node: "in_pv"}, To: flowPort{Node: "fn"}},
			{From: flowPort{Node: "in_imp"}, To: flowPort{Node: "fn"}},
			{From: flowPort{Node: "fn"}, To: flowPort{Node: "out"}},
		},
	}
	return builtinFlow{
		Slug: "solar-self-sufficiency", Name: "Self-sufficiency % (derived)",
		Description: "Reusable template: derives self-sufficiency (solar share of total energy) from the lifetime PV-yield and grid-import counters, persisted as a self_sufficiency % series. Bind $inverter.",
		Category:    "Solar", Graph: mustGraph(g),
	}
}

// batteryGuardFlow alerts on a low battery and — the control half — force-charges it when it falls
// critically low. The command node routes through CommandService.Issue, so it inherits every gate;
// it stays inert until the device's actuation is enabled. Sungrow: batt_force=170 (Charge, requires
// EMS mode = Forced). For a Deye, retarget the command to grid_charge=1 (see the KB).
//
//	batt_soc ─┬─▶ [threshold < 15] ─▶ [notify: battery low]
//	          └─▶ [threshold < 8]  ─▶ [command: force charge]
func batteryGuardFlow() builtinFlow {
	const dev = "$inverter"
	g := flowGraph{
		Nodes: []flowNode{
			{Id: "in_soc", Type: nodeDeviceTelemetry, X: 40, Y: 120, Config: map[string]any{"deviceKey": dev, "key": "batt_soc"}},
			{Id: "thr_low", Type: nodeThreshold, X: 280, Y: 60, Config: map[string]any{"op": "<", "value": 15.0}},
			{Id: "notify_low", Type: nodeNotify, X: 520, Y: 60, Config: map[string]any{
				"title": "Battery low", "severity": "warning",
				"body": "Battery state of charge has fallen below 15%.",
			}},
			{Id: "thr_crit", Type: nodeThreshold, X: 280, Y: 200, Config: map[string]any{"op": "<", "value": 8.0}},
			// Force-charge. value 170 = 0xAA (Sungrow "Charge"). VERIFY + enable actuation first.
			{Id: "cmd_charge", Type: nodeCommand, X: 520, Y: 200, Config: map[string]any{
				"deviceKey": dev, "command": "batt_force", "value": 170.0,
			}},
		},
		Wires: []flowWire{
			{From: flowPort{Node: "in_soc"}, To: flowPort{Node: "thr_low"}},
			{From: flowPort{Node: "thr_low"}, To: flowPort{Node: "notify_low"}},
			{From: flowPort{Node: "in_soc"}, To: flowPort{Node: "thr_crit"}},
			{From: flowPort{Node: "thr_crit"}, To: flowPort{Node: "cmd_charge"}},
		},
	}
	return builtinFlow{
		Slug: "solar-battery-guard", Name: "Battery low-SoC guard + force-charge",
		Description: "Reusable template: alerts when battery SoC drops below 15% and force-charges below 8% via the guarded command path. The command is inert until you enable actuation on the device and verify the register — see docs/kb/solar/. Bind $inverter.",
		Category:    "Solar", Graph: mustGraph(g),
	}
}

// exportLimitFlow caps grid export: when feed-in exceeds an allowance, it writes the inverter's
// export-power limit through the guarded path. THE control showcase. Sungrow: export_limit (W);
// for a Deye retarget to max_sell_power. Inert until actuation is enabled + the register verified.
//
//	grid_ac_power ─▶ [function: export = max(0,-grid)] ─▶ [threshold > 4500] ─▶ [command: export_limit=4000]
func exportLimitFlow() builtinFlow {
	const dev = "$inverter"
	g := flowGraph{
		Nodes: []flowNode{
			{Id: "in_grid", Type: nodeDeviceTelemetry, X: 40, Y: 120, Config: map[string]any{"deviceKey": dev, "key": "grid_ac_power"}},
			{Id: "fn", Type: nodeFunction, X: 260, Y: 120, Config: map[string]any{
				"code": "" +
					"// grid_ac_power: + import / - export. Export magnitude = -grid when negative.\n" +
					"msg.payload = msg.payload < 0 ? -msg.payload : 0;\n" +
					"msg.key = 'grid_export';\n" +
					"return msg;",
			}},
			{Id: "thr", Type: nodeThreshold, X: 480, Y: 120, Config: map[string]any{"op": ">", "value": 4500.0}},
			{Id: "cmd", Type: nodeCommand, X: 700, Y: 120, Config: map[string]any{
				"deviceKey": dev, "command": "export_limit", "value": 4000.0,
			}},
		},
		Wires: []flowWire{
			{From: flowPort{Node: "in_grid"}, To: flowPort{Node: "fn"}},
			{From: flowPort{Node: "fn"}, To: flowPort{Node: "thr"}},
			{From: flowPort{Node: "thr"}, To: flowPort{Node: "cmd"}},
		},
	}
	return builtinFlow{
		Slug: "solar-export-limit", Name: "Grid export-limit control",
		Description: "Reusable template: when feed-in to the grid exceeds ~4.5 kW, it clamps the inverter's export-power limit to 4 kW through the guarded command path. The control showcase — inert until actuation is enabled and the export-limit register is verified for your model. Bind $inverter.",
		Category:    "Solar", Graph: mustGraph(g),
	}
}

// inverterHealthFlow alerts on the two ways a string inverter fails silently: it runs too hot, and
// it drops out of its normal running state. Overheat uses inv_temperature; the fault branch uses a
// switch predicate on inv_operating_state (the "normal" code differs by vendor — tune the predicate).
func inverterHealthFlow() builtinFlow {
	const dev = "$inverter"
	g := flowGraph{
		Nodes: []flowNode{
			{Id: "in_temp", Type: nodeDeviceTelemetry, X: 40, Y: 60, Config: map[string]any{"deviceKey": dev, "key": "inv_temperature"}},
			{Id: "thr_hot", Type: nodeThreshold, X: 280, Y: 60, Config: map[string]any{"op": ">", "value": 60.0}},
			{Id: "notify_hot", Type: nodeNotify, X: 520, Y: 60, Config: map[string]any{
				"title": "Inverter overheating", "severity": "warning",
				"body": "Inverter internal temperature is above 60 °C.",
			}},
			{Id: "in_state", Type: nodeDeviceTelemetry, X: 40, Y: 200, Config: map[string]any{"deviceKey": dev, "key": "inv_operating_state"}},
			// A running-state that is neither 0 (off/standby) nor the vendor's "running" code is a fault.
			// Tune the predicate to your inverter's state table (see the KB).
			{Id: "sw_fault", Type: nodeSwitch, X: 280, Y: 200, Config: map[string]any{
				"predicate": "msg.payload >= 5",
			}},
			{Id: "notify_fault", Type: nodeNotify, X: 520, Y: 200, Config: map[string]any{
				"title": "Inverter fault", "severity": "critical",
				"body": "Inverter reported a fault/abnormal running state.",
			}},
		},
		Wires: []flowWire{
			{From: flowPort{Node: "in_temp"}, To: flowPort{Node: "thr_hot"}},
			{From: flowPort{Node: "thr_hot"}, To: flowPort{Node: "notify_hot"}},
			{From: flowPort{Node: "in_state"}, To: flowPort{Node: "sw_fault"}},
			{From: flowPort{Node: "sw_fault"}, To: flowPort{Node: "notify_fault"}},
		},
	}
	return builtinFlow{
		Slug: "solar-inverter-health", Name: "Inverter overheat & fault alert",
		Description: "Reusable template: alerts when the inverter runs above 60 °C or reports an abnormal running state. Tune the fault predicate to your inverter's state table. Bind $inverter (needs inv_temperature + inv_operating_state, e.g. Sungrow/Huawei).",
		Category:    "Solar", Graph: mustGraph(g),
	}
}

// EnsureBuiltins seeds the shipped sample flows. Present-by-slug is skipped, so a site's edits to a
// copied flow are never clobbered.
func (s *FlowService) EnsureBuiltins(ctx context.Context) error {
	for _, b := range builtinFlows() {
		existing, err := s.flows.GetByUnique(ctx, "", "slug", b.Slug)
		if err != nil && !isNoResultErr(err) {
			return err
		}
		if existing != nil {
			continue
		}
		now := time.Now().Unix()
		if _, err := s.flows.Create(ctx, "", entities.IotFlow{
			Slug: b.Slug, Name: b.Name, Description: b.Description, Category: b.Category,
			Enabled: false, Graph: b.Graph, Builtin: true,
			CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			return fmt.Errorf("seed flow %s: %w", b.Slug, err)
		}
	}
	return nil
}
