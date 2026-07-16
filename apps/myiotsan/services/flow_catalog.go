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
	return []builtinFlow{solarSystemFlow()}
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
