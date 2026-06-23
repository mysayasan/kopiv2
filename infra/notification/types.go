// Package notification provides a small, app-agnostic notification hub.
//
// Producers publish a Notification to a Hub; the Hub fans it out to any number
// of registered Channels (log, webhook, SSE, persistence, ...). The package has
// no dependency on a specific app, database, or HTTP framework, so it can be
// reused by any module that needs to deliver events through one pipeline.
package notification

// Severity ranks how urgent a notification is.
type Severity string

const (
	// Info is an informational notification (no action required).
	Info Severity = "info"
	// Warning is a notification that may need attention.
	Warning Severity = "warning"
	// Critical is a notification that needs immediate attention.
	Critical Severity = "critical"
)

// rank returns a comparable weight for a severity, used by Filter.
func (s Severity) rank() int {
	switch s {
	case Critical:
		return 3
	case Warning:
		return 2
	case Info:
		return 1
	default:
		return 0
	}
}

// Normalize returns a known severity, defaulting unknown values to Info.
func (s Severity) Normalize() Severity {
	switch s {
	case Critical, Warning, Info:
		return s
	default:
		return Info
	}
}

// Common notification categories. Apps may define their own values; these are
// the cross-cutting ones the core understands.
const (
	// CategoryVisionAlert is raised when an AI detection rule triggers.
	CategoryVisionAlert = "vision.alert"
	// CategoryHealthCheck is raised by health/diagnostic checks.
	CategoryHealthCheck = "health.check"
	// CategorySystem is raised for general system events.
	CategorySystem = "system"
)

// Attachment is optional binary media (e.g. a detection snapshot) carried with a
// notification. It rides on a non-serialized pointer field so it reaches only the
// outbound delivery channels that support media (Telegram photo upload, webhook
// base64) — persistence, SSE, and log channels marshal the Notification to JSON
// and therefore skip it, keeping the stored metadata and live stream lean.
type Attachment struct {
	// Filename is the suggested file name (e.g. "alert-7.jpg").
	Filename string
	// ContentType is the MIME type (e.g. "image/jpeg").
	ContentType string
	// Data is the raw media bytes.
	Data []byte
}

// Notification is the single event shape every producer funnels into the hub.
//
// Data carries arbitrary structured context (bounding box, zone polygon,
// confidence, ...) so consumers get the full detail without the engine needing
// to understand any of those fields. Persistence channels typically serialize
// Data to a JSON metadata column.
type Notification struct {
	// ID uniquely identifies the notification. If empty, the hub assigns one.
	ID string `json:"id"`
	// Category groups notifications by source, e.g. "vision.alert".
	Category string `json:"category"`
	// Severity ranks urgency.
	Severity Severity `json:"severity"`
	// Title is a short human-readable headline.
	Title string `json:"title"`
	// Body is the longer human-readable message.
	Body string `json:"body"`
	// Source names the subsystem that produced the notification.
	Source string `json:"source"`
	// CameraId is optional context linking the event to a camera (0 if none).
	CameraId int64 `json:"cameraId,omitempty"`
	// RefType and RefId optionally link back to a domain record (e.g. an
	// AlertEvent) so consumers can fetch the full detail.
	RefType string `json:"refType,omitempty"`
	RefId   int64  `json:"refId,omitempty"`
	// Link is an optional deep-link the UI can redirect to.
	Link string `json:"link,omitempty"`
	// Data carries arbitrary structured properties (bounding box, etc.).
	Data map[string]any `json:"data,omitempty"`
	// Attachment is optional binary media delivered only to media-capable outbound
	// channels. It is json:"-" so persistence, SSE, and log channels skip it.
	Attachment *Attachment `json:"-"`
	// Internal marks a notification that should be persisted/streamed (store, log,
	// SSE) but NOT delivered to outbound destination channels. Used for the
	// canonical copy of a vision alert, whose external delivery is handled by
	// separately rendered per-destination copies. It is json:"-" so it never
	// leaks into stored metadata or the SSE stream.
	Internal bool `json:"-"`
	// CreatedAt is a unix timestamp (seconds). If zero, the hub fills it.
	CreatedAt int64 `json:"createdAt"`
}
