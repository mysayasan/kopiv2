package notification

import "context"

// Channel is one delivery sink for notifications. Implementations must be safe
// for concurrent use and should never block for long: slow I/O (HTTP, etc.)
// belongs behind the channel's own internal queue so the hub stays responsive.
type Channel interface {
	// Name identifies the channel in logs and diagnostics.
	Name() string
	// Send delivers one notification. A returned error is logged by the hub but
	// never aborts delivery to other channels.
	Send(ctx context.Context, n Notification) error
}

// Filter decides whether a channel should receive a given notification. The
// zero value allows everything.
type Filter struct {
	// MinSeverity drops notifications below this severity. Empty means no floor.
	MinSeverity Severity
	// Categories is an allow-list of categories. Empty means all categories.
	Categories []string
}

// Allows reports whether the notification passes the filter.
func (f Filter) Allows(n Notification) bool {
	if f.MinSeverity != "" && n.Severity.rank() < f.MinSeverity.rank() {
		return false
	}
	if len(f.Categories) == 0 {
		return true
	}
	for _, c := range f.Categories {
		if c == n.Category {
			return true
		}
	}
	return false
}

// Logger is the minimal logging surface the hub and channels need. It is
// satisfied by infra/logging.Logger, but kept local so this package stays
// dependency-free and reusable.
type Logger interface {
	Infof(source string, format string, args ...any)
	Warnf(source string, format string, args ...any)
	Errorf(source string, format string, args ...any)
}
