package notification

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/mysayasan/kopiv2/domain/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
	infranotif "github.com/mysayasan/kopiv2/infra/notification"
	"github.com/mysayasan/kopiv2/infra/telemetry"
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
	CategoryDeviceAlert = infranotif.CategoryDeviceAlert
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

	// Metrics (optional) counts delivery attempts per channel and outcome. Delivery is
	// at-most-once — a full queue or exhausted retries drops the notification — so
	// without this a drop is only ever a log line nobody reads.
	Metrics telemetry.Metrics
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

// DestinationConfig is one outbound delivery target in the per-destination model.
// Each destination becomes its own filtered channel (own severity floor + category
// subscription) and is addressable by Id for tailored per-destination delivery.
type DestinationConfig struct {
	Id          string
	Type        string // "webhook" | "telegram" | "mqtt"
	URL         string
	Headers     map[string]string
	BotToken    string
	ChatID      string
	MinSeverity Severity
	// Categories is an allow-list of notification categories (empty = all).
	Categories []string
	QueueSize  int
	// Mqtt holds the MQTT-specific settings when Type is "mqtt".
	Mqtt MqttDestinationConfig
}

// MqttDestinationConfig holds the MQTT broker/publish settings for a destination.
type MqttDestinationConfig struct {
	BrokerURL          string
	Topic              string
	ClientID           string
	QoS                byte
	Retain             bool
	Username           string
	Password           string
	CACert             string
	ClientCert         string
	ClientKey          string
	InsecureSkipVerify bool
}

// ChannelConfig is the runtime-reconfigurable set of outbound delivery channels.
// Destinations is the per-destination model; Webhook/Telegram are the legacy
// singletons, used only as a fallback when Destinations is empty.
type ChannelConfig struct {
	Webhook      WebhookConfig
	Telegram     TelegramConfig
	Destinations []DestinationConfig
}

// Service is a reusable, database-backed notification facade: it owns the hub,
// the always-on channels, persistence, the live SSE stream, and a reloadable
// set of outbound delivery channels. Apps publish through it and read history
// from it.
type Service struct {
	repo     dbsql.IGenericRepo[entities.Notification]
	rollups  dbsql.IGenericRepo[entities.NotificationRollup]
	hub      *infranotif.Hub
	sse      *infranotif.SSEChannel
	outbound *infranotif.ReloadableChannel
	logger   infranotif.Logger

	// destByID maps a destination Id to its outbound channel for tailored
	// per-destination delivery (DeliverTo). Rebuilt on every Configure.
	destMu   sync.RWMutex
	destByID map[string]Channel
}

// NewService builds a Service with persistence, log, and SSE channels plus a
// reloadable outbound channel pre-registered on a single hub. Call Configure to
// enable webhook/telegram delivery.
func NewService(repo dbsql.IGenericRepo[entities.Notification], opts Options) *Service {
	hub := infranotif.NewHub(infranotif.WithLogger(opts.Logger))
	hub.SetMetrics(opts.Metrics)
	sse := infranotif.NewSSEChannel(infranotif.SSEOptions{ClientBuffer: opts.SSEClientBuffer})
	outbound := infranotif.NewReloadableChannel("outbound")

	// Persist first so the record exists before delivery side effects.
	hub.Register(infranotif.NewStoreChannel(NewStore(repo)))
	hub.Register(infranotif.NewLogChannel(opts.Logger))
	hub.Register(sse)
	hub.Register(outbound)

	return &Service{repo: repo, hub: hub, sse: sse, outbound: outbound, logger: opts.Logger}
}

// WithRollups attaches the notification-rollup read repository, enabling the
// dashboard analytics reads (Heatmap and, later, baseline queries). It is set
// separately from NewService so control-plane apps that never aggregate can omit
// it. Returns the service for chaining.
func (s *Service) WithRollups(rollups dbsql.IGenericRepo[entities.NotificationRollup]) *Service {
	s.rollups = rollups
	return s
}

