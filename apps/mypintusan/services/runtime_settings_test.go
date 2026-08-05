package services

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

const siteKeyHex = "a0a1a2a3a4a5a6a7b0b1b2b3b4b5b6b7"

func seedDefaults() AccessSettings {
	return AccessSettings{
		Timezone: "Asia/Kuala_Lumpur", TickSeconds: 1, PINWindowSeconds: 15,
		Buses: []BusSettings{{
			Port: "tcp://127.0.0.1:4950", SlotMillis: 50, ReplyTimeoutMillis: 200,
			Readers: []ReaderSettings{{Address: 1, SCBK: siteKeyHex, RequireSecureChannel: true, Label: "Front"}},
		}},
	}
}

func newSettings(t *testing.T) IAccessSettingsService {
	t.Helper()
	s := newSQLiteStore(t)
	return NewAccessSettingsService(s.settingsRepo(), seedDefaults())
}

// TestFirstRunSeedsFromConfigThenDatabaseWins is the pattern mymatasan established: config.json is
// a SEED, not the source of truth. A door system is bought by a facilities manager, so once the app
// has booted, every value has to be reachable from a screen rather than a JSON file.
func TestFirstRunSeedsFromConfigThenDatabaseWins(t *testing.T) {
	svc := newSettings(t)
	ctx := context.Background()

	// First read seeds from the config defaults.
	got, err := svc.Get(ctx)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Timezone != "Asia/Kuala_Lumpur" || len(got.Buses) != 1 {
		t.Fatalf("first run did not seed from config: %+v", got)
	}

	// An edit lands in the database.
	got.Timezone = "Asia/Singapore"
	got.TickSeconds = 2
	if _, err := svc.Save(ctx, got, 7); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// A NEW service with the ORIGINAL config defaults must still read the saved value — this is
	// the whole point. If config.json won here, every restart would silently revert the operator.
	again := NewAccessSettingsService(svc.(*accessSettingsService).repo, seedDefaults())
	after, err := again.Get(ctx)
	if err != nil {
		t.Fatalf("Get after save: %v", err)
	}
	if after.Timezone != "Asia/Singapore" || after.TickSeconds != 2 {
		t.Errorf("config.json overrode the database: %+v", after)
	}
}

// TestSaveDoesNotWipeKeys is the sharpest edge in this file.
//
// The API redacts SCBK on read, so a UI that loads the settings screen and presses Save sends the
// key back EMPTY. Without carry-forward that silently wipes every site key and drops every door to
// cleartext — a catastrophic downgrade caused by a user doing nothing but clicking Save.
func TestSaveDoesNotWipeKeys(t *testing.T) {
	svc := newSettings(t)
	ctx := context.Background()

	loaded, err := svc.Get(ctx)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	// Simulate the round trip a UI performs: keys came back redacted, so they go back empty.
	roundTripped := loaded
	roundTripped.Buses[0].Readers[0].SCBK = ""
	roundTripped.Buses[0].Readers[0].Label = "Front door (renamed)"

	if _, err := svc.Save(ctx, roundTripped, 7); err != nil {
		t.Fatalf("Save: %v", err)
	}

	after, err := svc.Get(ctx)
	if err != nil {
		t.Fatalf("Get after save: %v", err)
	}
	if after.Buses[0].Readers[0].SCBK != siteKeyHex {
		t.Fatalf("saving the settings screen WIPED the site key (%q) — every door would drop to cleartext",
			after.Buses[0].Readers[0].SCBK)
	}
	if after.Buses[0].Readers[0].Label != "Front door (renamed)" {
		t.Errorf("the edit itself was lost: %q", after.Buses[0].Readers[0].Label)
	}
}

// TestKeyIsNeverSerialisedOut: a site key must not be readable from an API response. Anyone who can
// read it can decrypt the bus and impersonate a reader — the exact attack Secure Channel prevents.
func TestKeyIsNeverSerialisedOut(t *testing.T) {
	svc := newSettings(t)
	got, err := svc.Get(context.Background())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	payload, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(payload), siteKeyHex) {
		t.Fatalf("the site key appeared in an API payload: %s", payload)
	}
	// The UI still needs to know a key EXISTS, so it can show "encrypted" and offer Rekey.
	if !strings.Contains(string(payload), `"hasScbk":true`) {
		t.Errorf("payload does not tell the UI a key is installed: %s", payload)
	}
}

