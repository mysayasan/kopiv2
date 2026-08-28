package services

import (
	"context"
	"sync"
	"time"

	"github.com/mysayasan/kopiv2/apps/myiotsan/entities"
	"github.com/mysayasan/kopiv2/infra/iot/codec"
	iotmqtt "github.com/mysayasan/kopiv2/infra/iot/mqtt"
)

// Ingest is the hot path: a payload arrives, and this decides what it means, whether it is
// worth storing, and whether it should wake somebody up.
//
//	broker -> decode (profile bindings) -> deadband -> batched write
//	                                            \
//	                                             -> rule evaluation -> alert
//
// Everything here is built to keep the database OFF this path. A publish must not become a
// query: profile bindings are cached, liveness writes are throttled, readings are batched, and
// the deadband drops most samples before they ever reach the queue. The one thing that is
// allowed to be slow is an alert, because an alert is rare and matters.
type Ingest struct {
	devices *DeviceService
	profile *ProfileService
	gate    *DeadbandGate
	writer  *ReadingWriter
	rules   *RuleService
	enroll  *Enrollment
	twin    *CommandService
	flows   *FlowRuntime
	logf    func(format string, args ...any)

	// bindings caches each profile's decoded key list. Reading the telemetry keys from the
	// database on every message would put a query on the hot path — the whole thing this
	// pipeline is arranged to avoid. Invalidated when a profile is edited.
	mu       sync.RWMutex
	bindings map[int64]*profileBindings

	// stats
	statsMu    sync.Mutex
	received   int64
	decoded    int64
	stored     int64
	suppressed int64
}

type profileBindings struct {
	binds []codec.Binding
	rules map[string]GateRule
	keys  map[string]*entities.TelemetryKey
	raw   bool
}

func NewIngest(devices *DeviceService, profile *ProfileService, gate *DeadbandGate, writer *ReadingWriter, rules *RuleService, logf func(string, ...any)) *Ingest {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &Ingest{
		devices:  devices,
		profile:  profile,
		gate:     gate,
		writer:   writer,
		rules:    rules,
		logf:     logf,
		bindings: map[int64]*profileBindings{},
	}
}

// InvalidateProfile drops a cached profile, so an edited deadband takes effect on the next
// message rather than on the next restart.
func (i *Ingest) InvalidateProfile(profileId int64) {
	i.mu.Lock()
	defer i.mu.Unlock()
	delete(i.bindings, profileId)
}

// ForgetDevice drops a device's deadband baselines. It must be called when a device is deleted,
// for two reasons the gate's own comment states and nothing used to act on.
//
// The map is the one unbounded structure in the ingest path — Size() is exposed to the metrics
// endpoint precisely so an operator can watch it — and without this it could only ever grow: a
// site that replaces sensors over ten years accumulates a baseline for every device it has ever
// had. And on a database that hands out row ids as max+1, a replacement device can land on a
// deleted one's id, at which point its first reading is compared against a stranger's last value
// and can be suppressed as "unchanged" — the one sample that must never be dropped.
func (i *Ingest) ForgetDevice(deviceId int64) {
	i.gate.Forget(deviceId)
}

// SetEnrollment wires the enrollment window in, so a quarantined client's payloads become
// candidates instead of telemetry.
func (i *Ingest) SetEnrollment(e *Enrollment) { i.enroll = e }

// SetTwin wires actuation in. Every reading updates the twin's REPORTED half, which is what
// confirms a command: "we published a message" is not "the relay closed" — only the device
// saying so is.
func (i *Ingest) SetTwin(c *CommandService) { i.twin = c }

// SetFlows attaches the flow runtime. Like SetTwin, it is wired after construction (the runtime is
// built later in app.go) and before the broker starts, so no reading is missed. A nil runtime means
// the flow engine is simply not consulted.
func (i *Ingest) SetFlows(f *FlowRuntime) { i.flows = f }

