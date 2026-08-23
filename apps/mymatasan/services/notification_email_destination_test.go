package services

import (
	"strings"
	"testing"

	"github.com/mysayasan/kopiv2/domain/notification"
)

func TestNormalizeEmailRecipients(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{"splits a pasted list", []string{"a@corp.test, b@corp.test"}, []string{"a@corp.test", "b@corp.test"}},
		{"trims and drops blanks", []string{" a@corp.test ", "", "   "}, []string{"a@corp.test"}},
		{"dedups case-insensitively", []string{"Ops@corp.test", "ops@corp.test"}, []string{"Ops@corp.test"}},
		{"empty becomes nil", []string{"", " "}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeEmailRecipients(tc.in)
			if strings.Join(got, "|") != strings.Join(tc.want, "|") {
				t.Errorf("= %v, want %v", got, tc.want)
			}
		})
	}
}

func TestLooksLikeEmail(t *testing.T) {
	valid := []string{"ops@corp.test", "a.b+tag@sub.corp.test", "guard@nvr.internal.local"}
	for _, v := range valid {
		if !looksLikeEmail(v) {
			t.Errorf("%q rejected but should be accepted", v)
		}
	}
	// The CR/LF and separator cases are the ones that matter: they are what keeps
	// a recipient field from opening a new mail header.
	invalid := []string{"", "ops", "ops@", "@corp.test", "ops@corp", "a@b.test\r\nBcc: x@y.test",
		"a@b.test, c@d.test", "a b@corp.test", "a@b@corp.test", "ops@corp.test."}
	for _, v := range invalid {
		if looksLikeEmail(v) {
			t.Errorf("%q accepted but should be rejected", v)
		}
	}
}

func TestValidateEmailDestination(t *testing.T) {
	t.Run("enabled with no recipients is rejected", func(t *testing.T) {
		err := validateDestinations([]NotificationDestination{{
			Name: "Ops mail", Type: DestinationTypeEmail, Enabled: true,
		}})
		if err == nil {
			t.Fatal("an enabled email destination with no recipient was accepted")
		}
		if !strings.Contains(err.Error(), "recipient") {
			t.Errorf("error does not name the cause: %v", err)
		}
	})

	t.Run("disabled with no recipients is allowed", func(t *testing.T) {
		// An operator must be able to save a half-built destination while it is
		// switched off, exactly as the other types allow.
		if err := validateDestinations([]NotificationDestination{{
			Name: "Ops mail", Type: DestinationTypeEmail, Enabled: false,
		}}); err != nil {
			t.Errorf("disabled destination rejected: %v", err)
		}
	})

	t.Run("a malformed recipient is rejected even when disabled", func(t *testing.T) {
		// Stored-then-enabled-later is the dangerous path: a bad address must never
		// reach the store, or it becomes a header-injection attempt at send time.
		err := validateDestinations([]NotificationDestination{{
			Name: "Ops mail", Type: DestinationTypeEmail, Enabled: false,
			Email: NotificationEmailSettings{To: []string{"ops@corp.test\r\nBcc: attacker@evil.test"}},
		}})
		if err == nil {
			t.Fatal("a CR/LF-bearing recipient was accepted")
		}
	})

	t.Run("valid destination passes", func(t *testing.T) {
		if err := validateDestinations([]NotificationDestination{{
			Name: "Ops mail", Type: DestinationTypeEmail, Enabled: true,
			Email: NotificationEmailSettings{To: []string{"ops@corp.test"}, SubjectPrefix: "[Site A]"},
		}}); err != nil {
			t.Errorf("valid email destination rejected: %v", err)
		}
	})
}

