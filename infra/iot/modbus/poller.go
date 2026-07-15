package modbus

import (
	"context"
	"fmt"
	"time"

	"github.com/mysayasan/kopiv2/infra/iot/codec"
	"github.com/mysayasan/kopiv2/infra/iot/sunspec"
)

// Mode selects how a Modbus device is decoded.
type Mode int

const (
	ModeSunSpec Mode = iota // self-describing: walk the model chain
	ModeRegMap              // non-SunSpec: use the site-authored register map
)

// DeviceConf describes one pollable Modbus device. This is the runtime shape of what a
// device_profile + iot_device will carry once the app integration lands (see MYIOTSAN_PLAN P8).
type DeviceConf struct {
	Key      string        // device key, used when emitting samples
	Endpoint string        // host:port
	Unit     byte          // Modbus unit id
	Mode     Mode          // SunSpec or manual map
	Base     int           // SunSpec base register; 0 = auto-discover (40000/50000/0)
	Map      RegisterMap   // used when Mode == ModeRegMap
	Timeout  time.Duration // per-operation timeout (default 3s)
}

func (d DeviceConf) to() time.Duration {
	if d.Timeout <= 0 {
		return 3 * time.Second
	}
	return d.Timeout
}

// PollOnce dials the device, reads it once, and returns the decoded samples. For a SunSpec device
// it also returns the nameplate (nil for a register-map device, which is not self-describing).
// The connection is opened and closed per poll: at a poll cadence of seconds this is negligible and
// avoids the stale-socket handling a long-lived connection would need.
func PollOnce(d DeviceConf) ([]codec.Sample, *sunspec.Common, error) {
	c, err := Dial(d.Endpoint, d.Unit, d.to())
	if err != nil {
		return nil, nil, err
	}
	defer c.Close()

	if d.Mode == ModeRegMap {
		s, err := d.Map.Read(c)
		return s, nil, err
	}

	base := d.Base
	var blocks []sunspec.Block
	if base == 0 {
		base, blocks, err = sunspec.Discover(c)
	} else {
		blocks, err = sunspec.Walk(c, base)
	}
	if err != nil {
		return nil, nil, err
	}
	var common *sunspec.Common
	for _, b := range blocks {
		if b.ID == 1 {
			cc := sunspec.DecodeCommon(b)
			common = &cc
		}
	}
	return sunspec.DecodeDevice(blocks), common, nil
}

// Run polls the device on its interval until ctx is cancelled, handing each batch of samples to
// emit. A failed poll is reported and retried on the next tick — a device that briefly drops off
// the bus must not kill the poller.
func Run(ctx context.Context, d DeviceConf, interval time.Duration, emit func(key string, s []codec.Sample), onErr func(error)) {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s, _, err := PollOnce(d)
			if err != nil {
				if onErr != nil {
					onErr(err)
				}
				continue
			}
			emit(d.Key, s)
		}
	}
}

// WriteConfirm performs a GUARDED write: it writes value to reg, then reads that same register back
// until it reports value or the timeout elapses.
//
// It NEVER re-issues the write. A Modbus write to an inverter or a battery is a physical action;
// re-sending it because a confirmation was slow is a SECOND physical action, and nothing at this
// layer can tell "the write was lost" from "the confirmation was lost". So on doubt it fails and a
// human decides — the same rule the actuation path already enforces for MQTT relays. Confirmation
// is by RE-READING, which is safe to repeat.
func WriteConfirm(d DeviceConf, reg int, value uint16, timeout time.Duration) error {
	c, err := Dial(d.Endpoint, d.Unit, d.to())
	if err != nil {
		return err
	}
	defer c.Close()

	if err := c.WriteSingle(reg, value); err != nil {
		return fmt.Errorf("write reg %d: %w", reg, err)
	}

	deadline := time.Now().Add(timeout)
	var last uint16
	for {
		rb, err := c.ReadHolding(reg, 1)
		if err == nil {
			last = rb[0]
			if last == value {
				return nil // confirmed by the device itself
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("write to reg %d not confirmed: wrote %d, read back %d", reg, value, last)
		}
		time.Sleep(200 * time.Millisecond)
	}
}
