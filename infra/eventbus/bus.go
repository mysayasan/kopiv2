// Package eventbus carries fire-and-forget events between instances of one app.
//
// It exists for the things a single instance learns but every instance needs to know.
// A node holds its control channel to exactly one instance, so that instance is the only
// one that sees the node's events. Two features break on that: the live notification bell
// only shows what happened to reach the instance a browser is connected to, and a fleet
// rule whose conditions span nodes attached to different instances never sees them
// together, so it never fires at all.
//
// Delivery is at-most-once and unordered, which is the honest contract for a Redis
// publish/subscribe and exactly what these consumers already tolerate: the notification
// feed is a LIVE relay whose durable copy is the database (a browser refresh reconciles
// from history), and the correlator is a best-effort matcher whose absent-clause sweep is
// driven by the passage of time rather than by any single message arriving. Anything that
// must not be lost belongs in the database, not here.
//
// Providers mirror infra/cache and infra/coordination: "memory" is an in-process fan-out
// (correct for a single instance, where publisher and subscriber are the same process),
// "redis" reaches every instance. Standalone installs therefore need no configuration and
// behave exactly as they did before this existed.
package eventbus

import (
	"context"
	"strings"
	"time"
)

// Handler receives one published payload. It runs on the bus's own goroutine, so a slow
// handler delays later events on that subscription — do the minimum here and hand off
// anything expensive.
type Handler func(payload []byte)

// Bus publishes payloads on a topic and delivers them to every subscriber, in this process
// or in another instance.
type Bus interface {
	// Publish sends payload to every subscriber of topic. It never blocks on delivery.
	Publish(ctx context.Context, topic string, payload []byte) error
	// Subscribe registers handler for topic until ctx is cancelled.
	Subscribe(ctx context.Context, topic string, handler Handler) error
	// Distributed reports whether this bus reaches other instances. False for the
	// in-process provider, which lets a caller skip work that only matters in a cluster.
	Distributed() bool
	Ping(ctx context.Context) error
	Close() error
}

// Config selects and configures a provider.
type Config struct {
	// Provider is "memory"/"inmemory" (default) or "redis".
	Provider string
	// KeyPrefix is the deployment-wide key prefix (typically "kopiv2").
	KeyPrefix string
	// AppName is what actually separates one app's topics from another's when several
	// apps share one Redis — KeyPrefix does NOT, because every app in the suite is
	// configured with the SAME prefix. Leaving the app out is the mistake that made
	// myseliasan's and myidsan's leader leases collide on a single `kopiv2:tx:lock:leader`
	// key; the same shape of bug here would cross-deliver one app's events to another,
	// silently, and only once a second app happened to pick a topic name in common.
	AppName string

	RedisAddress   string
	RedisPassword  string
	RedisDB        int
	RedisUseTLS    bool
	ConnectTimeout time.Duration
	CommandTimeout time.Duration
}

// New builds the configured bus. An unrecognised provider is an error rather than a
// silent fall back to in-process: quietly degrading to a bus that reaches nobody would
// leave a clustered deployment with a dark notification bell and rules that never fire,
// which is precisely the failure this package exists to remove.
func New(cfg Config) (Bus, string, error) {
	provider := strings.ToLower(strings.TrimSpace(cfg.Provider))
	switch provider {
	case "", "default", "memory", "inmemory":
		return NewMemoryBus(), "memory", nil
	case "redis", "rediscluster", "redis-cluster":
		return NewRedisBus(cfg), "redis", nil
	default:
		return nil, "", &UnsupportedProviderError{Provider: cfg.Provider}
	}
}

// UnsupportedProviderError reports a provider name the package does not implement.
type UnsupportedProviderError struct{ Provider string }

func (e *UnsupportedProviderError) Error() string {
	return "eventbus: unsupported provider " + e.Provider
}

// topicKey namespaces a topic by prefix AND application, mirroring the Redis locker's
// key(): `<prefix>:<app>:bus:<topic>`, with either segment omitted when empty.
func topicKey(prefix, app, topic string) string {
	prefix = strings.TrimSpace(prefix)
	app = strings.TrimSpace(app)
	switch {
	case prefix == "" && app == "":
		return "bus:" + topic
	case prefix == "":
		return app + ":bus:" + topic
	case app == "":
		return prefix + ":bus:" + topic
	default:
		return prefix + ":" + app + ":bus:" + topic
	}
}
