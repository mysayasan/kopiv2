package services

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/mysayasan/kopiv2/apps/mymatasan/entities"
	"github.com/mysayasan/kopiv2/domain/notification"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

const notificationSettingsKey = "notification"

// NotificationSettings is the runtime-editable notification delivery
// configuration persisted in the database and edited from the Settings UI.
type NotificationSettings struct {
	Webhook   NotificationWebhookSettings   `json:"webhook"`
	Telegram  NotificationTelegramSettings  `json:"telegram"`
	Retention NotificationRetentionSettings `json:"retention"`
	// Destinations is the per-destination delivery list (Phase 1: persisted +
	// migrated from the Webhook/Telegram singletons; the live hub still delivers
	// via those singletons until the Phase 2 refactor consumes this list).
	Destinations []NotificationDestination `json:"destinations,omitempty"`
}

type NotificationWebhookSettings struct {
	Enabled     bool   `json:"enabled"`
	URL         string `json:"url"`
	MinSeverity string `json:"minSeverity"`
}

type NotificationTelegramSettings struct {
	Enabled     bool   `json:"enabled"`
	BotToken    string `json:"botToken"`
	ChatId      string `json:"chatId"`
	MinSeverity string `json:"minSeverity"`
}

type NotificationRetentionSettings struct {
	Days          int  `json:"days"`
	OnlyRead      bool `json:"onlyRead"`
	IntervalHours int  `json:"intervalHours"`
}

type notificationSettingsService struct {
	repo     dbsql.IGenericRepo[entities.RuntimeSetting]
	notif    INotificationService
	defaults NotificationSettings
}

// NewNotificationSettingsService creates the notification settings service,
// seeded with defaults from app config. It persists settings under a single
// "notification" key (the same runtime_setting table used by decoder/vision).
func NewNotificationSettingsService(
	repo dbsql.IGenericRepo[entities.RuntimeSetting],
	notif INotificationService,
	defaults NotificationSettings,
) INotificationSettingsService {
	return &notificationSettingsService{repo: repo, notif: notif, defaults: normalizeNotificationSettings(defaults)}
}

func (s *notificationSettingsService) Get(ctx context.Context) (NotificationSettings, error) {
	row, err := s.repo.GetByUnique(ctx, "", "key", notificationSettingsKey)
	if err != nil {
		if isNoResultFoundErr(err) {
			return s.Save(ctx, s.defaults)
		}
		return NotificationSettings{}, err
	}
	settings := NotificationSettings{}
	if row != nil && strings.TrimSpace(row.Value) != "" {
		if err := json.Unmarshal([]byte(row.Value), &settings); err != nil {
			return NotificationSettings{}, fmt.Errorf("parse notification settings failed: %w", err)
		}
	}
	return normalizeNotificationSettings(settings), nil
}

