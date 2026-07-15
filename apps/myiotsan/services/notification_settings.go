package services

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	sharedentities "github.com/mysayasan/kopiv2/domain/entities"
	"github.com/mysayasan/kopiv2/domain/notification"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

const notificationSettingsKey = "notification"

// notifier is the slice of the notification service this settings store needs: push a notification,
// and reconfigure the outbound delivery channels. *notification.Service satisfies it. Depending on
// the interface keeps the store unit-testable without a real hub.
type notifier interface {
	Publish(ctx context.Context, n notification.Notification) notification.Notification
	Configure(cfg notification.ChannelConfig)
}

// NotificationSettingsService is the runtime-editable outbound-delivery config for myiotsan.
//
// myiotsan writes every alert to its in-app feed, but until this is configured it delivers NOWHERE
// external — the shared notification service's outbound channels start empty and nothing calls
// Configure. This store IS that missing call: Save/Sync push a webhook/telegram config into the live
// hub (the delivery engine itself is shared infra), so an operator can finally be told off-box when
// a sensor trips. Persisted as one JSON blob in the shared RuntimeSetting KV, the same way the site
// location is (see schedules.go) — no new table.
type NotificationSettingsService struct {
	repo  dbsql.IGenericRepo[sharedentities.RuntimeSetting]
	notif notifier
}

func NewNotificationSettingsService(db dbsql.IDbCrud, notif notifier) *NotificationSettingsService {
	return &NotificationSettingsService{
		repo:  dbsql.NewGenericRepo[sharedentities.RuntimeSetting](db),
		notif: notif,
	}
}

// NotificationSettings is the persisted shape: a webhook and a telegram channel, each with its own
// severity floor so noisy info events don't flood an external channel.
type NotificationSettings struct {
	Webhook  WebhookSettings  `json:"webhook"`
	Telegram TelegramSettings `json:"telegram"`
}

type WebhookSettings struct {
	Enabled     bool   `json:"enabled"`
	URL         string `json:"url"`
	MinSeverity string `json:"minSeverity"`
}

type TelegramSettings struct {
	Enabled     bool   `json:"enabled"`
	BotToken    string `json:"botToken"`
	ChatId      string `json:"chatId"`
	MinSeverity string `json:"minSeverity"`
}

// Get returns the stored settings, or the (disabled) defaults if none were saved yet.
func (s *NotificationSettingsService) Get(ctx context.Context) (NotificationSettings, error) {
	row, err := s.repo.GetByUnique(ctx, "", "key", notificationSettingsKey)
	if err != nil {
		if isNoResultErr(err) {
			return normalizeNotifSettings(NotificationSettings{}), nil
		}
		return NotificationSettings{}, err
	}
	out := NotificationSettings{}
	if row != nil && strings.TrimSpace(row.Value) != "" {
		if err := json.Unmarshal([]byte(row.Value), &out); err != nil {
			return NotificationSettings{}, fmt.Errorf("parse notification settings: %w", err)
		}
	}
	return normalizeNotifSettings(out), nil
}

// Save validates, persists, and applies the settings to the live hub. The Configure call is the one
// line that turns "in-app feed only" into "also delivers to webhook/telegram".
func (s *NotificationSettingsService) Save(ctx context.Context, settings NotificationSettings) (NotificationSettings, error) {
	settings = normalizeNotifSettings(settings)
	if err := validateNotifSettings(settings); err != nil {
		return NotificationSettings{}, err
	}
	payload, _ := json.Marshal(settings)
	now := time.Now().Unix()

	row, err := s.repo.GetByUnique(ctx, "", "key", notificationSettingsKey)
	if err != nil && !isNoResultErr(err) {
		return NotificationSettings{}, err
	}
	if row == nil {
		if _, err := s.repo.Create(ctx, "", sharedentities.RuntimeSetting{
			Key: notificationSettingsKey, Value: string(payload), CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			return NotificationSettings{}, err
		}
	} else {
		row.Value = string(payload)
		row.UpdatedAt = now
		if _, err := s.repo.UpdateById(ctx, "", *row); err != nil {
			return NotificationSettings{}, err
		}
	}
	if s.notif != nil {
		s.notif.Configure(channelConfig(settings))
	}
	return settings, nil
}

// Sync applies the stored settings to the hub without editing them — called once at startup so a
// saved-but-not-yet-re-applied config takes effect on boot.
func (s *NotificationSettingsService) Sync(ctx context.Context) error {
	settings, err := s.Get(ctx)
	if err != nil {
		return err
	}
	if s.notif != nil {
		s.notif.Configure(channelConfig(settings))
	}
	return nil
}

// Test publishes a notification at the given severity so an operator can confirm a channel actually
// delivers — "saved" is not "reaches my phone".
func (s *NotificationSettingsService) Test(ctx context.Context, severity string) error {
	if s.notif == nil {
		return fmt.Errorf("notification service unavailable")
	}
	s.notif.Publish(ctx, notification.Notification{
		Category: notification.CategorySystem,
		Severity: parseSeverity(severity),
		Title:    "Test notification",
		Body:     "This is a test from myiotsan. If you received it, your delivery channel works.",
		Source:   "settings",
	})
	return nil
}

// channelConfig maps the persisted settings onto the shared ChannelConfig. It uses the Webhook /
// Telegram singletons (the service converts them to destinations internally when Destinations is
// empty) — myiotsan does not need the full per-destination model mymatasan carries.
func channelConfig(s NotificationSettings) notification.ChannelConfig {
	return notification.ChannelConfig{
		Webhook: notification.WebhookConfig{
			Enabled:     s.Webhook.Enabled,
			URL:         s.Webhook.URL,
			MinSeverity: parseSeverity(s.Webhook.MinSeverity),
		},
		Telegram: notification.TelegramConfig{
			Enabled:     s.Telegram.Enabled,
			BotToken:    s.Telegram.BotToken,
			ChatID:      s.Telegram.ChatId,
			MinSeverity: parseSeverity(s.Telegram.MinSeverity),
		},
	}
}

func normalizeNotifSettings(s NotificationSettings) NotificationSettings {
	s.Webhook.URL = strings.TrimSpace(s.Webhook.URL)
	s.Webhook.MinSeverity = normalizeSeverity(s.Webhook.MinSeverity)
	s.Telegram.BotToken = strings.TrimSpace(s.Telegram.BotToken)
	s.Telegram.ChatId = strings.TrimSpace(s.Telegram.ChatId)
	s.Telegram.MinSeverity = normalizeSeverity(s.Telegram.MinSeverity)
	return s
}

func validateNotifSettings(s NotificationSettings) error {
	if s.Webhook.Enabled {
		if s.Webhook.URL == "" {
			return fmt.Errorf("a webhook URL is required when the webhook channel is enabled")
		}
		parsed, err := url.ParseRequestURI(s.Webhook.URL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return fmt.Errorf("the webhook URL must be a valid http(s) URL")
		}
	}
	if s.Telegram.Enabled && (s.Telegram.BotToken == "" || s.Telegram.ChatId == "") {
		return fmt.Errorf("a telegram bot token and chat id are required when the telegram channel is enabled")
	}
	return nil
}

// normalizeSeverity defaults to "warning" — an unconfigured floor should not flood a channel with
// info-level chatter.
func normalizeSeverity(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "info":
		return "info"
	case "critical":
		return "critical"
	default:
		return "warning"
	}
}

func parseSeverity(v string) notification.Severity {
	switch normalizeSeverity(v) {
	case "info":
		return notification.Info
	case "critical":
		return notification.Critical
	default:
		return notification.Warning
	}
}
