package firstboot

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// distConfigDir holds the config files the packaged installers ship.
const distConfigDir = "../../../deploy/dist"

// A shipped config that pins setup.address overrides the code default, so the two can
// drift apart silently — and the symptom is subtle: the wizard quietly lands on a random
// port and the address the operator was told to use is simply wrong. This caught exactly
// that on the first live run, where a stale 9080 in the shipped file sent every install
// down the fallback path.
func TestShippedConfigsAgreeWithTheDefaultSetupAddress(t *testing.T) {
	entries, err := os.ReadDir(distConfigDir)
	if err != nil {
		t.Skipf("no packaged configs to check: %v", err)
	}
	checked := 0
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		path := filepath.Join(distConfigDir, entry.Name())
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		block, err := readSetupBlock(raw)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if block.Address == "" {
			continue
		}
		checked++
		if block.Address != defaultSetupAddr {
			t.Errorf("%s pins setup.address %q but the default is %q — an operator following the shipped file would look at the wrong port",
				entry.Name(), block.Address, defaultSetupAddr)
		}
	}
	if checked == 0 {
		t.Skip("no shipped config pins setup.address")
	}
}

// A shipped config that carries a setup block must carry a usable one: an install whose
// wizard is marked complete before anyone has run it boots straight into whatever
// placeholder settings the file happens to hold.
func TestShippedConfigsThatOfferSetupDoNotPreMarkItComplete(t *testing.T) {
	entries, err := os.ReadDir(distConfigDir)
	if err != nil {
		t.Skipf("no packaged configs to check: %v", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		path := filepath.Join(distConfigDir, entry.Name())
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		// Only files that opted in are checked: a config with no setup block is
		// deliberately treated as already configured.
		var probe struct {
			Setup *json.RawMessage `json:"setup"`
		}
		if err := json.Unmarshal(raw, &probe); err != nil || probe.Setup == nil {
			continue
		}
		block, err := readSetupBlock(raw)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if block.Completed == nil {
			t.Errorf("%s has a setup block with no \"completed\" key, which reads as already set up", entry.Name())
			continue
		}
		if *block.Completed {
			t.Errorf("%s ships setup.completed=true, so a fresh install never sees the wizard", entry.Name())
		}
		if block.AllowRemote {
			t.Errorf("%s ships setup.allowRemote=true, exposing an unauthenticated setup page on the network by default", entry.Name())
		}
	}
}