func (s *notificationSettingsService) Save(ctx context.Context, settings NotificationSettings) (NotificationSettings, error) {
	settings = normalizeNotificationSettings(settings)
	if err := validateNotificationSettings(settings); err != nil {
		return NotificationSettings{}, err
	}
	payload, err := json.Marshal(settings)
	if err != nil {
		return NotificationSettings{}, err
	}
	now := time.Now().UTC().Unix()

	existing, err := s.repo.GetByUnique(ctx, "", "key", notificationSettingsKey)
	if err == nil && existing != nil {
		existing.Value = string(payload)
		existing.UpdatedAt = now
		if _, err := s.repo.UpdateById(ctx, "", *existing); err != nil {
			return NotificationSettings{}, err
		}
	} else if err != nil && !isNoResultFoundErr(err) {
		return NotificationSettings{}, err
	} else if _, err := s.repo.Create(ctx, "", entities.RuntimeSetting{
		Key:       notificationSettingsKey,
		Value:     string(payload),
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		return NotificationSettings{}, err
	}

	// Apply the new delivery configuration to the live hub.
	if s.notif != nil {
		s.notif.Configure(notificationChannelConfig(settings))
	}
	return settings, nil
}

// Destinations returns the configured delivery destinations for per-destination
// alert rendering. On error it returns nil (no external delivery).
func (s *notificationSettingsService) Destinations(ctx context.Context) []NotificationDestination {
	settings, err := s.Get(ctx)
	if err != nil {
		return nil
	}
	return settings.Destinations
}

func (s *notificationSettingsService) Sync(ctx context.Context) error {
	settings, err := s.Get(ctx)
	if err != nil {
		return err
	}
	if s.notif != nil {
		s.notif.Configure(notificationChannelConfig(settings))
	}
	return nil
}

func (s *notificationSettingsService) Retention(ctx context.Context) (int, bool) {
	settings, err := s.Get(ctx)
	if err != nil {
		return 0, false
	}
	return settings.Retention.Days, settings.Retention.OnlyRead
}

func (s *notificationSettingsService) Test(ctx context.Context, severity string) error {
	if s.notif == nil {
		return fmt.Errorf("notification service unavailable")
	}
	s.notif.Publish(ctx, notification.Notification{
		Category: notification.CategorySystem,
		Severity: parseNotificationSeverity(severity),
		Title:    "Test notification",
		Body:     "This is a test notification from mymatasan. If you received it, your delivery channel is working.",
		Source:   "settings",
	})
	return nil
}

// notificationChannelConfig maps persisted settings into the domain channel
// configuration applied to the hub. Delivery is per-destination: each enabled
// destination becomes a filtered outbound channel (own severity floor + category
// subscription). The legacy Webhook/Telegram singletons are migrated into the
// destination list by normalizeNotificationSettings, so they are not mapped here.
func notificationChannelConfig(s NotificationSettings) notification.ChannelConfig {
	cfg := notification.ChannelConfig{}
	for _, d := range s.Destinations {
		if !d.Enabled {
			continue
		}
		dc := notification.DestinationConfig{
			Id:          d.Id,
			Type:        d.Type,
			MinSeverity: parseNotificationSeverity(d.MinSeverity),
			Categories:  d.Categories,
		}
		switch d.Type {
		case DestinationTypeWebhook:
			dc.URL = d.URL
		case DestinationTypeTelegram:
			dc.BotToken = d.BotToken
			dc.ChatID = d.ChatId
		case DestinationTypeMqtt:
			dc.Mqtt = notification.MqttDestinationConfig{
				BrokerURL:          d.Mqtt.BrokerURL,
				Topic:              d.Mqtt.Topic,
				ClientID:           d.Mqtt.ClientId,
				QoS:                byte(d.Mqtt.Qos),
				Retain:             d.Mqtt.Retain,
				Username:           d.Mqtt.Username,
				Password:           d.Mqtt.Password,
				CACert:             d.Mqtt.CaCert,
				ClientCert:         d.Mqtt.ClientCert,
				ClientKey:          d.Mqtt.ClientKey,
				InsecureSkipVerify: d.Mqtt.InsecureSkipVerify,
			}
		}
		cfg.Destinations = append(cfg.Destinations, dc)
	}
	return cfg
}

func normalizeNotificationSettings(s NotificationSettings) NotificationSettings {
	s.Webhook.URL = strings.TrimSpace(s.Webhook.URL)
	s.Webhook.MinSeverity = normalizeSeverityString(s.Webhook.MinSeverity)
	s.Telegram.BotToken = strings.TrimSpace(s.Telegram.BotToken)
	s.Telegram.ChatId = strings.TrimSpace(s.Telegram.ChatId)
	s.Telegram.MinSeverity = normalizeSeverityString(s.Telegram.MinSeverity)
	if s.Retention.Days < 0 {
		s.Retention.Days = 0
	}
	if s.Retention.IntervalHours <= 0 {
		s.Retention.IntervalHours = 6
	}
	// Seed the destination list from the legacy singletons the first time (before
	// any destinations are stored), then normalize. Delivery is unaffected in
	// Phase 1 — notificationChannelConfig still reads Webhook/Telegram.
	if len(s.Destinations) == 0 {
		s.Destinations = migrateLegacyDestinations(s)
	}
	s.Destinations = normalizeDestinations(s.Destinations)
	return s
}

func validateNotificationSettings(s NotificationSettings) error {
	if s.Webhook.Enabled {
		if s.Webhook.URL == "" {
			return fmt.Errorf("webhook url is required when the webhook channel is enabled")
		}
		parsed, err := url.ParseRequestURI(s.Webhook.URL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return fmt.Errorf("webhook url must be a valid http(s) URL")
		}
	}
	if s.Telegram.Enabled {
		if s.Telegram.BotToken == "" || s.Telegram.ChatId == "" {
			return fmt.Errorf("telegram botToken and chatId are required when the telegram channel is enabled")
		}
	}
	if err := validateDestinations(s.Destinations); err != nil {
		return err
	}
	return nil
}

// normalizeSeverityString returns a valid severity string, defaulting to
// "warning" so noisy informational events do not flood external channels.
func normalizeSeverityString(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "info":
		return "info"
	case "critical":
		return "critical"
	case "warning":
		return "warning"
	default:
		return "warning"
	}
}

func parseNotificationSeverity(value string) notification.Severity {
	switch normalizeSeverityString(value) {
	case "info":
		return notification.Info
	case "critical":
		return notification.Critical
	default:
		return notification.Warning
	}
}
