// Package config owns myiotsan's own configuration blocks.
//
// It exists because of the phase-C per-app config seam (apphost.AppConfigDecoder): the shared
// AppConfigModel carries what every app needs (server, db, security, auth), and each app
// decodes its OWN blocks from the same raw document. myiotsan's blocks stay TOP-LEVEL in
// config.json — they are not nested under an "app" key — so a deployed config file does not
// have to be rewritten to gain them.
package config

import (
	"encoding/json"
	"strings"
)

// Config is myiotsan's slice of config.json.
type Config struct {
	MQTT      MQTTConfig      `json:"mqtt"`
	Telemetry TelemetryConfig `json:"telemetry_store"`
}

// MQTTConfig configures the embedded broker.
type MQTTConfig struct {
	// Enabled turns the broker on. A site that already runs Mosquitto or EMQX can turn it off
	// and point its devices at that instead — the ingest pipeline does not care where a payload
	// came from.
	Enabled bool `json:"enabled"`
	// Addr is the plaintext listener. 1883 is the standard MQTT port.
	Addr string `json:"addr"`
}

// TelemetryConfig tunes the storage path — the write batching and how long data is kept.
//
// These are the knobs that decide whether the appliance keeps up. They are configurable
// because the right values depend on the hardware and on how chatty the estate is, and the
// operator is the only one who knows that.
type TelemetryConfig struct {
	// BatchSize and FlushMs shape the write-behind batcher: how many rows go into one
	// transaction, and how long a reading may wait for company.
	BatchSize int `json:"batchSize"`
	FlushMs   int `json:"flushMs"`
	// QueueSize is the buffer depth before load is shed. A full queue drops readings rather
	// than stalling the broker on the disk.
	QueueSize int `json:"queueSize"`
	// RawRetentionDays is how long individual readings survive.
	RawRetentionDays int `json:"rawRetentionDays"`
	// RollupRetentionDays is how long the downsampled buckets survive. LONGER than the raw
	// rows on purpose: the rollups are what make last summer comparable to this one, at a
	// fraction of the size.
	RollupRetentionDays int `json:"rollupRetentionDays"`
}

// Load decodes myiotsan's blocks from the raw config document, then applies defaults.
//
// A config file with none of these blocks is valid and yields a working appliance — the
// defaults are the shipped behaviour, not a degraded one.
func Load(raw []byte) (*Config, error) {
	cfg := &Config{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, cfg); err != nil {
			return nil, err
		}
	}
	cfg.normalize()
	return cfg, nil
}

func (c *Config) normalize() {
	if strings.TrimSpace(c.MQTT.Addr) == "" {
		c.MQTT.Addr = "0.0.0.0:1883"
	}
	if c.Telemetry.BatchSize <= 0 {
		c.Telemetry.BatchSize = 200
	}
	if c.Telemetry.FlushMs <= 0 {
		c.Telemetry.FlushMs = 250
	}
	if c.Telemetry.QueueSize <= 0 {
		c.Telemetry.QueueSize = 8192
	}
	if c.Telemetry.RawRetentionDays <= 0 {
		c.Telemetry.RawRetentionDays = 30
	}
	if c.Telemetry.RollupRetentionDays <= 0 {
		c.Telemetry.RollupRetentionDays = 400
	}
}