// Handle processes one payload. It satisfies mqtt.MessageHandler and is also the entry point
// for the HTTP ingest route, so a device that cannot speak MQTT has the same pipeline behind it.
func (i *Ingest) Handle(ctx context.Context, p iotmqtt.Principal, clientId, topic string, payload []byte) {
	// THE QUARANTINE. An enrolling client is a stranger that presented an open window's key. What
	// it says is recorded as a CANDIDATE and goes no further: no telemetry row, no rule
	// evaluation, no effect on any chart or alert. This early return IS the security boundary —
	// everything below it treats the payload as trusted sensor data, and a stranger must never
	// reach it.
	if p.Enrolling {
		if i.enroll != nil {
			i.enroll.Observe(ctx, clientId, topic, payload)
		}
		return
	}

	deviceId := p.DeviceId
	now := time.Now()
	nowMs := now.UnixMilli()
	nowSec := now.Unix()

	i.bump(&i.received)

	// Liveness FIRST, and unconditionally. A device faithfully reporting an unchanged value is
	// alive, and if this sat behind the deadband a perfectly healthy stable sensor would look
	// dead to the offline rule. The database write behind it is throttled.
	i.devices.TouchSeen(ctx, deviceId, nowSec)

	dev, err := i.devices.GetById(ctx, deviceId)
	if err != nil || dev == nil {
		return
	}
	if dev.ProfileId <= 0 {
		// A device with no profile can connect and is provably alive, but nothing it says can
		// be decoded. That is a configuration gap, not an error to spam about per message.
		return
	}

	binds, err := i.bindingsFor(ctx, dev.ProfileId)
	if err != nil || binds == nil || len(binds.binds) == 0 {
		return
	}

	var samples []codec.Sample
	if binds.raw {
		if len(binds.binds) == 1 {
			if s, ok := codec.DecodeRaw(payload, binds.binds[0]); ok {
				samples = []codec.Sample{s}
			}
		}
	} else {
		samples, err = codec.DecodeJSON(payload, binds.binds)
		if err != nil {
			// A malformed payload is a device problem worth seeing, but at a bounded rate.
			i.logf("device %q sent an undecodable payload on %q: %v", dev.DeviceKey, topic, err)
			return
		}
	}
	if len(samples) == 0 {
		return
	}
	i.bump(&i.decoded)

	i.handleSamples(ctx, dev, binds, samples, nowMs, nowSec)
}

// handleSamples runs a decoded batch through the deadband, storage, rules and twin. It is the BACK
// HALF of the pipeline — everything past "we have samples" — and it is deliberately protocol- and
// codec-blind: an MQTT payload and a Modbus poll both arrive here as a []codec.Sample and are
// treated identically. That is what lets a polled inverter ride the exact same deadband, storage,
// rule and command-confirmation machinery a published sensor does.
func (i *Ingest) handleSamples(ctx context.Context, dev *entities.IotDevice, binds *profileBindings, samples []codec.Sample, nowMs, nowSec int64) {
	deviceId := dev.Id
	for _, s := range samples {
		key := binds.keys[s.Key]
		if key == nil {
			continue
		}
		// A key declared numeric whose payload was not a number (Zigbee2MQTT really does send
		// "unavailable") yields no sample at all rather than a fabricated 0 — see codec.coerce.
		if isNumericType(key.DataType) && !s.IsNum {
			continue
		}

		rule := binds.rules[s.Key]
		if !i.gate.Admit(deviceId, s.Key, rule, s.Num, s.Str, nowMs) {
			i.bump(&i.suppressed)
			// NOTE: a suppressed sample is still evaluated by the rules below. A deadband is a
			// STORAGE decision, not a detection one — a value that sat 3 degrees over the limit
			// without moving must still fire, and gating rules behind the deadband would mean a
			// perfectly steady overheat is never alerted on. That would be the worst possible
			// bug in this app, so the rule call is deliberately outside the admit branch.
		} else {
			i.writer.Enqueue(entities.DeviceReading{
				DeviceId: deviceId,
				Key:      s.Key,
				Ts:       nowMs,
				Num:      s.Num,
				Str:      s.Str,
				Suspect:  isSuspect(key, s),
			})
			i.bump(&i.stored)
		}

		if i.rules != nil {
			i.rules.OnReading(ctx, dev, s.Key, s.Num, nowSec)
		}
		// The flow engine, alongside the rules — same reading, a parallel consumer. It only
		// ENQUEUES here (execution is on its own worker), so a flow can never slow ingest.
		if i.flows != nil {
			i.flows.OnReading(ctx, dev, s.Key, s.Num, nowSec)
		}
		// The twin's reported half. This is what closes the loop on a command.
		if i.twin != nil {
			i.twin.OnReported(ctx, deviceId, s.Key, s.Num, nowSec)
		}
	}
}