// Configure rebuilds the outbound delivery channels from cfg — one filtered
// channel per destination (own severity floor + category subscription). The
// channels both back the generic hub fan-out (for non-alert notifications, each
// self-filtering) and are addressable by Id for tailored per-destination
// delivery via DeliverTo. Previously-active channels are drained and closed.
// Safe to call at runtime whenever notification settings change.
func (s *Service) Configure(cfg ChannelConfig) {
	dests := cfg.Destinations
	if len(dests) == 0 {
		dests = legacyDestinations(cfg)
	}

	var channels []Channel
	byID := make(map[string]Channel, len(dests))
	for _, d := range dests {
		inner := s.buildDestinationChannel(d)
		if inner == nil {
			continue
		}
		fc := &filteredChannel{
			inner:  inner,
			filter: Filter{MinSeverity: d.MinSeverity, Categories: d.Categories},
		}
		channels = append(channels, fc)
		if d.Id != "" {
			byID[d.Id] = fc
		}
	}

	s.destMu.Lock()
	s.destByID = byID
	s.destMu.Unlock()
	s.outbound.Set(channels)
}

// buildDestinationChannel builds the underlying delivery channel for a
// destination. Severity/category filtering is applied by the wrapping
// filteredChannel, so the inner channel is built without a severity floor.
func (s *Service) buildDestinationChannel(d DestinationConfig) Channel {
	switch d.Type {
	case "webhook":
		if d.URL == "" {
			return nil
		}
		return infranotif.NewWebhookChannel(infranotif.WebhookOptions{
			URL:       d.URL,
			Headers:   d.Headers,
			QueueSize: d.QueueSize,
			Logger:    s.logger,
		})
	case "telegram":
		if d.BotToken == "" || d.ChatID == "" {
			return nil
		}
		return infranotif.NewTelegramChannel(infranotif.TelegramOptions{
			BotToken:  d.BotToken,
			ChatID:    d.ChatID,
			QueueSize: d.QueueSize,
			Logger:    s.logger,
		})
	case "mqtt":
		if d.Mqtt.BrokerURL == "" || d.Mqtt.Topic == "" {
			return nil
		}
		return infranotif.NewMqttChannel(infranotif.MqttOptions{
			BrokerURL:          d.Mqtt.BrokerURL,
			Topic:              d.Mqtt.Topic,
			ClientID:           d.Mqtt.ClientID,
			QoS:                d.Mqtt.QoS,
			Retain:             d.Mqtt.Retain,
			Username:           d.Mqtt.Username,
			Password:           d.Mqtt.Password,
			CACert:             d.Mqtt.CACert,
			ClientCert:         d.Mqtt.ClientCert,
			ClientKey:          d.Mqtt.ClientKey,
			InsecureSkipVerify: d.Mqtt.InsecureSkipVerify,
			QueueSize:          d.QueueSize,
			Logger:             s.logger,
		})
	default:
		return nil
	}
}

// DeliverTo delivers a notification to a single destination by Id, bypassing the
// hub (so it is not re-persisted) and applying that destination's filter. Used
// for tailored per-destination vision-alert payloads. Unknown ids are no-ops.
func (s *Service) DeliverTo(ctx context.Context, destinationId string, n Notification) {
	s.destMu.RLock()
	ch := s.destByID[destinationId]
	s.destMu.RUnlock()
	if ch == nil {
		return
	}
	if err := ch.Send(ctx, n); err != nil && s.logger != nil {
		s.logger.Warnf("notification", "deliver to destination %s failed: %v", destinationId, err)
	}
}

// legacyDestinations adapts the deprecated single Webhook/Telegram config into
// the destination model, used only when no explicit destinations are supplied.
func legacyDestinations(cfg ChannelConfig) []DestinationConfig {
	var out []DestinationConfig
	if cfg.Webhook.Enabled {
		out = append(out, DestinationConfig{
			Id: "default-webhook", Type: "webhook", URL: cfg.Webhook.URL,
			Headers: cfg.Webhook.Headers, MinSeverity: cfg.Webhook.MinSeverity, QueueSize: cfg.Webhook.QueueSize,
		})
	}
	if cfg.Telegram.Enabled {
		out = append(out, DestinationConfig{
			Id: "default-telegram", Type: "telegram", BotToken: cfg.Telegram.BotToken,
			ChatID: cfg.Telegram.ChatID, MinSeverity: cfg.Telegram.MinSeverity, QueueSize: cfg.Telegram.QueueSize,
		})
	}
	return out
}