// TestDefaultKeyIsFlagged: a reader still on the published install-mode key is not secure at all,
// and the UI has to be able to nag about it.
func TestDefaultKeyIsFlagged(t *testing.T) {
	r := ReaderSettings{Address: 1, SCBK: defaultSCBKHex}
	payload, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(payload), `"usingDefaultKey":true`) {
		t.Errorf("a reader on SCBK-D was not flagged: %s", payload)
	}

	r.SCBK = siteKeyHex
	payload, _ = json.Marshal(r)
	if strings.Contains(string(payload), `"usingDefaultKey":true`) {
		t.Errorf("a rekeyed reader was flagged as default: %s", payload)
	}
}

// TestValidationRefusesBadEdits: these values are read at boot, so a saved-but-invalid setting is a
// controller that will not start. Refusing at save time is the difference between an error message
// and a site visit.
func TestValidationRefusesBadEdits(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*AccessSettings)
		want   string
	}{
		{"bad timezone", func(s *AccessSettings) { s.Timezone = "Mars/Olympus" }, "IANA"},
		{"two readers at one address", func(s *AccessSettings) {
			s.Buses[0].Readers = append(s.Buses[0].Readers,
				ReaderSettings{Address: 1, SCBK: siteKeyHex})
		}, "two readers at address"},
		{"duplicate bus", func(s *AccessSettings) {
			s.Buses = append(s.Buses, s.Buses[0])
		}, "listed twice"},
		{"short key", func(s *AccessSettings) { s.Buses[0].Readers[0].SCBK = "abcd" }, "32 hex"},
		{"encryption required with no key", func(s *AccessSettings) {
			s.Buses[0].Readers[0].SCBK = ""
			s.Buses[0].Readers[0].RequireSecureChannel = true
		}, "no security key"},
		{"address out of range", func(s *AccessSettings) { s.Buses[0].Readers[0].Address = 200 }, "0-127"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := seedDefaults()
			c.mutate(&in)
			if err := validateAccessSettings(normalizeAccessSettings(in)); err == nil {
				t.Fatalf("accepted an invalid edit")
			} else if !strings.Contains(err.Error(), c.want) {
				t.Errorf("error %q does not mention %q", err, c.want)
			}
		})
	}
}

// TestRequireSecureChannelWithCarriedKeyIsAccepted guards an interaction between two rules: a UI
// round trip sends the key back empty, and "requires encryption but has no key" is a validation
// error. Naively combined, renaming an encrypted reader would be REFUSED.
func TestRequireSecureChannelWithCarriedKeyIsAccepted(t *testing.T) {
	svc := newSettings(t)
	ctx := context.Background()

	loaded, err := svc.Get(ctx)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	loaded.Buses[0].Readers[0].SCBK = "" // redacted on the way out
	loaded.Buses[0].Readers[0].Label = "renamed"

	if _, err := svc.Save(ctx, loaded, 1); err != nil {
		t.Fatalf("renaming an encrypted reader was refused: %v", err)
	}
}

// TestResetRestoresTheConfigSeed is the recovery path. Without it, a bad settings edit would need a
// database edit to undo.
func TestResetRestoresTheConfigSeed(t *testing.T) {
	svc := newSettings(t)
	ctx := context.Background()

	edited, _ := svc.Get(ctx)
	edited.Timezone = "Asia/Singapore"
	edited.Buses = nil
	if _, err := svc.Save(ctx, edited, 1); err != nil {
		t.Fatalf("Save: %v", err)
	}

	back, err := svc.Reset(ctx, 1)
	if err != nil {
		t.Fatalf("Reset: %v", err)
	}
	if back.Timezone != "Asia/Kuala_Lumpur" || len(back.Buses) != 1 {
		t.Errorf("Reset did not restore the config seed: %+v", back)
	}
	if got, _ := svc.Get(ctx); got.Buses[0].Readers[0].SCBK != siteKeyHex {
		t.Error("Reset lost the seeded key")
	}
}

// TestNormalizeRepairsRatherThanRejects: a row written by an older version must be repaired on read,
// not turned into a controller that refuses to boot.
func TestNormalizeRepairsRatherThanRejects(t *testing.T) {
	got := normalizeAccessSettings(AccessSettings{
		Buses: []BusSettings{{Port: "  tcp://x  "}},
	})
	if got.Timezone != "Local" || got.TickSeconds != 1 || got.PINWindowSeconds != 15 {
		t.Errorf("defaults not filled: %+v", got)
	}
	if got.Buses[0].Port != "tcp://x" {
		t.Errorf("port not trimmed: %q", got.Buses[0].Port)
	}
	if got.Buses[0].SlotMillis != 50 || got.Buses[0].ReplyTimeoutMillis != 200 {
		t.Errorf("bus defaults not filled: %+v", got.Buses[0])
	}
}