// HandlePolled feeds a batch of samples produced by a POLL driver (Modbus, and later OPC-UA) through
// the same back half as an MQTT publish. The driver has already decoded to codec.Sample, so there is
// no payload to parse — and no quarantine to apply: a polled device is one the operator configured
// and the app dialled OUT to, not a stranger that dialled in. A sample whose key the profile does
// not declare is dropped exactly as an unbound MQTT field would be.
func (i *Ingest) HandlePolled(ctx context.Context, dev *entities.IotDevice, samples []codec.Sample) {
	if dev == nil || dev.ProfileId <= 0 {
		return
	}
	now := time.Now()
	nowMs, nowSec := now.UnixMilli(), now.Unix()

	i.bump(&i.received)
	// Liveness first and unconditionally, the same rule as the MQTT path: a device polling an
	// unchanged value is alive and must not look dead to the offline rule behind the deadband.
	i.devices.TouchSeen(ctx, dev.Id, nowSec)

	binds, err := i.bindingsFor(ctx, dev.ProfileId)
	if err != nil || binds == nil || len(binds.keys) == 0 {
		return
	}
	if len(samples) == 0 {
		return
	}
	i.bump(&i.decoded)
	i.handleSamples(ctx, dev, binds, samples, nowMs, nowSec)
}

// bindingsFor returns a profile's decode plan, caching it.
func (i *Ingest) bindingsFor(ctx context.Context, profileId int64) (*profileBindings, error) {
	i.mu.RLock()
	cached, ok := i.bindings[profileId]
	i.mu.RUnlock()
	if ok {
		return cached, nil
	}

	keys, err := i.profile.KeysFor(ctx, profileId)
	if err != nil {
		return nil, err
	}
	detail, err := i.profile.Detail(ctx, profileId)
	if err != nil {
		return nil, err
	}

	b := &profileBindings{
		binds: make([]codec.Binding, 0, len(keys)),
		rules: make(map[string]GateRule, len(keys)),
		keys:  make(map[string]*entities.TelemetryKey, len(keys)),
		raw:   detail.Profile != nil && detail.Profile.PayloadFormat == "raw",
	}
	for _, k := range keys {
		numeric := isNumericType(k.DataType)
		b.binds = append(b.binds, codec.Binding{Key: k.Key, Path: k.JsonPath, Numeric: numeric})
		b.rules[k.Key] = GateRule{
			Deadband:         k.Deadband,
			HeartbeatSeconds: k.HeartbeatSeconds,
			Numeric:          numeric,
		}
		b.keys[k.Key] = k
	}

	i.mu.Lock()
	i.bindings[profileId] = b
	i.mu.Unlock()
	return b, nil
}

// isSuspect flags a reading outside its key's declared range. It is stored anyway: a sensor
// reporting -3000 degrees is broken, and dropping the evidence would hide the failure.
func isSuspect(key *entities.TelemetryKey, s codec.Sample) bool {
	if !s.IsNum || key.Min == 0 && key.Max == 0 {
		return false
	}
	return s.Num < key.Min || s.Num > key.Max
}

func isNumericType(t string) bool {
	return t == "" || t == "number" || t == "bool"
}

func (i *Ingest) bump(counter *int64) {
	i.statsMu.Lock()
	*counter++
	i.statsMu.Unlock()
}

// IngestStats is what the ingest path has done since boot. `Suppressed` is the deadband
// earning its keep: the ratio of suppressed to stored IS the storage design, and if it ever
// falls near zero the deadbands are mistuned and the database is about to be in trouble.
type IngestStats struct {
	Received   int64 `json:"received"`
	Decoded    int64 `json:"decoded"`
	Stored     int64 `json:"stored"`
	Suppressed int64 `json:"suppressed"`
	Written    int64 `json:"written"`
	Dropped    int64 `json:"dropped"`
	Queued     int   `json:"queued"`
	Series     int   `json:"series"`
}

func (i *Ingest) Stats() IngestStats {
	i.statsMu.Lock()
	s := IngestStats{
		Received:   i.received,
		Decoded:    i.decoded,
		Stored:     i.stored,
		Suppressed: i.suppressed,
	}
	i.statsMu.Unlock()
	s.Written, s.Dropped, s.Queued = i.writer.Stats()
	s.Series = i.gate.Size()
	return s
}
