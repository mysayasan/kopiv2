package services

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/mysayasan/kopiv2/apps/myiotsan/entities"
	"github.com/mysayasan/kopiv2/infra/iot/codec"
	"github.com/mysayasan/kopiv2/infra/iot/modbus"
	"github.com/mysayasan/kopiv2/infra/safego"
)

// ModbusPoller drives the POLLED half of ingest.
//
// Everything shipped before P5 PUSHES: a device connects to the embedded broker and publishes, and
// ingest reacts. A Modbus device does the opposite — it sits on a TCP port and answers only when
// read — so something has to do the reading, on a schedule. That is this service: one goroutine per
// Modbus device, each dialing OUT, decoding (SunSpec auto-discovery for compliant hardware, or a
// vendor register map for the rest), and handing the samples to Ingest.HandlePolled — the identical
// deadband → storage → rules → twin back half a published reading takes.
//
// It reconciles against the device inventory on a ticker, so a device added, edited, disabled or
// deleted in the UI starts, restarts or stops its poller with no process restart. A poll is
// idempotent and connectionless (PollOnce dials fresh each tick), so a device that drops off the bus
// simply fails a tick and is retried on the next — a transient outage never needs recovery logic.
type ModbusPoller struct {
	devices  *DeviceService
	profiles *ProfileService
	ingest   *Ingest
	logf     func(string, ...any)

	mu      sync.Mutex
	running map[int64]*devicePoller // by device id
}

// devicePoller is one running per-device goroutine plus the configuration signature that started
// it, so a reconcile can tell an unchanged device (leave it running) from an edited one (restart).
type devicePoller struct {
	cancel context.CancelFunc
	sig    string
}

// NewModbusPoller builds the poller. It is inert until Run is called.
func NewModbusPoller(devices *DeviceService, profiles *ProfileService, ingest *Ingest, logf func(string, ...any)) *ModbusPoller {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &ModbusPoller{
		devices:  devices,
		profiles: profiles,
		ingest:   ingest,
		logf:     logf,
		running:  map[int64]*devicePoller{},
	}
}

// Run reconciles the running pollers with the inventory until ctx is cancelled. It is meant to be
// launched under safego.Supervise, alongside the offline and command sweeps.
func (p *ModbusPoller) Run(ctx context.Context, reconcileEvery time.Duration) {
	if reconcileEvery <= 0 {
		reconcileEvery = 30 * time.Second
	}
	t := time.NewTicker(reconcileEvery)
	defer t.Stop()
	p.reconcile(ctx)
	for {
		select {
		case <-ctx.Done():
			p.stopAll()
			return
		case <-t.C:
			p.reconcile(ctx)
		}
	}
}

// modbusPlan is the resolved intent to poll one device: what to dial and how to decode it.
type modbusPlan struct {
	dev      *entities.IotDevice
	conf     modbus.DeviceConf
	interval time.Duration
	mode     string
	sig      string
}

