package services

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"

	"github.com/mysayasan/kopiv2/domain/notification"
)

// Destination types.
const (
	DestinationTypeWebhook  = "webhook"
	DestinationTypeTelegram = "telegram"
	DestinationTypeMqtt     = "mqtt"
)

// Snapshot delivery modes — how a destination receives the alert image.
const (
	// SnapshotModeInline embeds the image bytes (webhook/MQTT base64, Telegram
	// photo upload). This is the default.
	SnapshotModeInline = "inline"
	// SnapshotModeLink sends only a reference (link + snapshotPath) and no bytes,
	// so the consumer fetches the image itself — keeps payloads/broker messages
	// small.
	SnapshotModeLink = "link"
)

// knownCategories are the notification categories a destination can subscribe to.
// An empty subscription on a destination means "all categories".
var knownCategories = map[string]bool{
	notification.CategoryVisionAlert: true, // AI detection alerts
	notification.CategoryHealthCheck: true, // camera offline + machine health
	notification.CategorySystem:      true, // general system events + Test
}

// NotificationDestination is one configured delivery target. Unlike the legacy
// single webhook/telegram singletons, any number of destinations can exist, and
// each carries its OWN field-inclusion toggles (Fields) and static CustomFields,
// so different recipients can receive different payloads (e.g. one without the
// snapshot image, another tagged with a site identifier).
//
// Phase 1 (data model + migration) only persists this list — the live hub still
// delivers via the legacy singletons until the Phase 2 hub refactor consumes
// destinations.
type NotificationDestination struct {
	Id          string `json:"id"`
	Name        string `json:"name"`
	Type        string `json:"type"` // webhook | telegram
	Enabled     bool   `json:"enabled"`
	MinSeverity string `json:"minSeverity"`
	// Webhook target.
	URL string `json:"url,omitempty"`
	// Telegram target.
	BotToken string `json:"botToken,omitempty"`
	ChatId   string `json:"chatId,omitempty"`
	// Categories is this destination's notification-type subscription (an
	// allow-list of category constants: vision.alert, health.check, system). nil
	// or empty means "all categories", so migrated destinations keep receiving
	// everything they would today.
	Categories []string `json:"categories,omitempty"`
	// Fields is this destination's field-inclusion config. nil means "include
	// everything" (same default semantics as the global setting), so migrated
	// destinations keep current behavior without storing an explicit struct.
	Fields *AlertNotificationSettings `json:"fields,omitempty"`
	// SnapshotMode controls how the snapshot is delivered when the snapshot field
	// is enabled: "inline" (image bytes — base64/photo, default) or "link" (a
	// reference only, no bytes).
	SnapshotMode string `json:"snapshotMode,omitempty"`
	// CustomFields are static key/value pairs merged into the payload Data. They
	// take priority over built-in fields of the same key (custom wins).
	CustomFields []NotificationCustomField `json:"customFields,omitempty"`
	// Mqtt holds the broker/publish settings when Type is "mqtt".
	Mqtt NotificationMqttSettings `json:"mqtt,omitempty"`
}

// NotificationMqttSettings configures an MQTT destination's broker connection,
// publish topic/QoS, and optional TLS / client-certificate auth.
type NotificationMqttSettings struct {
	BrokerURL          string `json:"brokerUrl,omitempty"`
	Topic              string `json:"topic,omitempty"`
	ClientId           string `json:"clientId,omitempty"`
	Qos                int    `json:"qos,omitempty"`
	Retain             bool   `json:"retain,omitempty"`
	Username           string `json:"username,omitempty"`
	Password           string `json:"password,omitempty"`
	CaCert             string `json:"caCert,omitempty"`
	ClientCert         string `json:"clientCert,omitempty"`
	ClientKey          string `json:"clientKey,omitempty"`
	InsecureSkipVerify bool   `json:"insecureSkipVerify,omitempty"`
}

// AllowsCategory reports whether this destination subscribes to a category. An
// empty subscription means all categories are allowed.
func (d NotificationDestination) AllowsCategory(category string) bool {
	if len(d.Categories) == 0 {
		return true
	}
	for _, c := range d.Categories {
		if c == category {
			return true
		}
	}
	return false
}

// NotificationCustomField is a single static key/value added to a destination's
// payload. (Templated values — e.g. "{{ruleName}}" — are a planned follow-up;
// the schema does not need to change for that.)
type NotificationCustomField struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// migrateLegacyDestinations derives a destination list from the legacy single
// webhook/telegram settings. Called only when no destinations are stored yet, so
// existing installs keep delivering exactly as before once Phase 2 lands. A
// legacy channel becomes a destination when it is enabled or has target config,
// so a fresh, unconfigured install migrates to an empty list (no noise).
func migrateLegacyDestinations(s NotificationSettings) []NotificationDestination {
	var out []NotificationDestination
	if s.Webhook.Enabled || strings.TrimSpace(s.Webhook.URL) != "" {
		out = append(out, NotificationDestination{
			Id:          "default-webhook",
			Name:        "Webhook",
			Type:        DestinationTypeWebhook,
			Enabled:     s.Webhook.Enabled,
			MinSeverity: s.Webhook.MinSeverity,
			URL:         s.Webhook.URL,
		})
	}
	if s.Telegram.Enabled || (strings.TrimSpace(s.Telegram.BotToken) != "" && strings.TrimSpace(s.Telegram.ChatId) != "") {
		out = append(out, NotificationDestination{
			Id:          "default-telegram",
			Name:        "Telegram",
			Type:        DestinationTypeTelegram,
			Enabled:     s.Telegram.Enabled,
			MinSeverity: s.Telegram.MinSeverity,
			BotToken:    s.Telegram.BotToken,
			ChatId:      s.Telegram.ChatId,
		})
	}
	return out
}

