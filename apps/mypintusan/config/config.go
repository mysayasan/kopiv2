// Package config decodes mypintusan's own slice of config.json — the parts no other app in the
// suite needs, kept out of the shared AppConfigModel that every app would otherwise carry.
package config

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Config is the access-control block.
type Config struct {
	Access AccessConfig `json:"access"`
	Buses  []BusConfig  `json:"buses"`
}

// AccessConfig is site-wide door behaviour.
type AccessConfig struct {
	// Timezone is the site's local zone, as an IANA name ("Asia/Kuala_Lumpur").
	//
	// It is CONFIGURED rather than taken from the host clock because schedules and the holiday
	// calendar are local concepts: a night shift that runs 22:00–06:00 and a public holiday are
	// both wrong if evaluated in UTC, and a controller in a rack somewhere may well be set to it.
	Timezone string `json:"timezone"`

	// TickSeconds drives the door state machines' relock and held-open timers. It bounds how LATE
	// an alarm can be, not whether it fires: a 30s held-open threshold with a 1s tick alarms
	// somewhere in 30–31s. Coarsening it to save wakeups directly delays every door alarm on site.
	TickSeconds int `json:"tickSeconds"`

	// PINWindowSeconds is how long a keypad entry stays available to be paired with a card. Short
	// on purpose: a PIN left buffered would be picked up by whoever badges next.
	PINWindowSeconds int `json:"pinWindowSeconds"`

	// Offline marks this controller as running from a cached replica rather than the live
	// database. It exists so a door controller separated from its database still works — within
	// the per-door-class TTL, and never as "allow everything".
	Offline bool `json:"offline"`
}

// BusConfig is one RS-485 segment (or one TCP endpoint, for the simulator).
type BusConfig struct {
	// Port is "COM3", "/dev/ttyUSB0", or "tcp://host:port" against tools/osdp-sim.
	Port string `json:"port"`
	// Readers are the PD addresses to poll on this bus.
	Readers []ReaderConfig `json:"readers"`
	// SlotMillis is the minimum gap between bus transactions. Per-PD poll cadence is
	// SlotMillis × number of readers, so this and the reader count together decide whether the
	// 100–200 ms target per reader is met.
	SlotMillis int `json:"slotMillis"`
	// ReplyTimeoutMillis is how long a reader gets to answer.
	ReplyTimeoutMillis int `json:"replyTimeoutMillis"`
	// StatusMillis is how often an online reader is asked for its tamper state and its
	// door-position contact instead of being given a bare poll. A PD volunteers neither, so a CP
	// that never asks is blind to both: raising it trades alarm latency for bus headroom on a long
	// multidrop segment, and 0 takes the driver's default.
	StatusMillis int `json:"statusMillis"`
}

// ReaderConfig binds one PD address to its Secure Channel policy.
type ReaderConfig struct {
	Address int `json:"address"`
	// SCBK is the 16-byte base key as hex. Empty runs the reader in the clear, which is only
	// acceptable on an `interior` door.
	SCBK string `json:"scbk"`
	// RequireSecureChannel takes the reader OUT OF SERVICE if a session cannot be established or
	// drops, rather than falling back to cleartext. It is a DOOR policy expressed here because the
	// bus is where the session lives; the door's own RequireSecureChannel column is what the
	// decision path enforces.
	RequireSecureChannel bool `json:"requireSecureChannel"`
}

// Load decodes the app block, filling defaults. A nil or empty payload yields a usable config with
// no buses — which is exactly what a fresh install has before anyone has wired a reader.
func Load(raw []byte) (*Config, error) {
	cfg := &Config{}
	if len(raw) > 0 {
		var envelope struct {
			Access *AccessConfig `json:"access"`
			Buses  []BusConfig   `json:"buses"`
		}
		if err := json.Unmarshal(raw, &envelope); err != nil {
			return nil, fmt.Errorf("decode mypintusan config: %w", err)
		}
		if envelope.Access != nil {
			cfg.Access = *envelope.Access
		}
		cfg.Buses = envelope.Buses
	}

	if strings.TrimSpace(cfg.Access.Timezone) == "" {
		cfg.Access.Timezone = "Local"
	}
	if cfg.Access.TickSeconds <= 0 {
		cfg.Access.TickSeconds = 1
	}
	if cfg.Access.PINWindowSeconds <= 0 {
		cfg.Access.PINWindowSeconds = 15
	}
	for i := range cfg.Buses {
		if cfg.Buses[i].SlotMillis <= 0 {
			cfg.Buses[i].SlotMillis = 50
		}
		if cfg.Buses[i].ReplyTimeoutMillis <= 0 {
			cfg.Buses[i].ReplyTimeoutMillis = 200
		}
		if cfg.Buses[i].StatusMillis <= 0 {
			cfg.Buses[i].StatusMillis = 1000
		}
	}
	return cfg, nil
}

// Location resolves the configured timezone.
//
// A bad zone name is an ERROR, not a silent fall back to UTC. Falling back would shift every
// schedule on the site by the offset and deny people at the edges of their shift for reasons
// nobody could reproduce — far worse than refusing to start.
func (c *Config) Location() (*time.Location, error) {
	name := strings.TrimSpace(c.Access.Timezone)
	if name == "" || name == "Local" {
		return time.Local, nil
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, fmt.Errorf("access.timezone %q: %w", name, err)
	}
	return loc, nil
}

// Tick returns the door-timer interval.
func (c *Config) Tick() time.Duration {
	return time.Duration(c.Access.TickSeconds) * time.Second
}

// PINWindow returns the keypad pairing window.
func (c *Config) PINWindow() time.Duration {
	return time.Duration(c.Access.PINWindowSeconds) * time.Second
}
