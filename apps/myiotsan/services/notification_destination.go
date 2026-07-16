package services

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"

	"github.com/mysayasan/kopiv2/domain/notification"
)

// Notification DESTINATIONS — the per-target delivery model, ported from mymatasan and scaled to the
// IoT hub. A destination is one place an alert is delivered (a webhook, a Telegram chat, an MQTT
// topic), each with its own severity floor and category filter. It supersedes the single
// webhook/telegram toggles (which are kept only to migrate an older config forward once).
//
// The vision-only pieces mymatasan carries (per-field toggles, snapshot mode, LPR/template tokens)
// are deliberately dropped — a sensor alert has no bounding box or plate to template.

const (
	DestinationTypeWebhook  = "webhook"
	DestinationTypeTelegram = "telegram"
	DestinationTypeMqtt     = "mqtt"
)

// knownCategories is the set a destination may subscribe to — the categories myiotsan actually
// publishes. An empty Categories list on a destination means "all of them".
var knownCategories = map[string]bool{
	notification.CategoryDeviceAlert: true, // an IoT rule fired on a reading (incl. a device going offline)
	notification.CategorySystem:      true, // enrollment, actuation, sign-in security, the Test button
}

// NotificationDestination is one delivery target.
type NotificationDestination struct {
	Id          string   `json:"id"`
	Name        string   `json:"name"`
	Type        string   `json:"type"` // webhook | telegram | mqtt
	Enabled     bool     `json:"enabled"`
	MinSeverity string   `json:"minSeverity"`
	URL         string   `json:"url,omitempty"`      // webhook target
	BotToken    string   `json:"botToken,omitempty"` // telegram
	ChatId      string   `json:"chatId,omitempty"`
	Categories  []string `json:"categories,omitempty"` // nil/empty = all categories
	Mqtt        NotificationMqttSettings `json:"mqtt,omitempty"`
}

// NotificationMqttSettings is the MQTT publish target for a destination of type "mqtt". Natural for
// an IoT hub — an alert can be republished to a broker topic other systems already watch.
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

// AllowsCategory reports whether this destination should receive a notification of the given
// category. An empty Categories list means every category is allowed.
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

// migrateLegacyDestinations seeds the destination list from the old webhook/telegram singletons the
// first time (before any destination is stored), so an operator who configured delivery under the
// singleton model does not silently lose it. Only channels that are enabled or actually configured
// are carried across.
func migrateLegacyDestinations(s NotificationSettings) []NotificationDestination {
	var out []NotificationDestination
	if s.Webhook.Enabled || s.Webhook.URL != "" {
		out = append(out, NotificationDestination{
			Id: "default-webhook", Name: "Webhook", Type: DestinationTypeWebhook,
			Enabled: s.Webhook.Enabled, MinSeverity: s.Webhook.MinSeverity, URL: s.Webhook.URL,
		})
	}
	if s.Telegram.Enabled || s.Telegram.BotToken != "" {
		out = append(out, NotificationDestination{
			Id: "default-telegram", Name: "Telegram", Type: DestinationTypeTelegram,
			Enabled: s.Telegram.Enabled, MinSeverity: s.Telegram.MinSeverity,
			BotToken: s.Telegram.BotToken, ChatId: s.Telegram.ChatId,
		})
	}
	return out
}

// normalizeDestinations trims, assigns ids, dedups, and clamps every destination to a valid shape.
func normalizeDestinations(in []NotificationDestination) []NotificationDestination {
	seen := map[string]bool{}
	out := make([]NotificationDestination, 0, len(in))
	for _, d := range in {
		d.Type = strings.ToLower(strings.TrimSpace(d.Type))
		if d.Type == "" {
			d.Type = DestinationTypeWebhook
		}
		d.Id = strings.TrimSpace(d.Id)
		if d.Id == "" || seen[d.Id] {
			d.Id = newDestinationID()
		}
		seen[d.Id] = true
		if strings.TrimSpace(d.Name) == "" {
			d.Name = titleizeType(d.Type)
		}
		d.MinSeverity = normalizeSeverity(d.MinSeverity)
		d.URL = strings.TrimSpace(d.URL)
		d.BotToken = strings.TrimSpace(d.BotToken)
		d.ChatId = strings.TrimSpace(d.ChatId)
		d.Categories = normalizeCategories(d.Categories)
		d.Mqtt = normalizeMqtt(d.Mqtt)
		out = append(out, d)
	}
	return out
}

// normalizeCategories lowercases/dedups and drops any category the hub does not publish (so a stale
// category can never silently filter out everything). Returns nil when empty (= all).
func normalizeCategories(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, c := range in {
		c = strings.ToLower(strings.TrimSpace(c))
		if c == "" || seen[c] || !knownCategories[c] {
			continue
		}
		seen[c] = true
		out = append(out, c)
	}
	return out
}

func normalizeMqtt(m NotificationMqttSettings) NotificationMqttSettings {
	m.BrokerURL = strings.TrimSpace(m.BrokerURL)
	m.Topic = strings.TrimSpace(m.Topic)
	m.ClientId = strings.TrimSpace(m.ClientId)
	m.Username = strings.TrimSpace(m.Username)
	if m.Qos < 0 || m.Qos > 2 {
		m.Qos = 1
	}
	return m
}

// validateDestinations refuses an ENABLED destination that cannot deliver. A disabled one may be
// half-filled (an operator saving a draft), so it is left alone.
func validateDestinations(in []NotificationDestination) error {
	for _, d := range in {
		switch d.Type {
		case DestinationTypeWebhook:
			if d.Enabled {
				if d.URL == "" {
					return fmt.Errorf("destination %q: a webhook URL is required when enabled", d.Name)
				}
				parsed, err := url.ParseRequestURI(d.URL)
				if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
					return fmt.Errorf("destination %q: the webhook URL must be a valid http(s) URL", d.Name)
				}
			}
		case DestinationTypeTelegram:
			if d.Enabled && (d.BotToken == "" || d.ChatId == "") {
				return fmt.Errorf("destination %q: a telegram bot token and chat id are required when enabled", d.Name)
			}
		case DestinationTypeMqtt:
			if d.Enabled && (d.Mqtt.BrokerURL == "" || d.Mqtt.Topic == "") {
				return fmt.Errorf("destination %q: an MQTT broker URL and topic are required when enabled", d.Name)
			}
			if d.Enabled && (d.Mqtt.ClientCert != "") != (d.Mqtt.ClientKey != "") {
				return fmt.Errorf("destination %q: an MQTT client certificate and key must be provided together", d.Name)
			}
		default:
			return fmt.Errorf("destination %q: unknown type %q (want webhook, telegram or mqtt)", d.Name, d.Type)
		}
	}
	return nil
}

// titleizeType is the human default name for a typeless destination.
func titleizeType(t string) string {
	switch t {
	case DestinationTypeTelegram:
		return "Telegram"
	case DestinationTypeMqtt:
		return "MQTT"
	default:
		return "Webhook"
	}
}

// newDestinationID mints a short unique id. A destination is addressable by it (for per-destination
// delivery and for the delete route).
func newDestinationID() string {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return "dst-0"
	}
	return "dst-" + hex.EncodeToString(b)
}
