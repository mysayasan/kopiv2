package services

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/mysayasan/kopiv2/domain/notification"
	"github.com/mysayasan/kopiv2/infra/config"
)

func emailConfig() *config.AppConfigModel {
	cfg := &config.AppConfigModel{}
	cfg.Smtp.Enabled = true
	cfg.Smtp.Host = "relay.corp.test"
	cfg.Smtp.Port = 587
	cfg.Smtp.From = "fleet@corp.test"
	cfg.Notification.Email.Enabled = true
	cfg.Notification.Email.To = "ops@corp.test, noc@corp.test"
	cfg.Notification.Email.SubjectPrefix = "[HQ]"
	cfg.Notification.Email.MinSeverity = "warning"
	return cfg
}

func TestNotificationChannelConfigBuildsEmailDestination(t *testing.T) {
	cfg := NotificationChannelConfig(emailConfig())

	if cfg.Smtp.Host != "relay.corp.test" || cfg.Smtp.Port != 587 {
		t.Errorf("relay not mapped: %+v", cfg.Smtp)
	}
	if len(cfg.Destinations) != 1 {
		t.Fatalf("expected 1 destination, got %d", len(cfg.Destinations))
	}
	d := cfg.Destinations[0]
	if d.Type != "email" {
		t.Errorf("type = %q", d.Type)
	}
	if strings.Join(d.Email.To, "|") != "ops@corp.test|noc@corp.test" {
		t.Errorf("recipients = %v", d.Email.To)
	}
	if d.Email.SubjectPrefix != "[HQ]" {
		t.Errorf("subject prefix = %q", d.Email.SubjectPrefix)
	}
	if d.MinSeverity != notification.Warning {
		t.Errorf("severity floor = %q", d.MinSeverity)
	}
	// A control-plane notification crosses the control channel as JSON and
	// Attachment is json:"-", so no image ever survives the hop. Promising one
	// would produce mail that claims evidence it does not carry.
	if d.Email.IncludeSnapshot {
		t.Error("the control plane claimed it would attach a snapshot")
	}
}

// TestControlPlaneNotificationDropsAttachment is the assertion behind that
// IncludeSnapshot=false decision. If the wire format ever starts carrying the
// image, this test fails and the decision above should be revisited — rather
// than the comment quietly becoming untrue.
func TestControlPlaneNotificationDropsAttachment(t *testing.T) {
	n := notification.Notification{
		ID: "n1", Title: "Alert",
		Attachment: &notification.Attachment{Filename: "a.jpg", ContentType: "image/jpeg", Data: []byte{1, 2, 3}},
	}
	encoded, err := json.Marshal(n)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded notification.Notification
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Attachment != nil {
		t.Error("an attachment survived the control-channel encoding; revisit IncludeSnapshot")
	}
}

// TestNotificationChannelConfigDefaultsOff is the upgrade-safety assertion: an
// install that upgrades onto this build must not start emailing anyone.
func TestNotificationChannelConfigDefaultsOff(t *testing.T) {
	if got := NotificationChannelConfig(&config.AppConfigModel{}); len(got.Destinations) != 0 {
		t.Errorf("a default config produced %d destinations: %+v", len(got.Destinations), got.Destinations)
	}
	if got := NotificationChannelConfig(nil); len(got.Destinations) != 0 {
		t.Error("a nil config produced destinations")
	}
	// An empty config has no recipients either, so the two reasons to deliver
	// nothing are indistinguishable above. Pin the one that actually protects an
	// upgrade: the enabled flag alone must be able to hold delivery back, even
	// with a fully populated relay and recipient list sitting behind it.
	cfg := emailConfig()
	cfg.Notification.Email.Enabled = false
	if got := NotificationChannelConfig(cfg); len(got.Destinations) != 0 {
		t.Errorf("a disabled email block with recipients still delivered: %+v", got.Destinations)
	}
}

// TestNotificationChannelConfigSkipsIncompleteDestinations keeps a half-filled
// config from producing a channel that cannot deliver.
func TestNotificationChannelConfigSkipsIncompleteDestinations(t *testing.T) {
	cfg := emailConfig()
	cfg.Notification.Email.To = "  ,  "
	if got := NotificationChannelConfig(cfg); len(got.Destinations) != 0 {
		t.Errorf("email enabled with no recipients still built a destination: %+v", got.Destinations)
	}

	cfg = emailConfig()
	cfg.Notification.Email.Enabled = false
	if got := NotificationChannelConfig(cfg); len(got.Destinations) != 0 {
		t.Error("a disabled email block still built a destination")
	}
}

