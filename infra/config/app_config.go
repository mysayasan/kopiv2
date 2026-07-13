package config

import (
	"encoding/json"
	"fmt"
	"os"
)

// LoadAppConfiguration reads config.json into the shared model and retains the raw
// document, so an app that owns its own config blocks can decode them from the same bytes.
//
// A malformed document is now an ERROR. This previously called Decode and DISCARDED the
// error, so a single trailing comma produced a silently all-zero config: the app booted on
// defaults, ignored everything the operator had configured, and said nothing about it.
func LoadAppConfiguration(file string) (*AppConfigModel, error) {
	raw, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}

	config := AppConfigModel{}
	if err := json.Unmarshal(raw, &config); err != nil {
		return nil, fmt.Errorf("parse %s: %w", file, err)
	}
	config.raw = raw
	return &config, nil
}

// Raw returns the config document this model was decoded from. It is what lets an app
// decode its OWN blocks out of the same file without the shared model having to know they
// exist — the seam that keeps camera/vision config out of infra and off every other app.
//
// It is nil when the model was not built by LoadAppConfiguration (in a test, say), in which
// case an app config decodes to its zero value — the correct default.
func (c *AppConfigModel) Raw() []byte {
	if c == nil {
		return nil
	}
	return c.raw
}
