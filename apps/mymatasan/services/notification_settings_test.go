package services

import (
	"testing"

	"github.com/mysayasan/kopiv2/domain/notification"
)

func TestValidateNotificationSettings(t *testing.T) {
	cases := []struct {
		name    string
		in      NotificationSettings
		wantErr bool
	}{
		{name: "all disabled", in: NotificationSettings{}, wantErr: false},
		{
			name:    "webhook enabled without url",
			in:      NotificationSettings{Webhook: NotificationWebhookSettings{Enabled: true}},
			wantErr: true,
		},
		{
			name:    "webhook enabled bad url",
			in:      NotificationSettings{Webhook: NotificationWebhookSettings{Enabled: true, URL: "ftp://x"}},
			wantErr: true,
		},
		{
			name:    "webhook enabled good url",
			in:      NotificationSettings{Webhook: NotificationWebhookSettings{Enabled: true, URL: "https://hooks.example.com/x"}},
			wantErr: false,
		},
		{
			name:    "telegram enabled missing chat",
			in:      NotificationSettings{Telegram: NotificationTelegramSettings{Enabled: true, BotToken: "123:ABC"}},
			wantErr: true,
		},
		{
			name:    "telegram enabled complete",
			in:      NotificationSettings{Telegram: NotificationTelegramSettings{Enabled: true, BotToken: "123:ABC", ChatId: "42"}},
			wantErr: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateNotificationSettings(normalizeNotificationSettings(tc.in))
			if (err != nil) != tc.wantErr {
				t.Fatalf("validate err = %v, wantErr = %v", err, tc.wantErr)
			}
		})
	}
}

func TestNormalizeNotificationSettingsDefaults(t *testing.T) {
	got := normalizeNotificationSettings(NotificationSettings{
		Webhook:   NotificationWebhookSettings{MinSeverity: ""},
		Retention: NotificationRetentionSettings{IntervalHours: 0, Days: -5},
	})
	if got.Webhook.MinSeverity != "warning" {
		t.Errorf("default webhook minSeverity = %q, want warning", got.Webhook.MinSeverity)
	}
	if got.Retention.IntervalHours != 6 {
		t.Errorf("default interval = %d, want 6", got.Retention.IntervalHours)
	}
	if got.Retention.Days != 0 {
		t.Errorf("negative days should clamp to 0, got %d", got.Retention.Days)
	}
}

func TestNotificationChannelConfigMapping(t *testing.T) {
	// Normalize first so the legacy singletons migrate into the destination list,
	// which is what notificationChannelConfig now maps to domain destinations.
	s := normalizeNotificationSettings(NotificationSettings{
		Webhook:  NotificationWebhookSettings{Enabled: true, URL: "https://x", MinSeverity: "critical"},
		Telegram: NotificationTelegramSettings{Enabled: true, BotToken: "t", ChatId: "c", MinSeverity: "info"},
	})
	cfg := notificationChannelConfig(s)
	if len(cfg.Destinations) != 2 {
		t.Fatalf("expected 2 destinations, got %d: %+v", len(cfg.Destinations), cfg.Destinations)
	}
	var web, tel *notification.DestinationConfig
	for i := range cfg.Destinations {
		switch cfg.Destinations[i].Type {
		case DestinationTypeWebhook:
			web = &cfg.Destinations[i]
		case DestinationTypeTelegram:
			tel = &cfg.Destinations[i]
		}
	}
	if web == nil || web.URL != "https://x" || web.MinSeverity != notification.Critical {
		t.Errorf("webhook destination wrong: %+v", web)
	}
	if tel == nil || tel.ChatID != "c" || tel.MinSeverity != notification.Info {
		t.Errorf("telegram destination wrong: %+v", tel)
	}
}
