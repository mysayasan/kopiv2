package app

import (
	"context"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	appentities "github.com/mysayasan/kopiv2/apps/mypintusan/entities"
	"github.com/mysayasan/kopiv2/apps/mypintusan/services"
	"github.com/mysayasan/kopiv2/infra/access/osdp"
	"github.com/mysayasan/kopiv2/infra/apphost"
	"github.com/mysayasan/kopiv2/infra/safego"
)

// runtime owns the OSDP buses and their controllers, and keeps them alive.
type runtime struct {
	deps     apphost.Dependencies
	cfg      services.AccessSettings
	location *time.Location
	store    services.Store
	alarms   services.Alarmer
	strikes  services.StrikeResolver
	// decisions receives every access decision for the notification feed (and, through it, the
	// fleet uplink). Nil when nothing consumes decisions.
	decisions func(ctx context.Context, ev appentities.AccessEvent)
	// cache measures how stale this controller's replica is. See services.CacheClock: without it
	// `Decide()`'s TTL comparison has no number to compare and can never deny.
	cache *services.CacheClock

	cancel context.CancelFunc

	// offline is the live answer to "is this controller serving from a cached replica?".
	//
	// ATOMIC AND HELD HERE, not copied into each controller, for the same reason lockdown is:
	// controllers are rebuilt on every bus reconnect, and a value captured at process start cannot
	// follow a setting an operator changes afterwards. Before this the flag was read once at boot,
	// so turning offline mode on from the settings API persisted the change, returned 200, read
	// back as true — and every door carried on deciding as though it were online.
	offline atomic.Bool

	mu sync.RWMutex
	// lockdown is held HERE rather than only inside a controller, because controllers are rebuilt
	// on every reconnect. Keeping it on the runtime is what stops unplugging a USB adapter from
	// quietly lifting a lockdown.
	lockdown bool
	// live maps a bus port to its current controller; the value is replaced on each reconnect.
	live map[string]*services.Controller
}

func newRuntime(deps apphost.Dependencies, cfg services.AccessSettings, loc *time.Location,
	store services.Store, alarms services.Alarmer,
	decisions func(ctx context.Context, ev appentities.AccessEvent),
	strikes services.StrikeResolver, cache *services.CacheClock) *runtime {
	r := &runtime{
		deps: deps, cfg: cfg, location: loc, store: store, alarms: alarms, decisions: decisions,
		strikes: strikes, cache: cache,
		live: map[string]*services.Controller{},
	}
	// offline is deliberately NOT seeded here. The caller applies it through SetOffline so that a
	// site configured offline from first boot gets the degraded-mode alert like any other.
	return r
}

// SetOffline puts the site into — or takes it out of — cached-replica mode, and tells somebody.
//
// THE ALERT IS THE POINT. A cached grant is indistinguishable from a live one at the door, on the
// activity screen and in the access log's decision column; the only visible difference is a boolean
// on each row that nobody reads. docs/MYPINTUSAN_DATA_MODEL.md §2 has always said a controller
// running degraded "raises a degraded-mode alert immediately", and nothing raised one. A site can
// be running on rules it can no longer be told have changed, and until now the first sign of it
// would have been a door refusing a badge after the TTL expired, with no earlier warning at all.
//
// COMING BACK is logged, not alarmed. An alarm's headline is fixed by its kind, so an all-clear
// raised as a `degraded` alarm would arrive on somebody's phone titled "Running from cache" while
// saying the opposite in its body. This app has no alarm-clear concept to hang it on, and a
// contradictory 3am notification is worse than a journal line.
func (r *runtime) SetOffline(ctx context.Context, on bool) {
	if r.offline.Swap(on) == on {
		return
	}
	if !on {
		r.deps.Logger.Infof("mypintusan.access",
			"offline mode is off; this controller is back on a live source of truth")
		return
	}
	r.deps.Logger.Warnf("mypintusan.access",
		"offline mode is ON; access decisions are being served from the cached replica")
	if r.alarms == nil {
		return
	}
	r.alarms.Raise(ctx, services.AlarmDegraded,
		appentities.AccessEvent{At: time.Now().Unix()},
		"this controller is serving access decisions from its cached replica; changes made "+
			"elsewhere cannot reach it, and each door denies once its offline cache TTL expires")
}

// Offline reports the live cached-replica state.
func (r *runtime) Offline() bool { return r.offline.Load() }