// TestNotificationChannelConfigDefaultsSmtpPort keeps an unset port from
// producing a relay the sender cannot dial.
func TestNotificationChannelConfigDefaultsSmtpPort(t *testing.T) {
	for _, port := range []int{0, -1, 70000} {
		cfg := emailConfig()
		cfg.Smtp.Port = port
		if got := NotificationChannelConfig(cfg); got.Smtp.Port != 587 {
			t.Errorf("port %d normalised to %d, want 587", port, got.Smtp.Port)
		}
	}
	cfg := emailConfig()
	cfg.Smtp.Port = 25
	if got := NotificationChannelConfig(cfg); got.Smtp.Port != 25 {
		t.Errorf("a valid port was overwritten: %d", got.Smtp.Port)
	}
}

// TestNotificationChannelConfigWiresLegacyBlocks asserts the webhook and telegram
// config blocks — present in the model since before this item and never consumed
// — now actually produce destinations.
func TestNotificationChannelConfigWiresLegacyBlocks(t *testing.T) {
	cfg := &config.AppConfigModel{}
	enabled := true
	cfg.Notification.Webhook.Enabled = &enabled
	cfg.Notification.Webhook.URL = "https://hooks.corp.test/fleet"
	cfg.Notification.Telegram.Enabled = &enabled
	cfg.Notification.Telegram.BotToken = "token"
	cfg.Notification.Telegram.ChatId = "chat"

	got := NotificationChannelConfig(cfg)
	types := make([]string, 0, len(got.Destinations))
	for _, d := range got.Destinations {
		types = append(types, d.Type)
	}
	if strings.Join(types, ",") != "webhook,telegram" {
		t.Errorf("destinations = %v, want webhook and telegram", types)
	}
}

