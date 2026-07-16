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

// NotificationSettings is the persisted shape. Delivery is per-DESTINATION (webhook/telegram/mqtt,
// each with its own severity floor and category filter). The Webhook/Telegram singletons are kept
// only so an older, pre-destinations config migrates forward once; live delivery reads Destinations.
type NotificationSettings struct {
	Webhook      WebhookSettings           `json:"webhook"`
	Telegram     TelegramSettings          `json:"telegram"`
	Destinations []NotificationDestination `json:"destinations"`
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

// SaveDestination upserts ONE destination against the persisted settings (not a client-supplied
// full blob), so saving one destination never clobbers another's stored config. Empty id = append a
// new one; otherwise replace by id (append if it no longer exists).
func (s *NotificationSettingsService) SaveDestination(ctx context.Context, dest NotificationDestination) (NotificationDestination, NotificationSettings, error) {
	current, err := s.Get(ctx)
	if err != nil {
		return NotificationDestination{}, NotificationSettings{}, err
	}
	dest.Id = strings.TrimSpace(dest.Id)
	if dest.Id == "" {
		dest.Id = newDestinationID()
		current.Destinations = append(current.Destinations, dest)
	} else {
		replaced := false
		for i := range current.Destinations {
			if current.Destinations[i].Id == dest.Id {
				current.Destinations[i] = dest
				replaced = true
				break
			}
		}
		if !replaced {
			current.Destinations = append(current.Destinations, dest)
		}
	}
	saved, err := s.Save(ctx, current)
	if err != nil {
		return NotificationDestination{}, NotificationSettings{}, err
	}
	for _, d := range saved.Destinations {
		if d.Id == dest.Id {
			return d, saved, nil
		}
	}
	return dest, saved, nil
}

// DeleteDestination removes one destination by id, leaving the rest untouched.
func (s *NotificationSettingsService) DeleteDestination(ctx context.Context, id string) (NotificationSettings, error) {
	id = strings.TrimSpace(id)
	current, err := s.Get(ctx)
	if err != nil {
		return NotificationSettings{}, err
	}
	kept := current.Destinations[:0:0]
	for _, d := range current.Destinations {
		if d.Id != id {
			kept = append(kept, d)
		}
	}
	current.Destinations = kept
	return s.Save(ctx, current)
}

// Save validates, persists, and applies the settings to the live hub. The Configure call is the one
// line that turns "in-app feed only" into "also delivers to every enabled destination".
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

// channelConfig maps the persisted destinations onto the shared ChannelConfig — one filtered
// outbound channel per enabled destination (its own severity floor + category subscription). The
// delivery engine itself is shared infra (domain/notification + infra/notification).
func channelConfig(s NotificationSettings) notification.ChannelConfig {
	cfg := notification.ChannelConfig{}
	for _, d := range s.Destinations {
		if !d.Enabled {
			continue
		}
		dc := notification.DestinationConfig{
			Id:          d.Id,
			Type:        d.Type,
			MinSeverity: parseSeverity(d.MinSeverity),
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

func normalizeNotifSettings(s NotificationSettings) NotificationSettings {
	s.Webhook.URL = strings.TrimSpace(s.Webhook.URL)
	s.Webhook.MinSeverity = normalizeSeverity(s.Webhook.MinSeverity)
	s.Telegram.BotToken = strings.TrimSpace(s.Telegram.BotToken)
	s.Telegram.ChatId = strings.TrimSpace(s.Telegram.ChatId)
	s.Telegram.MinSeverity = normalizeSeverity(s.Telegram.MinSeverity)
	// Seed the destination list from the legacy singletons the first time (before any destination is
	// stored), then normalize. After that, Destinations is authoritative.
	if len(s.Destinations) == 0 {
		s.Destinations = migrateLegacyDestinations(s)
	}
	s.Destinations = normalizeDestinations(s.Destinations)
	return s
}

func validateNotifSettings(s NotificationSettings) error {
	// The legacy singletons are still validated (in case an old client PUTs the whole blob), but the
	// destinations are what deliver.
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
	return validateDestinations(s.Destinations)
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