// filteredChannel wraps a delivery channel with a Filter (severity + category
// subscription) and skips Internal notifications, so the generic hub fan-out
// respects each destination's subscription and the canonical copy of a vision
// alert is not re-delivered (its per-destination copies are sent directly).
type filteredChannel struct {
	inner  Channel
	filter Filter
}

func (c *filteredChannel) Name() string { return c.inner.Name() }

func (c *filteredChannel) Send(ctx context.Context, n Notification) error {
	if n.Internal {
		return nil
	}
	if !c.filter.Allows(n) {
		return nil
	}
	return c.inner.Send(ctx, n)
}

func (c *filteredChannel) Close() error {
	if closer, ok := c.inner.(io.Closer); ok {
		return closer.Close()
	}
	return nil
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
// only unread notifications are returned. cameraId > 0 filters by camera; a
// non-empty category and/or source narrows to one kind (e.g. category
// "health.check", or source "machine-health-monitor" to separate machine-health
// from other system events).
func (s *Service) List(ctx context.Context, limit, offset uint64, cameraId int64, unreadOnly bool, category, source string) ([]*entities.Notification, uint64, error) {
	var filters []sqldataenums.Filter
	if cameraId > 0 {
		filters = append(filters, sqldataenums.Filter{FieldName: "CameraId", Compare: sqldataenums.Equal, Value: cameraId})
	}
	if unreadOnly {
		filters = append(filters, sqldataenums.Filter{FieldName: "IsRead", Compare: sqldataenums.Equal, Value: false})
	}
	if category != "" {
		filters = append(filters, sqldataenums.Filter{FieldName: "Category", Compare: sqldataenums.Equal, Value: category})
	}
	if source != "" {
		filters = append(filters, sqldataenums.Filter{FieldName: "Source", Compare: sqldataenums.Equal, Value: source})
	}
	return s.repo.Get(ctx, "", limit, offset, filters, listSorters())
}

// ListSince returns notifications created at or after `since` (unix seconds), OLDEST-first,
// capped at limit (default/max 500). It is the replay query a fleet control plane uses to
// catch up on a node's notifications it missed while the control channel was disconnected:
// oldest-first + a (CreatedAt, Id) sort so a caller can page by advancing `since` without
// skipping rows. Unlike List it applies no read/camera/category filter — the caller wants the
// node's full feed for the window and dedups on its own side.
func (s *Service) ListSince(ctx context.Context, since int64, limit uint64) ([]*entities.Notification, uint64, error) {
	if limit == 0 || limit > 500 {
		limit = 500
	}
	filters := []sqldataenums.Filter{{FieldName: "CreatedAt", Compare: sqldataenums.GreaterThanOrEqualTo, Value: since}}
	sorters := []sqldataenums.Sorter{
		{FieldName: "CreatedAt", Sort: sqldataenums.ASC},
		{FieldName: "Id", Sort: sqldataenums.ASC},
	}
	return s.repo.Get(ctx, "", limit, 0, filters, sorters)
}

// MarkReadByRef marks every unread notification that references the given record
// (refType + refId) as read, returning how many were updated. It keeps the feed
// in sync when the underlying record is resolved elsewhere — e.g. acknowledging a
// detection's AlertEvent also dismisses its notification, so it cannot linger as
// unread after the alert is already handled.
func (s *Service) MarkReadByRef(ctx context.Context, refType string, refId int64, userId int64) (int, error) {
	if refType == "" || refId <= 0 {
		return 0, nil
	}
	filters := []sqldataenums.Filter{
		{FieldName: "RefType", Compare: sqldataenums.Equal, Value: refType},
		{FieldName: "RefId", Compare: sqldataenums.Equal, Value: refId},
		{FieldName: "IsRead", Compare: sqldataenums.Equal, Value: false},
	}
	items, _, err := s.repo.Get(ctx, "", 100, 0, filters, nil)
	if err != nil {
		return 0, err
	}
	now := time.Now().UTC().Unix()
	count := 0
	for _, n := range items {
		n.IsRead = true
		n.ReadBy = userId
		n.ReadAt = now
		n.UpdatedBy = userId
		n.UpdatedAt = now
		if _, err := s.repo.UpdateById(ctx, "", *n); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
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
