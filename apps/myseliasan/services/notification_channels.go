package services

import (
	"strings"

	"github.com/mysayasan/kopiv2/domain/notification"
	"github.com/mysayasan/kopiv2/infra/config"
)

// This file gives the control plane an OUTBOUND leg.
//
// Until now myseliasan built a notification Service and never called Configure,
// so the fleet feed was persist + log + SSE only: every node-offline, every
// relayed alert, every monitoring gap reached a browser and nowhere else. An
// operator watching fifty sites had to be looking at the screen to learn that one
// of them had gone dark, and the `notification.webhook` / `notification.telegram`
// blocks that have been in the config model all along quietly did nothing.
//
// NotificationChannelConfig maps the config into the delivery layer, so those
// blocks become true and email works alongside them.

// NotificationChannelConfig builds the outbound delivery configuration from app
// config. Every destination is off unless explicitly enabled, so an existing
// install that upgrades onto this build starts delivering nothing new.
func NotificationChannelConfig(cfg *config.AppConfigModel) notification.ChannelConfig {
	if cfg == nil {
		return notification.ChannelConfig{}
	}
	out := notification.ChannelConfig{
		Smtp: notification.SmtpConfig{
			Enabled:     cfg.Smtp.Enabled,
			Host:        strings.TrimSpace(cfg.Smtp.Host),
			Port:        cfg.Smtp.Port,
			From:        strings.TrimSpace(cfg.Smtp.From),
			Username:    strings.TrimSpace(cfg.Smtp.Username),
			Password:    cfg.Smtp.Password,
			UseStartTls: cfg.Smtp.UseStartTls,
		},
	}
	if out.Smtp.Port <= 0 || out.Smtp.Port > 65535 {
		// 587 (submission), not 25: the target is an internal submission endpoint
		// and 25 is blocked outbound on most networks.
		out.Smtp.Port = 587
	}

	n := cfg.Notification

	if boolValue(n.Webhook.Enabled, false) && strings.TrimSpace(n.Webhook.URL) != "" {
		out.Destinations = append(out.Destinations, notification.DestinationConfig{
			Id:          "config-webhook",
			Type:        "webhook",
			URL:         strings.TrimSpace(n.Webhook.URL),
			Headers:     n.Webhook.Headers,
			MinSeverity: notification.Severity(strings.ToLower(strings.TrimSpace(n.Webhook.MinSeverity))).Normalize(),
		})
	}

	if boolValue(n.Telegram.Enabled, false) &&
		strings.TrimSpace(n.Telegram.BotToken) != "" && strings.TrimSpace(n.Telegram.ChatId) != "" {
		out.Destinations = append(out.Destinations, notification.DestinationConfig{
			Id:          "config-telegram",
			Type:        "telegram",
			BotToken:    strings.TrimSpace(n.Telegram.BotToken),
			ChatID:      strings.TrimSpace(n.Telegram.ChatId),
			MinSeverity: notification.Severity(strings.ToLower(strings.TrimSpace(n.Telegram.MinSeverity))).Normalize(),
		})
	}

	if n.Email.Enabled {
		if to := splitList(n.Email.To); len(to) > 0 {
			out.Destinations = append(out.Destinations, notification.DestinationConfig{
				Id:          "config-email",
				Type:        "email",
				MinSeverity: notification.Severity(strings.ToLower(strings.TrimSpace(n.Email.MinSeverity))).Normalize(),
				Categories:  splitList(n.Email.Categories),
				Email: notification.EmailDestinationConfig{
					To:            to,
					SubjectPrefix: strings.TrimSpace(n.Email.SubjectPrefix),
					// Never set on the control plane: a node's notification crosses the
					// control channel as JSON, and Notification.Attachment is json:"-",
					// so no image survives the hop. Claiming to attach one would produce
					// mail that promises evidence it does not carry.
					IncludeSnapshot: false,
				},
			})
		}
	}

	return out
}

// splitList splits a comma-separated config value into trimmed, de-duplicated
// entries, preserving order. Returns nil when nothing valid remains.
func splitList(v string) []string {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	out := make([]string, 0, 4)
	seen := map[string]bool{}
	for _, part := range strings.Split(v, ",") {
		item := strings.TrimSpace(part)
		if item == "" {
			continue
		}
		key := strings.ToLower(item)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, item)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// looksLikeEmailAddress is a deliberately loose shape check used by the settings
// editor: exactly one @, something either side, a dotted domain, and no
// whitespace or CR/LF. It is NOT an RFC 5322 validator — rejecting an unusual but
// legitimate internal address would be a worse failure than accepting one the
// relay later refuses, which the delivery path already reports per recipient. The
// CR/LF part is the one that must not be relaxed: it is what keeps a recipient
// field out of the mail headers.
func looksLikeEmailAddress(addr string) bool {
	if addr == "" || strings.ContainsAny(addr, " \t\r\n,;<>") {
		return false
	}
	local, domain, ok := strings.Cut(addr, "@")
	if !ok || local == "" || domain == "" || strings.Contains(domain, "@") {
		return false
	}
	if strings.HasPrefix(domain, ".") || strings.HasSuffix(domain, ".") {
		return false
	}
	return strings.Contains(domain, ".")
}
