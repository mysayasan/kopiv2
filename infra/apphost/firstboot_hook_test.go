package apphost

import "testing"

// Every capability the host hands the wizard is optional to firstboot and degrades
// silently when nil: a missing Browser means no browser opens and nothing is logged, a
// missing probe means "Test connection" reports it cannot verify. That is invisible at
// runtime — the first live run of this feature shipped with Browser unset and the only
// symptom was a banner quietly missing one line — so the wiring is asserted here.
func TestFirstBootOptionsCarryEveryCapability(t *testing.T) {
	opts := firstBootOptions("testapp", "config.json", "data")

	if opts.AppName != "testapp" {
		t.Fatalf("AppName = %q", opts.AppName)
	}
	if opts.ConfigPath != "config.json" {
		t.Fatalf("ConfigPath = %q", opts.ConfigPath)
	}
	if opts.DataDir != "data" {
		t.Fatalf("DataDir = %q", opts.DataDir)
	}
	if opts.Logf == nil {
		t.Error("Logf is nil — the wizard's progress would go nowhere")
	}
	if opts.Browser == nil {
		t.Error("Browser is nil — the setup page would never open on its own")
	}
	if opts.ProbeDB == nil {
		t.Error("ProbeDB is nil — the database Test connection button would report it cannot verify")
	}
	if opts.ProbeCache == nil {
		t.Error("ProbeCache is nil — the cache Test connection button would report it cannot verify")
	}
}