// reconcile diffs the desired set of pollers (enabled Modbus devices) against the running set,
// stopping those that vanished or changed and starting those that are new or reconfigured.
func (p *ModbusPoller) reconcile(ctx context.Context) {
	devs, _, err := p.devices.List(ctx, 500, 0)
	if err != nil {
		p.logf("modbus poller: list devices: %v", err)
		return
	}
	want := map[int64]modbusPlan{}
	for _, d := range devs {
		if !d.Enabled {
			continue
		}
		plan, ok, err := p.planFor(ctx, d)
		if err != nil {
			// A misconfigured Modbus device (no endpoint, unknown mode) is a config gap, not an
			// error to crash on — log it and skip, exactly as an unprofiled MQTT device is skipped.
			p.logf("modbus device %q not pollable: %v", d.DeviceKey, err)
			continue
		}
		if ok {
			want[d.Id] = plan
		}
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	// Stop pollers for devices that vanished, were disabled, or were reconfigured.
	for id, run := range p.running {
		plan, keep := want[id]
		if !keep || plan.sig != run.sig {
			run.cancel()
			delete(p.running, id)
		}
	}
	// Start pollers for devices that are newly desired (or just restarted above).
	for id, plan := range want {
		if _, ok := p.running[id]; ok {
			continue
		}
		cctx, cancel := context.WithCancel(ctx)
		p.running[id] = &devicePoller{cancel: cancel, sig: plan.sig}

		dev, conf, interval := plan.dev, plan.conf, plan.interval
		p.logf("polling %s at %s unit %d every %s (%s)", dev.DeviceKey, conf.Endpoint, conf.Unit, interval, plan.mode)
		safego.Go("myiotsan.modbus."+dev.DeviceKey, func() {
			modbus.Run(cctx, conf, interval,
				func(_ string, samples []codec.Sample) { p.ingest.HandlePolled(cctx, dev, samples) },
				func(err error) { p.logf("poll %s: %v", dev.DeviceKey, err) })
		})
	}
}

// planFor resolves a device + its profile into a poll plan, or (false, nil) if the device is not a
// Modbus device (its profile pushes over the broker instead, which is not this service's business).
func (p *ModbusPoller) planFor(ctx context.Context, d *entities.IotDevice) (modbusPlan, bool, error) {
	if d.ProfileId <= 0 {
		return modbusPlan{}, false, nil
	}
	detail, err := p.profiles.Detail(ctx, d.ProfileId)
	if err != nil {
		return modbusPlan{}, false, err
	}
	prof := detail.Profile
	if prof == nil || !strings.EqualFold(strings.TrimSpace(prof.Transport), "modbus") {
		return modbusPlan{}, false, nil // a push device — the broker path owns it
	}
	if strings.TrimSpace(d.Endpoint) == "" {
		return modbusPlan{}, false, fmt.Errorf("a Modbus device needs an endpoint (host:port)")
	}

	conf := modbus.DeviceConf{
		Key:      d.DeviceKey,
		Endpoint: strings.TrimSpace(d.Endpoint),
		Unit:     byte(d.Unit),
		Base:     prof.ModbusBase,
	}
	mode := strings.ToLower(strings.TrimSpace(prof.ModbusMode))
	switch mode {
	case "", "sunspec":
		conf.Mode = modbus.ModeSunSpec
		mode = "sunspec"
	case "regmap":
		conf.Mode = modbus.ModeRegMap
		m, err := registerMapFromKeys(detail.Keys)
		if err != nil {
			return modbusPlan{}, false, err
		}
		conf.Map = m
	default:
		return modbusPlan{}, false, fmt.Errorf("unknown modbus mode %q (want sunspec|regmap)", prof.ModbusMode)
	}

	interval := time.Duration(prof.PollSeconds) * time.Second
	if interval <= 0 {
		interval = 5 * time.Second
	}
	// The signature captures everything that would change how (or how often) this device is polled:
	// its address, the decode mode/base, the cadence, and the profile's UpdatedAt — which bumps
	// whenever the register-map keys are edited, so a remapped register restarts the poller.
	sig := fmt.Sprintf("%s|%d|%s|%d|%ds|p%d|d%d",
		conf.Endpoint, d.Unit, mode, prof.ModbusBase, prof.PollSeconds, prof.UpdatedAt, d.UpdatedAt)
	return modbusPlan{dev: d, conf: conf, interval: interval, mode: mode, sig: sig}, true, nil
}

// stopAll cancels every running poller (on shutdown).
func (p *ModbusPoller) stopAll() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for id, run := range p.running {
		run.cancel()
		delete(p.running, id)
	}
}

// registerMapFromKeys builds a modbus.RegisterMap from a register-map profile's telemetry keys. Keys
// without a RegKind are skipped (a profile may mix Modbus and non-Modbus keys), and a profile that
// declares none is an error — a register-map device with nothing mapped can never yield a reading.
func registerMapFromKeys(keys []*entities.TelemetryKey) (modbus.RegisterMap, error) {
	var pts []modbus.Point
	for _, k := range keys {
		if strings.TrimSpace(k.RegKind) == "" {
			continue
		}
		t, err := ptypeOf(k.RegKind)
		if err != nil {
			return modbus.RegisterMap{}, fmt.Errorf("key %q: %w", k.Key, err)
		}
		pts = append(pts, modbus.Point{
			Key:      k.Key,
			Register: k.Register,
			Type:     t,
			Scale:    k.ScaleFactor,
			WordSwap: k.WordSwap,
			Unit:     k.Unit,
		})
	}
	if len(pts) == 0 {
		return modbus.RegisterMap{}, fmt.Errorf("register-map profile declares no Modbus-bound keys")
	}
	return modbus.RegisterMap{Points: pts}, nil
}

// ptypeOf maps the profile's register-kind string to the driver's PType.
func ptypeOf(kind string) (modbus.PType, error) {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "u16":
		return modbus.PU16, nil
	case "i16":
		return modbus.PI16, nil
	case "u32":
		return modbus.PU32, nil
	case "i32":
		return modbus.PI32, nil
	default:
		return 0, fmt.Errorf("unknown register kind %q (want u16|i16|u32|i32)", kind)
	}
}
