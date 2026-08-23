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
	// Smtp is the ONE mail relay every email destination delivers through. It is
	// seeded from the config.json smtp block on first run and thereafter edited in
	// the UI, like the rest of these settings.
	Smtp NotificationSmtpSettings `json:"smtp"`
}

// NotificationSmtpSettings is the install-wide SMTP relay backing email
// destinations. Disabled by default: an air-gapped install must never reach for
// a mail server it was not explicitly pointed at, and turning this off silences
// email without anyone having to delete their destinations.
type NotificationSmtpSettings struct {
	Enabled bool   `json:"enabled"`
	Host    string `json:"host"`
	Port    int    `json:"port"`
	// From is the sender address (falls back to Username when blank).
	From string `json:"from"`
	// Username/Password authenticate to the relay. When a username is set,
	// UseStartTls must be on — the sender refuses to transmit a credential over a
	// cleartext link.
	Username    string `json:"username"`
	Password    string `json:"password"`
	UseStartTls bool   `json:"useStartTls"`
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

// SaveDestination upserts a single destination against the PERSISTED settings
// (not a client-supplied full blob), so a save of one destination never clobbers
// another destination's stored config or the retention section. When dest.Id is
// empty a new id is assigned and the destination is appended; otherwise the row
// with that id is replaced (falling back to append if it no longer exists).
func (s *notificationSettingsService) SaveDestination(ctx context.Context, dest NotificationDestination) (NotificationDestination, NotificationSettings, error) {
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
	// Return the persisted copy of this destination (post-normalization) so the
	// caller gets the canonical id/fields back.
	for _, d := range saved.Destinations {
		if d.Id == dest.Id {
			return d, saved, nil
		}
	}
	return dest, saved, nil
}

// DeleteDestination removes one destination by id from the persisted settings,
// leaving all other sections untouched. Removing an unknown id is a no-op.
func (s *notificationSettingsService) DeleteDestination(ctx context.Context, id string) (NotificationSettings, error) {
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

// SaveRetention persists only the retention section against the stored settings,
// leaving destinations (and the legacy singletons) as they are on disk.
func (s *notificationSettingsService) SaveRetention(ctx context.Context, retention NotificationRetentionSettings) (NotificationSettings, error) {
	current, err := s.Get(ctx)
	if err != nil {
		return NotificationSettings{}, err
	}
	current.Retention = retention
	return s.Save(ctx, current)
}

// SaveSmtp persists only the mail-relay section, leaving destinations and
// retention untouched. It is its own endpoint for the same reason retention is:
// the relay is edited by whoever runs the mail server, the destinations by
// whoever decides who gets told, and a save of one must never silently rewrite
// the other from a stale copy the browser was holding.
//
// A blank incoming password keeps the stored one, so an operator can change the
// host or the port without re-typing a credential the UI never showed them.
func (s *notificationSettingsService) SaveSmtp(ctx context.Context, smtp NotificationSmtpSettings) (NotificationSettings, error) {
	current, err := s.Get(ctx)
	if err != nil {
		return NotificationSettings{}, err
	}
	if strings.TrimSpace(smtp.Password) == "" {
		smtp.Password = current.Smtp.Password
	}
	if err := validateSmtpSettings(smtp); err != nil {
		return NotificationSettings{}, err
	}
	current.Smtp = smtp
	return s.Save(ctx, current)
}

// validateSmtpSettings refuses a relay that cannot deliver. The credential rule
// is the one worth stating: the sender will not transmit a password over a link
// it has not upgraded, so a username without STARTTLS produces a relay that
// silently never delivers. Refusing it here is the difference between a
// corrected setting and an alerting path nobody knows is dead.
func validateSmtpSettings(smtp NotificationSmtpSettings) error {
	if !smtp.Enabled {
		return nil
	}
	if strings.TrimSpace(smtp.Host) == "" {
		return fmt.Errorf("an SMTP relay host is required when mail is enabled")
	}
	if strings.TrimSpace(smtp.From) == "" && strings.TrimSpace(smtp.Username) == "" {
		return fmt.Errorf("a sender address (from) is required when mail is enabled")
	}
	if smtp.Port < 0 || smtp.Port > 65535 {
		return fmt.Errorf("SMTP port must be between 1 and 65535")
	}
	if strings.TrimSpace(smtp.Username) != "" && !smtp.UseStartTls {
		return fmt.Errorf("STARTTLS must be enabled when an SMTP username is set - credentials are never sent over a cleartext link")
	}
	return nil
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
	cfg := notification.ChannelConfig{
		Smtp: notification.SmtpConfig{
			Enabled:     s.Smtp.Enabled,
			Host:        s.Smtp.Host,
			Port:        s.Smtp.Port,
			From:        s.Smtp.From,
			Username:    s.Smtp.Username,
			Password:    s.Smtp.Password,
			UseStartTls: s.Smtp.UseStartTls,
		},
	}
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
		case DestinationTypeEmail:
			dc.Email = notification.EmailDestinationConfig{
				To:            d.Email.To,
				SubjectPrefix: d.Email.SubjectPrefix,
				// Snapshot delivery reuses the destination-wide SnapshotMode rather
				// than an email-only toggle: "link" means send the reference only.
				IncludeSnapshot: !strings.EqualFold(d.SnapshotMode, SnapshotModeLink),
			}
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
	s.Smtp.Host = strings.TrimSpace(s.Smtp.Host)
	s.Smtp.From = strings.TrimSpace(s.Smtp.From)
	s.Smtp.Username = strings.TrimSpace(s.Smtp.Username)
	if s.Smtp.Port <= 0 || s.Smtp.Port > 65535 {
		// 587 (submission) rather than 25: the relay this talks to is an internal
		// submission endpoint, and 25 is blocked outbound on most networks.
		s.Smtp.Port = 587
	}
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