// ApplySettings carries a settings edit into the RUNNING controllers, and says plainly which parts
// of the edit it could not carry.
//
// Only `offline` takes effect live. The bus list, the reader keys, the tick and the site timezone
// are all read while building a bus supervisor and a controller, and re-reading them would mean
// tearing down every segment on the site mid-save — which on a door controller means every door on
// the site going dead for the length of a reconnect because somebody renamed a reader. The Settings
// screen carries a restart note for exactly that reason; this log line is its counterpart for an
// operator reading the journal.
func (r *runtime) ApplySettings(ctx context.Context, s services.AccessSettings) {
	r.SetOffline(ctx, s.Offline)
}

// start supervises every configured bus until the context is cancelled.
func (r *runtime) start(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	r.cancel = cancel

	configured := 0
	for _, bus := range r.cfg.Buses {
		if strings.TrimSpace(bus.Port) == "" {
			continue
		}
		configured++
		b := bus
		safego.Go("mypintusan.osdp.supervisor."+b.Port, func() { r.superviseBus(ctx, b) })
	}

	if configured == 0 {
		// Not an error: a fresh install has no buses until somebody wires a reader.
		r.deps.Logger.Infof("mypintusan.bus",
			"no OSDP buses configured; the API is up but no reader is being polled")
	}
	return nil
}

// superviseBus keeps one segment running, re-dialling whenever the transport dies.
//
// THIS LOOP IS THE DIFFERENCE BETWEEN A DOOR CONTROLLER AND A DEMO. Without it a CP that loses its
// port — a USB-RS485 adapter unplugged, a serial-to-Ethernet gateway rebooting, a cable knocked out
// by a cleaner — never recovers, and every door on that segment stays dead until somebody notices
// and restarts the process.
//
// It was found by booting the app against the simulator and then restarting the simulator: the app
// never reconnected, events simply stopped, and nothing in the logs said why. No unit test caught
// it, because the tests run over an in-memory pipe that is never torn down mid-run.
//
// A fresh Bus and Controller are built on each attempt: Bus closes its port on exit, and the per-PD
// sequence and Secure Channel state belong to the dead session. Lockdown is re-applied from the
// runtime, deliberately — a reconnect must never become a way to unseal a building.
func (r *runtime) superviseBus(ctx context.Context, cfg services.BusSettings) {
	const (
		minBackoff = time.Second
		maxBackoff = 30 * time.Second
	)
	backoff := minBackoff

	for ctx.Err() == nil {
		err := r.runBus(ctx, cfg)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			r.deps.Logger.Errorf("mypintusan.bus", "bus %s went down (%v); re-dialling in %s",
				cfg.Port, err, backoff)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		// Back off so a permanently absent adapter does not spin, but cap it: a door that comes
		// back must not wait minutes to be polled again.
		if backoff *= 2; backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

// runBus dials, runs one session, and returns when it ends.
func (r *runtime) runBus(ctx context.Context, cfg services.BusSettings) error {
	transport, err := dialBus(ctx, cfg.Port)
	if err != nil {
		return err
	}

	bus := osdp.NewBus(transport, osdp.Options{
		SlotInterval:   time.Duration(cfg.SlotMillis) * time.Millisecond,
		ReplyTimeout:   time.Duration(cfg.ReplyTimeoutMillis) * time.Millisecond,
		StatusInterval: time.Duration(cfg.StatusMillis) * time.Millisecond,
		Logf: func(f string, a ...any) {
			r.deps.Logger.Infof("mypintusan.osdp."+cfg.Port, f, a...)
		},
	})

	enrolled := 0
	for _, rd := range cfg.Readers {
		pdCfg := osdp.PDConfig{Address: uint8(rd.Address), RequireSecureChannel: rd.RequireSecureChannel}
		if key := strings.TrimSpace(rd.SCBK); key != "" {
			raw, decErr := hex.DecodeString(key)
			if decErr != nil || len(raw) != 16 {
				// Refuse the READER, not the bus. A mistyped key must never silently become a
				// cleartext session on a door configured to require encryption.
				r.deps.Logger.Errorf("mypintusan.bus",
					"bus %s reader %d has an invalid SCBK (need 16 bytes of hex); reader not enrolled",
					cfg.Port, rd.Address)
				continue
			}
			var scbk osdp.SCBK
			copy(scbk[:], raw)
			pdCfg.SCBK = &scbk
		} else if rd.RequireSecureChannel {
			r.deps.Logger.Errorf("mypintusan.bus",
				"bus %s reader %d requires Secure Channel but has no key; reader not enrolled",
				cfg.Port, rd.Address)
			continue
		}
		if addErr := bus.AddPD(pdCfg); addErr != nil {
			r.deps.Logger.Errorf("mypintusan.bus", "bus %s reader %d: %v", cfg.Port, rd.Address, addErr)
			continue
		}
		enrolled++
	}
	if enrolled == 0 {
		return fmt.Errorf("bus %s has no usable readers configured", cfg.Port)
	}

	ctrl := services.NewController(r.store, bus, services.BusActuator{
		Bus: bus, Resolve: r.strikes,
	}, r.alarms, services.BcryptPIN, services.ControllerConfig{
		BusPort:  cfg.Port,
		Location: r.location,
		// A live question, not the value at process start — see runtime.offline.
		Offline:      r.Offline,
		CacheAge:     r.cache.AgeFunc(),
		TickInterval: time.Duration(r.cfg.TickSeconds) * time.Second,
		PINWindow:    time.Duration(r.cfg.PINWindowSeconds) * time.Second,
		Decisions:    r.decisions,
	})

	busCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	r.mu.Lock()
	r.live[cfg.Port] = ctrl
	sealed := r.lockdown
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		if r.live[cfg.Port] == ctrl {
			delete(r.live, cfg.Port)
		}
		r.mu.Unlock()
	}()

	// Carry the site's lockdown into the new session BEFORE polling starts, so there is no window
	// in which a reconnected bus would honour a badge the site is currently refusing.
	if sealed {
		ctrl.SetLockdown(busCtx, true)
	}

	ctrlDone := make(chan struct{})
	safego.Go("mypintusan.osdp.ctrl."+cfg.Port, func() {
		defer close(ctrlDone)
		_ = ctrl.Run(busCtx)
	})

	runErr := bus.Run(busCtx)
	cancel()
	<-ctrlDone
	return runErr
}

func (r *runtime) stop() {
	if r.cancel != nil {
		r.cancel()
	}
}

// SetLockdown seals or releases every bus, and records the state so a reconnect preserves it.
func (r *runtime) SetLockdown(ctx context.Context, on bool) {
	r.mu.Lock()
	r.lockdown = on
	ctrls := r.snapshotLocked()
	r.mu.Unlock()

	for _, c := range ctrls {
		c.SetLockdown(ctx, on)
	}
}

// Lockdown reports the site's sealed state.
//
// It reads the RUNTIME's flag, not a controller's. A site whose buses are all disconnected is still
// in lockdown, and reporting "not locked down" because nothing is live would be a dangerous lie to
// put in front of an operator.
func (r *runtime) Lockdown() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.lockdown
}

