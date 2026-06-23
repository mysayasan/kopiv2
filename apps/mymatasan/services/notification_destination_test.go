package services

import "testing"

func TestMigrateLegacyDestinationsFromConfiguredSingletons(t *testing.T) {
	s := NotificationSettings{
		Webhook:  NotificationWebhookSettings{Enabled: true, URL: "https://hooks.example/x", MinSeverity: "warning"},
		Telegram: NotificationTelegramSettings{Enabled: false, BotToken: "tok", ChatId: "123", MinSeverity: "critical"},
	}
	got := migrateLegacyDestinations(s)
	if len(got) != 2 {
		t.Fatalf("expected 2 destinations, got %d: %+v", len(got), got)
	}
	if got[0].Type != DestinationTypeWebhook || got[0].URL != "https://hooks.example/x" || !got[0].Enabled {
		t.Fatalf("webhook destination wrong: %+v", got[0])
	}
	// Telegram has token+chatId (so it migrates) even though disabled; enabled flag preserved.
	if got[1].Type != DestinationTypeTelegram || got[1].BotToken != "tok" || got[1].ChatId != "123" || got[1].Enabled {
		t.Fatalf("telegram destination wrong: %+v", got[1])
	}
}

func TestMigrateLegacyDestinationsEmptyWhenUnconfigured(t *testing.T) {
	if got := migrateLegacyDestinations(NotificationSettings{}); len(got) != 0 {
		t.Fatalf("expected no destinations for a blank config, got %+v", got)
	}
}

func TestNormalizeNotificationSettingsSeedsDestinationsOnce(t *testing.T) {
	s := normalizeNotificationSettings(NotificationSettings{
		Webhook: NotificationWebhookSettings{Enabled: true, URL: "https://h/x"},
	})
	if len(s.Destinations) != 1 || s.Destinations[0].Type != DestinationTypeWebhook {
		t.Fatalf("expected migrated webhook destination, got %+v", s.Destinations)
	}
	// The domain channel config now derives from the migrated destination list.
	cfg := notificationChannelConfig(s)
	if len(cfg.Destinations) != 1 || cfg.Destinations[0].URL != "https://h/x" {
		t.Fatalf("expected one webhook destination from migration, got %+v", cfg.Destinations)
	}
}

func TestNormalizeNotificationSettingsKeepsExistingDestinations(t *testing.T) {
	// When destinations already exist, migration must NOT overwrite them.
	s := normalizeNotificationSettings(NotificationSettings{
		Webhook:      NotificationWebhookSettings{Enabled: true, URL: "https://legacy/x"},
		Destinations: []NotificationDestination{{Id: "d1", Name: "Ops", Type: DestinationTypeWebhook, URL: "https://kept/y"}},
	})
	if len(s.Destinations) != 1 || s.Destinations[0].URL != "https://kept/y" {
		t.Fatalf("existing destinations must be preserved, got %+v", s.Destinations)
	}
}

func TestNormalizeDestinationsAssignsIdsAndDedupsFields(t *testing.T) {
	got := normalizeDestinations([]NotificationDestination{
		{Type: "webhook", MinSeverity: "bogus", CustomFields: []NotificationCustomField{
			{Key: " site ", Value: " Front Gate "},
			{Key: "site", Value: "dup-dropped"},
			{Key: "", Value: "no-key-dropped"},
		}},
		{Id: "dup", Type: "telegram"},
		{Id: "dup", Type: "telegram"},
	})
	if got[0].Id == "" {
		t.Error("missing id should be generated")
	}
	if got[0].Name != "Webhook" {
		t.Errorf("default name = %q, want Webhook", got[0].Name)
	}
	if got[0].MinSeverity != "warning" {
		t.Errorf("bogus severity should normalize to warning, got %q", got[0].MinSeverity)
	}
	if len(got[0].CustomFields) != 1 || got[0].CustomFields[0].Key != "site" || got[0].CustomFields[0].Value != "Front Gate" {
		t.Errorf("custom fields not normalized/deduped: %+v", got[0].CustomFields)
	}
	if got[1].Id == got[2].Id {
		t.Error("duplicate ids should be regenerated to be unique")
	}
}

func TestValidateDestinations(t *testing.T) {
	if err := validateDestinations([]NotificationDestination{{Type: DestinationTypeWebhook, Enabled: true}}); err == nil {
		t.Error("enabled webhook without url should fail")
	}
	if err := validateDestinations([]NotificationDestination{{Type: "sms", Enabled: true}}); err == nil {
		t.Error("unknown destination type should fail")
	}
	if err := validateDestinations([]NotificationDestination{
		{Type: DestinationTypeWebhook, Enabled: true, URL: "https://ok/x"},
		{Type: DestinationTypeTelegram, Enabled: false},
	}); err != nil {
		t.Errorf("valid destinations should pass, got %v", err)
	}
}

func TestValidateDestinationsMqtt(t *testing.T) {
	if err := validateDestinations([]NotificationDestination{{Type: DestinationTypeMqtt, Enabled: true}}); err == nil {
		t.Error("enabled mqtt without broker/topic should fail")
	}
	// Client cert without key (or vice versa) must fail.
	if err := validateDestinations([]NotificationDestination{{
		Type: DestinationTypeMqtt, Enabled: true,
		Mqtt: NotificationMqttSettings{BrokerURL: "ssl://b:8883", Topic: "t", ClientCert: "CERT"},
	}}); err == nil {
		t.Error("mqtt client cert without key should fail")
	}
	if err := validateDestinations([]NotificationDestination{{
		Type: DestinationTypeMqtt, Enabled: true,
		Mqtt: NotificationMqttSettings{BrokerURL: "ssl://b:8883", Topic: "matasan/alerts", ClientCert: "CERT", ClientKey: "KEY"},
	}}); err != nil {
		t.Errorf("valid mqtt destination should pass, got %v", err)
	}
}

func TestNotificationChannelConfigMapsMqtt(t *testing.T) {
	s := normalizeNotificationSettings(NotificationSettings{
		Destinations: []NotificationDestination{{
			Id: "m1", Name: "Broker", Type: DestinationTypeMqtt, Enabled: true, MinSeverity: "warning",
			Mqtt: NotificationMqttSettings{BrokerURL: "ssl://b:8883", Topic: "matasan/alerts", Qos: 2, Retain: true, ClientCert: "C", ClientKey: "K"},
		}},
	})
	cfg := notificationChannelConfig(s)
	if len(cfg.Destinations) != 1 {
		t.Fatalf("expected 1 destination, got %d", len(cfg.Destinations))
	}
	m := cfg.Destinations[0].Mqtt
	if m.BrokerURL != "ssl://b:8883" || m.Topic != "matasan/alerts" || m.QoS != 2 || !m.Retain || m.ClientCert != "C" {
		t.Errorf("mqtt mapping wrong: %+v", m)
	}
}