// TestNotificationChannelConfigMapsEmail asserts the persisted settings reach the
// delivery layer intact — including that the snapshot decision comes from the
// destination-wide SnapshotMode rather than an email-only toggle.
func TestNotificationChannelConfigMapsEmail(t *testing.T) {
	settings := NotificationSettings{
		Smtp: NotificationSmtpSettings{
			Enabled: true, Host: "relay.corp.test", Port: 587,
			From: "nvr@corp.test", Username: "nvr", Password: "s3cret", UseStartTls: true,
		},
		Destinations: []NotificationDestination{
			{
				Id: "d1", Name: "Ops mail", Type: DestinationTypeEmail, Enabled: true,
				MinSeverity: "warning", SnapshotMode: SnapshotModeInline,
				Email: NotificationEmailSettings{To: []string{"ops@corp.test"}, SubjectPrefix: "[Site A]"},
			},
			{
				Id: "d2", Name: "Link only", Type: DestinationTypeEmail, Enabled: true,
				SnapshotMode: SnapshotModeLink,
				Email:        NotificationEmailSettings{To: []string{"noc@corp.test"}},
			},
			{
				Id: "d3", Name: "Disabled", Type: DestinationTypeEmail, Enabled: false,
				Email: NotificationEmailSettings{To: []string{"off@corp.test"}},
			},
		},
	}

	cfg := notificationChannelConfig(settings)

	if cfg.Smtp.Host != "relay.corp.test" || cfg.Smtp.Port != 587 || !cfg.Smtp.UseStartTls {
		t.Errorf("relay not mapped: %+v", cfg.Smtp)
	}
	if cfg.Smtp.Password != "s3cret" {
		t.Error("relay password did not reach the delivery layer")
	}
	if len(cfg.Destinations) != 2 {
		t.Fatalf("expected the disabled destination to be dropped, got %d", len(cfg.Destinations))
	}

	d1 := cfg.Destinations[0]
	if d1.Type != "email" || strings.Join(d1.Email.To, ",") != "ops@corp.test" {
		t.Errorf("email destination not mapped: %+v", d1)
	}
	if d1.Email.SubjectPrefix != "[Site A]" {
		t.Errorf("subject prefix = %q", d1.Email.SubjectPrefix)
	}
	if d1.MinSeverity != notification.Warning {
		t.Errorf("severity floor = %q", d1.MinSeverity)
	}
	if !d1.Email.IncludeSnapshot {
		t.Error("inline snapshot mode did not enable the attachment")
	}
	if cfg.Destinations[1].Email.IncludeSnapshot {
		t.Error("link snapshot mode still attached the image")
	}
}

// TestNormalizeNotificationSettingsDefaultsSmtpPort keeps an unset or nonsense
// port from producing a relay the sender cannot dial.
func TestNormalizeNotificationSettingsDefaultsSmtpPort(t *testing.T) {
	for _, port := range []int{0, -1, 70000} {
		got := normalizeNotificationSettings(NotificationSettings{
			Smtp: NotificationSmtpSettings{Host: " relay.corp.test ", Port: port},
		})
		if got.Smtp.Port != 587 {
			t.Errorf("port %d normalised to %d, want 587", port, got.Smtp.Port)
		}
		if got.Smtp.Host != "relay.corp.test" {
			t.Errorf("host not trimmed: %q", got.Smtp.Host)
		}
	}
	// A deliberate, valid port must survive.
	got := normalizeNotificationSettings(NotificationSettings{
		Smtp: NotificationSmtpSettings{Host: "relay.corp.test", Port: 25},
	})
	if got.Smtp.Port != 25 {
		t.Errorf("a valid port was overwritten: %d", got.Smtp.Port)
	}
}

// TestNormalizeDestinationsSplitsEmailRecipients asserts the save path normalises
// what an operator pastes, so the stored row is what the sender will use.
func TestNormalizeDestinationsSplitsEmailRecipients(t *testing.T) {
	got := normalizeDestinations([]NotificationDestination{{
		Id: "d1", Type: DestinationTypeEmail, Enabled: true,
		Email: NotificationEmailSettings{
			To:            []string{"ops@corp.test, noc@corp.test", " ops@corp.test "},
			SubjectPrefix: "  [Site A]  ",
		},
	}})
	if len(got) != 1 {
		t.Fatalf("expected 1 destination, got %d", len(got))
	}
	if strings.Join(got[0].Email.To, "|") != "ops@corp.test|noc@corp.test" {
		t.Errorf("recipients = %v", got[0].Email.To)
	}
	if got[0].Email.SubjectPrefix != "[Site A]" {
		t.Errorf("subject prefix not trimmed: %q", got[0].Email.SubjectPrefix)
	}
	// A destination with no explicit name falls back to a titleised type, the same
	// as every other type.
	if got[0].Name != "Email" {
		t.Errorf("name fallback = %q, want Email", got[0].Name)
	}
}
