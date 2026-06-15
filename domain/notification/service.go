package notification

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/mysayasan/kopiv2/domain/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
	infranotif "github.com/mysayasan/kopiv2/infra/notification"
)

// Re-export the engine's core types and severities so apps depend on this one
// package rather than reaching into infra directly.
type (
	Notification = infranotif.Notification
	Attachment   = infranotif.Attachment
	Severity     = infranotif.Severity
	Channel      = infranotif.Channel
	Filter       = infranotif.Filter
)

const (
	Info     = infranotif.Info
	Warning  = infranotif.Warning
	Critical = infranotif.Critical
)

const (
	CategoryVisionAlert = infranotif.CategoryVisionAlert
	CategoryHealthCheck = infranotif.CategoryHealthCheck
	CategorySystem      = infranotif.CategorySystem
)

// Options configures the always-on channel set built by NewService. Outbound
// delivery channels (webhook, telegram) are applied separately via Configure so
// they can be reconfigured at runtime.
type Options struct {
	// Logger receives delivery diagnostics and powers the log channel.
	Logger infranotif.Logger

	// SSEClientBuffer is the per-connection SSE queue depth (0 = engine default).
	SSEClientBuffer int
}

// WebhookConfig configures the outbound webhook delivery channel.
type WebhookConfig struct {
	Enabled     bool
	URL         string
	Headers     map[string]string
	MinSeverity Severity
	QueueSize   int
}

// TelegramConfig configures the outbound Telegram bot delivery channel.
type TelegramConfig struct {
	Enabled     bool
	BotToken    string
	ChatID      string
	MinSeverity Severity
	QueueSize   int
}

// ChannelConfig is the runtime-reconfigurable set of outbound delivery channels.
type ChannelConfig struct {
	Webhook  WebhookConfig
	Telegram TelegramConfig
}

// Service is a reusable, database-backed notification facade: it owns the hub,
// the always-on channels, persistence, the live SSE stream, and a reloadable
// set of outbound delivery channels. Apps publish through it and read history
// from it.
type Service struct {
	repo     dbsql.IGenericRepo[entities.Notification]
	hub      *infranotif.Hub
	sse      *infranotif.SSEChannel
	outbound *infranotif.ReloadableChannel
	logger   infranotif.Logger
}

// NewService builds a Service with persistence, log, and SSE channels plus a
// reloadable outbound channel pre-registered on a single hub. Call Configure to
// enable webhook/telegram delivery.
func NewService(repo dbsql.IGenericRepo[entities.Notification], opts Options) *Service {
	hub := infranotif.NewHub(infranotif.WithLogger(opts.Logger))
	sse := infranotif.NewSSEChannel(infranotif.SSEOptions{ClientBuffer: opts.SSEClientBuffer})
	outbound := infranotif.NewReloadableChannel("outbound")

	// Persist first so the record exists before delivery side effects.
	hub.Register(infranotif.NewStoreChannel(NewStore(repo)))
	hub.Register(infranotif.NewLogChannel(opts.Logger))
	hub.Register(sse)
	hub.Register(outbound)

	return &Service{repo: repo, hub: hub, sse: sse, outbound: outbound, logger: opts.Logger}
}

// Configure replaces the outbound delivery channels (webhook, telegram) with a
// new set built from cfg. Previously-active channels are drained and closed.
// Safe to call at runtime whenever notification settings change.
func (s *Service) Configure(cfg ChannelConfig) {
	var channels []Channel
	if cfg.Webhook.Enabled {
		channels = append(channels, infranotif.NewWebhookChannel(infranotif.WebhookOptions{
			URL:         cfg.Webhook.URL,
			Headers:     cfg.Webhook.Headers,
			MinSeverity: cfg.Webhook.MinSeverity,
			QueueSize:   cfg.Webhook.QueueSize,
			Logger:      s.logger,
		}))
	}
	if cfg.Telegram.Enabled {
		channels = append(channels, infranotif.NewTelegramChannel(infranotif.TelegramOptions{
			BotToken:    cfg.Telegram.BotToken,
			ChatID:      cfg.Telegram.ChatID,
			MinSeverity: cfg.Telegram.MinSeverity,
			QueueSize:   cfg.Telegram.QueueSize,
			Logger:      s.logger,
		}))
	}
	s.outbound.Set(channels)
}

// Publish delivers a notification through the hub (persist + log + SSE +
// outbound). It returns the normalized notification including its assigned ID.
func (s *Service) Publish(ctx context.Context, n Notification) Notification {
	return s.hub.Publish(ctx, n)
}

// Register adds an extra delivery channel (e.g. email, push) at runtime.
func (s *Service) Register(channel Channel, filters ...Filter) {
	s.hub.Register(channel, filters...)
}

// StreamHandler returns the SSE handler to mount on an authenticated route.
func (s *Service) StreamHandler() http.Handler { return s.sse }

// List returns persisted notifications newest-first. When unreadOnly is true,
// only unread notifications are returned. cameraId > 0 filters by camera.
func (s *Service) List(ctx context.Context, limit, offset uint64, cameraId int64, unreadOnly bool) ([]*entities.Notification, uint64, error) {
	var filters []sqldataenums.Filter
	if cameraId > 0 {
		filters = append(filters, sqldataenums.Filter{FieldName: "CameraId", Compare: sqldataenums.Equal, Value: cameraId})
	}
	if unreadOnly {
		filters = append(filters, sqldataenums.Filter{FieldName: "IsRead", Compare: sqldataenums.Equal, Value: false})
	}
	return s.repo.Get(ctx, "", limit, offset, filters, listSorters())
}

// MarkRead flags one notification as read by a user.
func (s *Service) MarkRead(ctx context.Context, id uint64, userId int64) (*entities.Notification, error) {
	notif, err := s.repo.GetById(ctx, "", id)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC().Unix()
	notif.IsRead = true
	notif.ReadBy = userId
	notif.ReadAt = now
	notif.UpdatedBy = userId
	notif.UpdatedAt = now
	if _, err := s.repo.UpdateById(ctx, "", *notif); err != nil {
		return nil, err
	}
	return notif, nil
}

// Purge deletes notifications created before olderThan (a unix timestamp). When
// onlyRead is true, unread notifications are kept regardless of age. It returns
// the number of rows deleted.
func (s *Service) Purge(ctx context.Context, olderThan int64, onlyRead bool) (int, error) {
	if olderThan <= 0 {
		return 0, fmt.Errorf("olderThan must be greater than zero")
	}
	filters := []sqldataenums.Filter{
		{FieldName: "CreatedAt", Compare: sqldataenums.LessThan, Value: olderThan},
	}
	if onlyRead {
		filters = append(filters, sqldataenums.Filter{FieldName: "IsRead", Compare: sqldataenums.Equal, Value: true})
	}
	deleted, err := s.repo.Delete(ctx, "", filters)
	if err != nil {
		return 0, err
	}
	return int(deleted), nil
}

// PurgeOlderThanDays is a convenience wrapper over Purge using a day count.
func (s *Service) PurgeOlderThanDays(ctx context.Context, days int, onlyRead bool) (int, error) {
	if days <= 0 {
		return 0, fmt.Errorf("days must be greater than zero")
	}
	cutoff := time.Now().UTC().AddDate(0, 0, -days).Unix()
	return s.Purge(ctx, cutoff, onlyRead)
}

// Close drains and disconnects all channels. Call once during shutdown.
func (s *Service) Close(ctx context.Context) error {
	return s.hub.Close(ctx)
}
