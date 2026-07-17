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

// channelConfig maps enabled destinations (only) to the shared config, carrying severity + category.
func TestNotifSettings_ChannelConfigMapsDestinations(t *testing.T) {
	cfg := channelConfig(NotificationSettings{Destinations: []NotificationDestination{
		{Id: "a", Type: "webhook", Enabled: true, URL: "https://h/x", MinSeverity: "critical", Categories: []string{"device.alert"}},
		{Id: "b", Type: "telegram", Enabled: true, BotToken: "tok", ChatId: "42", MinSeverity: "info"},
		{Id: "c", Type: "webhook", Enabled: false, URL: "https://nope/x"}, // disabled → skipped
	}})
	if len(cfg.Destinations) != 2 {
		t.Fatalf("only enabled destinations map through: got %d", len(cfg.Destinations))
	}
	w := cfg.Destinations[0]
	if w.URL != "https://h/x" || w.MinSeverity != notification.Critical || len(w.Categories) != 1 || w.Categories[0] != "device.alert" {
		t.Errorf("webhook destination mapping wrong: %+v", w)
	}
	tg := cfg.Destinations[1]
	if tg.BotToken != "tok" || tg.ChatID != "42" || tg.MinSeverity != notification.Info {
		t.Errorf("telegram destination mapping wrong: %+v", tg)
	}
	if parseSeverity("") != notification.Warning {
		t.Error("empty severity must default to warning")
	}
}

// A destination with no categories receives everything; one with categories filters.
func TestNotifDestination_AllowsCategory(t *testing.T) {
	all := NotificationDestination{}
	if !all.AllowsCategory("device.alert") || !all.AllowsCategory("system") {
		t.Error("an empty category list must allow every category")
	}
	only := NotificationDestination{Categories: []string{"device.alert"}}
	if !only.AllowsCategory("device.alert") || only.AllowsCategory("system") {
		t.Error("a category list must filter to exactly its members")
	}
}

// normalize assigns ids, defaults names, and drops unknown categories; validate refuses an enabled
// destination that can't deliver.
func TestNotifDestination_NormalizeAndValidate(t *testing.T) {
	got := normalizeDestinations([]NotificationDestination{
		{Type: "webhook", Enabled: true, URL: "https://h/x", Categories: []string{"device.alert", "bogus.cat", "system"}},
		{Type: "", Name: ""}, // becomes webhook / "Webhook"
	})
	if got[0].Id == "" || got[1].Id == "" || got[0].Id == got[1].Id {
		t.Error("every destination must get a unique id")
	}
	if len(got[0].Categories) != 2 { // bogus.cat dropped
		t.Errorf("unknown categories must be dropped, got %v", got[0].Categories)
	}
	if got[1].Name != "Webhook" || got[1].Type != "webhook" {
		t.Errorf("a typeless destination defaults to webhook/Webhook: %+v", got[1])
	}
	if err := validateDestinations([]NotificationDestination{{Type: "webhook", Enabled: true}}); err == nil {
		t.Error("an enabled webhook with no URL must be refused")
	}
	if err := validateDestinations([]NotificationDestination{{Type: "mqtt", Enabled: true, Mqtt: NotificationMqttSettings{BrokerURL: "tcp://b:1883"}}}); err == nil {
		t.Error("an enabled MQTT destination with no topic must be refused")
	}
	if err := validateDestinations([]NotificationDestination{{Type: "webhook", Enabled: false}}); err != nil {
		t.Error("a DISABLED half-filled destination is allowed (a draft)")
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
