package services

import (
	"math"
	"sync"
)

// The deadband gate: the single mechanism that makes SQLite a viable telemetry store.
//
// 100 devices x 10 keys at 1 Hz is 1,000 rows/second, and SQLite will not absorb that — not
// with batching, not with WAL, not with anything short of a different database. But almost
// none of those samples SAY anything: a room is 21.4 degrees for an hour, a door is shut all
// night, a leak sensor is dry for its entire service life. The gate persists a sample only
// when it actually MOVES, which collapses the write rate by one to two orders of magnitude
// on real building sensors and loses nothing an operator would later ask about.
//
// Three ways through the gate, and each exists for a reason:
//
//	1. The value moved by more than the key's deadband.       (something happened)
//	2. The heartbeat elapsed.                                 (a flat line is provably alive,
//	                                                           and a chart has a point to draw)
//	3. It is the first sample seen for this device+key.       (there is nothing to compare to,
//	                                                           and the first value is a fact)
//
// A deadband of 0 means "store every sample", which is correct for a door contact — every
// transition matters — and disastrous for a temperature probe, where sensor noise in the last
// decimal place would write a row a second forever.

// gateKey identifies one series: one key on one device.
type gateKey struct {
	deviceId int64
	key      string
}

type gateState struct {
	value    float64
	str      string
	lastAtMs int64
	hasValue bool
}

// DeadbandGate decides which samples are worth persisting. It is in-memory and per-process,
// which is the right trade: losing it on restart costs exactly one extra row per series (the
// first sample after boot passes as "first seen"), and that is a far better failure than a
// database read on every single incoming packet.
type DeadbandGate struct {
	mu    sync.Mutex
	state map[gateKey]gateState
}

func NewDeadbandGate() *DeadbandGate {
	return &DeadbandGate{state: map[gateKey]gateState{}}
}

// GateRule is the part of a TelemetryKey the gate cares about.
type GateRule struct {
	Deadband         float64
	HeartbeatSeconds int
	// Numeric distinguishes a value compared by magnitude from one compared by equality.
	Numeric bool
}

// Admit reports whether a sample should be persisted, and records it as the new baseline if
// so. nowMs is passed in rather than read from the clock so this is testable and so a batch
// of samples from one payload share a timestamp.
func (g *DeadbandGate) Admit(deviceId int64, key string, rule GateRule, num float64, str string, nowMs int64) bool {
	k := gateKey{deviceId: deviceId, key: key}

	g.mu.Lock()
	defer g.mu.Unlock()

	prev, seen := g.state[k]
	admit := false

	switch {
	case !seen || !prev.hasValue:
		// First sample for this series. There is nothing to compare against, and the first
		// value is itself information.
		admit = true

	case rule.HeartbeatSeconds > 0 && nowMs-prev.lastAtMs >= int64(rule.HeartbeatSeconds)*1000:
		// The value may not have moved, but silence is indistinguishable from death. A
		// periodic row proves the device is alive and gives a chart something to draw.
		admit = true

	case !rule.Numeric:
		// A string reading changes or it does not; there is no "how much".
		admit = str != prev.str

	case rule.Deadband <= 0:
		// No deadband configured: every distinct value is a row. Correct for a door contact,
		// where a 0 -> 1 transition IS the event. Note this still suppresses an identical
		// repeat, which a device republishing its state every second will send.
		admit = num != prev.value

	default:
		admit = math.Abs(num-prev.value) >= rule.Deadband
	}

	if admit {
		g.state[k] = gateState{value: num, str: str, lastAtMs: nowMs, hasValue: true}
	}
	return admit
}

// Forget drops a device's series, so a deleted or re-provisioned device does not leave its
// last values behind to be compared against by a future device with the same id.
func (g *DeadbandGate) Forget(deviceId int64) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for k := range g.state {
		if k.deviceId == deviceId {
			delete(g.state, k)
		}
	}
}

// Size reports how many series the gate is tracking. Used by the metrics endpoint — this map
// is the one unbounded structure in the ingest path, so it is worth being able to watch it.
func (g *DeadbandGate) Size() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.state)
}
