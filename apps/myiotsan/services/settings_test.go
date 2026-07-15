package services

import (
	"testing"

	"github.com/mysayasan/kopiv2/domain/notification"
)

func TestNotifSettings_ValidationRequiresChannelFields(t *testing.T) {
	// A disabled channel needs nothing.
	if err := validateNotifSettings(normalizeNotifSettings(NotificationSettings{})); err != nil {
		t.Fatalf("disabled channels should validate: %v", err)
	}
	// Enabled webhook needs a valid http(s) URL.
	if err := validateNotifSettings(NotificationSettings{Webhook: WebhookSettings{Enabled: true}}); err == nil {
		t.Error("an enabled webhook with no URL must be rejected")
	}
	if err := validateNotifSettings(NotificationSettings{Webhook: WebhookSettings{Enabled: true, URL: "ftp://x"}}); err == nil {
		t.Error("a non-http webhook URL must be rejected")
	}
	if err := validateNotifSettings(NotificationSettings{Webhook: WebhookSettings{Enabled: true, URL: "https://hooks.example/x"}}); err != nil {
		t.Errorf("a valid https webhook must pass: %v", err)
	}
	// Enabled telegram needs both token and chat id.
	if err := validateNotifSettings(NotificationSettings{Telegram: TelegramSettings{Enabled: true, BotToken: "t"}}); err == nil {
		t.Error("telegram with no chat id must be rejected")
	}
	if err := validateNotifSettings(NotificationSettings{Telegram: TelegramSettings{Enabled: true, BotToken: "t", ChatId: "c"}}); err != nil {
		t.Errorf("telegram with token+chat must pass: %v", err)
	}
}

func TestNotifSettings_ChannelConfigMapsSeverityAndFields(t *testing.T) {
	cfg := channelConfig(NotificationSettings{
		Webhook:  WebhookSettings{Enabled: true, URL: "https://h/x", MinSeverity: "critical"},
		Telegram: TelegramSettings{Enabled: true, BotToken: "tok", ChatId: "42", MinSeverity: "info"},
	})
	if !cfg.Webhook.Enabled || cfg.Webhook.URL != "https://h/x" || cfg.Webhook.MinSeverity != notification.Critical {
		t.Errorf("webhook mapping wrong: %+v", cfg.Webhook)
	}
	if cfg.Telegram.BotToken != "tok" || cfg.Telegram.ChatID != "42" || cfg.Telegram.MinSeverity != notification.Info {
		t.Errorf("telegram mapping wrong: %+v", cfg.Telegram)
	}
	// An unset severity floors at warning, not info.
	if parseSeverity("") != notification.Warning {
		t.Error("empty severity must default to warning")
	}
}

func TestTelemetrySettings_DefaultsFillZeros(t *testing.T) {
	def := TelemetrySettings{RawRetentionDays: 30, RollupRetentionDays: 400, BatchSize: 200, FlushMs: 250, QueueSize: 8192, MqttAddr: "0.0.0.0:1883"}
	// A partial blob (only retention edited) keeps the other defaults, never 0.
	got := withTelemetryDefaults(TelemetrySettings{RawRetentionDays: 7}, def)
	if got.RawRetentionDays != 7 {
		t.Errorf("edited field must be kept: %d", got.RawRetentionDays)
	}
	if got.BatchSize != 200 || got.QueueSize != 8192 || got.MqttAddr != "0.0.0.0:1883" || got.RollupRetentionDays != 400 {
		t.Errorf("unset fields must fall back to defaults: %+v", got)
	}
}

func TestTelemetrySettings_Validation(t *testing.T) {
	ok := TelemetrySettings{RawRetentionDays: 30, RollupRetentionDays: 400, BatchSize: 200, FlushMs: 250, QueueSize: 8192, MqttAddr: "0.0.0.0:1883"}
	if err := validateTelemetrySettings(ok); err != nil {
		t.Fatalf("a sane config must validate: %v", err)
	}
	bad := ok
	bad.RawRetentionDays = 0
	if err := validateTelemetrySettings(bad); err == nil {
		t.Error("zero retention must be rejected")
	}
	bad = ok
	bad.MqttAddr = "1883"
	if err := validateTelemetrySettings(bad); err == nil {
		t.Error("a broker address without host:port must be rejected")
	}
	bad = ok
	bad.BatchSize = 0
	if err := validateTelemetrySettings(bad); err == nil {
		t.Error("zero batch size must be rejected")
	}
}