// Unlock opens a door through the audited chokepoint, from an operator action rather than a badge.
func (r *runtime) Unlock(ctx context.Context, door appentities.Door, actor int64, actorName string) error {
	r.mu.RLock()
	ctrls := r.snapshotLocked()
	r.mu.RUnlock()

	if len(ctrls) == 0 {
		return fmt.Errorf("no OSDP bus is currently connected")
	}
	for _, c := range ctrls {
		err := c.OperatorUnlock(ctx, door, actor, actorName)
		if err == nil {
			return nil
		}
		if !services.IsUnknownDoor(err) {
			return err
		}
	}
	return fmt.Errorf("door %d is not on any connected bus", door.Id)
}

// snapshotLocked copies the live controllers. The caller must hold the lock.
func (r *runtime) snapshotLocked() []*services.Controller {
	out := make([]*services.Controller, 0, len(r.live))
	for _, c := range r.live {
		out = append(out, c)
	}
	return out
}

// dialBus opens a transport. Serial is build-order step 5 and lands with the first real reader on
// the bench, so only the TCP form (the simulator, or a serial-to-Ethernet gateway) works today.
func dialBus(ctx context.Context, port string) (osdp.Transport, error) {
	if addr, ok := strings.CutPrefix(port, "tcp://"); ok {
		return osdp.DialTCP(ctx, addr)
	}
	return nil, fmt.Errorf("serial ports are not wired yet (build-order step 5); use tcp://host:port")
}