func normalizeDestinations(in []NotificationDestination) []NotificationDestination {
	if len(in) == 0 {
		return in
	}
	out := make([]NotificationDestination, 0, len(in))
	seenID := map[string]bool{}
	for _, d := range in {
		d.Id = strings.TrimSpace(d.Id)
		if d.Id == "" || seenID[d.Id] {
			d.Id = newDestinationID()
		}
		seenID[d.Id] = true
		d.Name = strings.TrimSpace(d.Name)
		d.Type = strings.ToLower(strings.TrimSpace(d.Type))
		d.MinSeverity = normalizeSeverityString(d.MinSeverity)
		d.URL = strings.TrimSpace(d.URL)
		d.BotToken = strings.TrimSpace(d.BotToken)
		d.ChatId = strings.TrimSpace(d.ChatId)
		if d.Name == "" {
			d.Name = titleizeSlug(d.Type)
		}
		d.Categories = normalizeCategories(d.Categories)
		if strings.EqualFold(strings.TrimSpace(d.SnapshotMode), SnapshotModeLink) {
			d.SnapshotMode = SnapshotModeLink
		} else {
			d.SnapshotMode = SnapshotModeInline
		}
		d.CustomFields = normalizeCustomFields(d.CustomFields)
		if d.Type == DestinationTypeMqtt {
			d.Mqtt.BrokerURL = strings.TrimSpace(d.Mqtt.BrokerURL)
			d.Mqtt.Topic = strings.TrimSpace(d.Mqtt.Topic)
			d.Mqtt.ClientId = strings.TrimSpace(d.Mqtt.ClientId)
			if d.Mqtt.Qos < 0 || d.Mqtt.Qos > 2 {
				d.Mqtt.Qos = 1
			}
		}
		out = append(out, d)
	}
	return out
}

// normalizeCategories lowercases/trims/dedups a category subscription and drops
// unknown values. Returns nil when nothing valid remains (= all categories).
func normalizeCategories(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	seen := map[string]bool{}
	for _, c := range in {
		c = strings.ToLower(strings.TrimSpace(c))
		if c == "" || seen[c] || !knownCategories[c] {
			continue
		}
		seen[c] = true
		out = append(out, c)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func normalizeCustomFields(in []NotificationCustomField) []NotificationCustomField {
	if len(in) == 0 {
		return nil
	}
	out := make([]NotificationCustomField, 0, len(in))
	seen := map[string]bool{}
	for _, f := range in {
		key := strings.TrimSpace(f.Key)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, NotificationCustomField{Key: key, Value: strings.TrimSpace(f.Value)})
	}
	return out
}

func validateDestinations(in []NotificationDestination) error {
	for _, d := range in {
		switch d.Type {
		case DestinationTypeWebhook:
			if d.Enabled {
				if d.URL == "" {
					return fmt.Errorf("destination %q: webhook url is required when enabled", d.Name)
				}
				parsed, err := url.ParseRequestURI(d.URL)
				if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
					return fmt.Errorf("destination %q: webhook url must be a valid http(s) URL", d.Name)
				}
			}
		case DestinationTypeTelegram:
			if d.Enabled && (d.BotToken == "" || d.ChatId == "") {
				return fmt.Errorf("destination %q: telegram botToken and chatId are required when enabled", d.Name)
			}
		case DestinationTypeMqtt:
			if d.Enabled && (d.Mqtt.BrokerURL == "" || d.Mqtt.Topic == "") {
				return fmt.Errorf("destination %q: mqtt brokerUrl and topic are required when enabled", d.Name)
			}
			if d.Enabled && (d.Mqtt.ClientCert != "") != (d.Mqtt.ClientKey != "") {
				return fmt.Errorf("destination %q: mqtt client certificate and key must be provided together", d.Name)
			}
		default:
			return fmt.Errorf("destination %q: unknown type %q (want webhook, telegram, or mqtt)", d.Name, d.Type)
		}
	}
	return nil
}

// newDestinationID returns a short random hex id for a destination row.
func newDestinationID() string {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		// rand failure is effectively impossible; fall back to a fixed prefix so
		// callers still get a non-empty id (deduped by the caller).
		return "dst-0"
	}
	return "dst-" + hex.EncodeToString(b)
}