func TestSplitList(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"a@b.test, c@d.test", []string{"a@b.test", "c@d.test"}},
		{" a@b.test ,, a@B.test ", []string{"a@b.test"}},
		{"   ", nil},
		{"", nil},
	}
	for _, tc := range cases {
		if got := splitList(tc.in); strings.Join(got, "|") != strings.Join(tc.want, "|") {
			t.Errorf("splitList(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestValidateNotificationSection(t *testing.T) {
	base := func() map[string]any {
		return map[string]any{
			"smtp": map[string]any{
				"enabled": true, "host": "relay.corp.test", "port": 587,
				"from": "fleet@corp.test", "username": "", "password": "", "useStartTls": false,
			},
			"notification": map[string]any{"email": map[string]any{
				"enabled": true, "to": "ops@corp.test", "minSeverity": "warning",
			}},
		}
	}

	if err := validateSection("notification", base()); err != nil {
		t.Fatalf("a valid section was rejected: %v", err)
	}

	t.Run("username without STARTTLS is refused", func(t *testing.T) {
		// The sender refuses to transmit a credential over a cleartext link, so
		// this combination yields a relay that silently never delivers. Catching it
		// at save time is the difference between a corrected setting and an
		// alerting path nobody knows is dead.
		d := base()
		d["smtp"].(map[string]any)["username"] = "fleet"
		err := validateSection("notification", d)
		if err == nil {
			t.Fatal("accepted a username with STARTTLS off")
		}
		if !strings.Contains(err.Error(), "STARTTLS") {
			t.Errorf("error does not name the cause: %v", err)
		}
	})

	t.Run("enabled email with no recipients is refused", func(t *testing.T) {
		d := base()
		d["notification"].(map[string]any)["email"].(map[string]any)["to"] = "  "
		if err := validateSection("notification", d); err == nil {
			t.Fatal("accepted email delivery with no recipients")
		}
	})

	t.Run("email without the relay enabled is refused", func(t *testing.T) {
		d := base()
		d["smtp"].(map[string]any)["enabled"] = false
		if err := validateSection("notification", d); err == nil {
			t.Fatal("accepted email delivery with the relay switched off")
		}
	})

	t.Run("a malformed recipient is refused", func(t *testing.T) {
		d := base()
		d["notification"].(map[string]any)["email"].(map[string]any)["to"] = "ops@corp.test\r\nBcc: attacker@evil.test"
		if err := validateSection("notification", d); err == nil {
			t.Fatal("accepted a CR/LF-bearing recipient")
		}
	})

	t.Run("no sender address is refused", func(t *testing.T) {
		d := base()
		d["smtp"].(map[string]any)["from"] = ""
		if err := validateSection("notification", d); err == nil {
			t.Fatal("accepted email delivery with no sender address")
		}
	})

	t.Run("an unknown severity is refused", func(t *testing.T) {
		d := base()
		d["notification"].(map[string]any)["email"].(map[string]any)["minSeverity"] = "urgent"
		if err := validateSection("notification", d); err == nil {
			t.Fatal("accepted an unknown severity")
		}
	})

	t.Run("disabled email skips the relay checks", func(t *testing.T) {
		// A half-built configuration must be savable while it is switched off.
		d := base()
		d["notification"].(map[string]any)["email"].(map[string]any)["enabled"] = false
		d["notification"].(map[string]any)["email"].(map[string]any)["to"] = ""
		d["smtp"].(map[string]any)["enabled"] = false
		d["smtp"].(map[string]any)["host"] = ""
		if err := validateSection("notification", d); err != nil {
			t.Errorf("a switched-off section was rejected: %v", err)
		}
	})
}

func TestApplyNotificationSection(t *testing.T) {
	cfg := &config.AppConfigModel{}
	data := map[string]any{
		"smtp": map[string]any{
			"enabled": true, "host": " relay.corp.test ", "port": 2525,
			"from": " fleet@corp.test ", "username": " fleet ", "password": "s3cret", "useStartTls": true,
		},
		"notification": map[string]any{"email": map[string]any{
			"enabled": true, "to": "ops@corp.test, ops@corp.test , noc@corp.test",
			"subjectPrefix": " [HQ] ", "minSeverity": "Critical", "categories": " health.check , system ",
		}},
	}
	if err := applyToConfig(cfg, "notification", data); err != nil {
		t.Fatalf("applyToConfig: %v", err)
	}

	if cfg.Smtp.Host != "relay.corp.test" || cfg.Smtp.From != "fleet@corp.test" || cfg.Smtp.Username != "fleet" {
		t.Errorf("relay fields not trimmed: %+v", cfg.Smtp)
	}
	if cfg.Smtp.Port != 2525 || !cfg.Smtp.UseStartTls || cfg.Smtp.Password != "s3cret" {
		t.Errorf("relay not applied: %+v", cfg.Smtp)
	}
	if cfg.Notification.Email.To != "ops@corp.test, noc@corp.test" {
		t.Errorf("recipients not normalised on save: %q", cfg.Notification.Email.To)
	}
	if cfg.Notification.Email.SubjectPrefix != "[HQ]" {
		t.Errorf("subject prefix = %q", cfg.Notification.Email.SubjectPrefix)
	}
	if cfg.Notification.Email.MinSeverity != "critical" {
		t.Errorf("severity not lowercased: %q", cfg.Notification.Email.MinSeverity)
	}
	if cfg.Notification.Email.Categories != "health.check,system" {
		t.Errorf("categories = %q", cfg.Notification.Email.Categories)
	}

	// The round trip must produce a working destination, not just a stored string.
	built := NotificationChannelConfig(cfg)
	if len(built.Destinations) != 1 || len(built.Destinations[0].Email.To) != 2 {
		t.Errorf("saved settings did not build a deliverable destination: %+v", built.Destinations)
	}
}

// TestNotificationSectionIsRegistered keeps the section reachable from the API —
// a validate/apply pair that no section id points at is dead code.
func TestNotificationSectionIsRegistered(t *testing.T) {
	found := false
	for _, id := range sectionOrder {
		if id == "notification" {
			found = true
		}
	}
	if !found {
		t.Fatal("the notification section is not in sectionOrder")
	}
	secrets := sectionSecrets["notification"]
	if len(secrets) != 1 || secrets[0] != "smtp.password" {
		t.Errorf("the relay password is not registered as a secret: %v", secrets)
	}
}
