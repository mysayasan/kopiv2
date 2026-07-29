package middlewares

import (
	"context"
	"fmt"
	"log"
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mysayasan/kopiv2/domain/entities"
	apiaccessenums "github.com/mysayasan/kopiv2/domain/enums/apiaccess"
	memcacheenums "github.com/mysayasan/kopiv2/domain/enums/memcache"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
	"github.com/mysayasan/kopiv2/infra/cache"
)

type apiEndpointLister interface {
	Get(ctx context.Context, limit uint64, offset uint64, filters []sqldataenums.Filter, sorters []sqldataenums.Sorter) ([]*entities.ApiEndpoint, uint64, error)
}

type RateLimitTierConfig struct {
	Enabled  bool
	Requests int64
	Window   time.Duration
}

type RateLimitConfig struct {
	Enabled          bool
	EndpointCacheTTL time.Duration
	// TrustedProxies are IPs/CIDRs permitted to declare the real client address
	// via X-Forwarded-For / X-Real-IP. Empty = trust none (use the direct peer).
	TrustedProxies []string
	DevOnly        RateLimitTierConfig
	AuthOnly       RateLimitTierConfig
	Public         RateLimitTierConfig
}

type RateLimitMidware struct {
	endpoints      apiEndpointLister
	store          cache.Store
	auth           *AuthMidware
	config         RateLimitConfig
	trustedProxies []*net.IPNet
}

type endpointTierEntry struct {
	Host       string                    `json:"host"`
	Path       string                    `json:"path"`
	AccessTier apiaccessenums.AccessTier `json:"accessTier"`
}

func NewRateLimit(endpoints apiEndpointLister, store cache.Store, auth *AuthMidware, config RateLimitConfig) *RateLimitMidware {
	if config.EndpointCacheTTL <= 0 {
		config.EndpointCacheTTL = 30 * time.Second
	}
	return &RateLimitMidware{
		endpoints:      endpoints,
		store:          store,
		auth:           auth,
		config:         config,
		trustedProxies: parseTrustedProxies(config.TrustedProxies),
	}
}

// parseTrustedProxies delegates to the shared implementation in clientip.go so the rate
// limiter and the audit trail cannot drift on which peers may declare a client address.
func parseTrustedProxies(entries []string) []*net.IPNet {
	return ParseTrustedProxies(entries)
}

func (m *RateLimitMidware) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if m == nil || !m.config.Enabled {
			next.ServeHTTP(w, r)
			return
		}
		if m.store == nil || m.endpoints == nil {
			controllers.SendError(w, controllers.ErrInternalServerError, "rate limiter is not configured")
			return
		}

		// myseliasan fans an entire managed node's UI through a few per-node control-plane
		// paths: the proxy tunnel (dashboard/cameras/settings/notifications), WebRTC live-view
		// signaling, and range-streamed recording playback (MANY Range requests per clip).
		// This limiter keys by the matched endpoint path, so all of those collapse into the
		// single /api/nodes bucket — a normal node session then trips 429 and the node "goes
		// online but can't load data". These surfaces are authenticated + per-node-access-gated
		// at their own handlers AND the node enforces its own limits downstream, so they are
		// exempt from this generic per-path limiter. Node CRUD/adopt/release/wipe/access stay
		// limited (they don't match here).
		if r != nil && r.URL != nil && isNodeFanoutPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		accessTier, matchedPath, err := m.resolveAccessTier(r)
		if err != nil {
			controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
			return
		}

		tierConfig := m.tierConfig(accessTier)
		if !tierConfig.Enabled || tierConfig.Requests <= 0 || tierConfig.Window <= 0 {
			next.ServeHTTP(w, r)
			return
		}

		identity := m.identityForRequest(r, accessTier)
		key := rateLimitKey(accessTier, identity, matchedPath)
		result, err := m.store.AllowSlidingWindow(r.Context(), key, tierConfig.Requests, tierConfig.Window, time.Now().UTC())
		if err != nil {
			controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
			return
		}

		setRateLimitHeaders(w, result)
		if !result.Allowed {
			if result.RetryAfter > 0 {
				retryAfterSeconds := int64(math.Ceil(result.RetryAfter.Seconds()))
				if retryAfterSeconds < 1 {
					retryAfterSeconds = 1
				}
				w.Header().Set("Retry-After", strconv.FormatInt(retryAfterSeconds, 10))
			}
			controllers.SendError(w, controllers.ErrRateLimited, "rate limit exceeded")
			return
		}

		next.ServeHTTP(w, r)
	})
}

// isNodeFanoutPath reports whether p is one of myseliasan's high-volume per-node data
// surfaces that fan a whole node's UI through a single control-plane path and so must not
// be rate-limited as one endpoint (see the exemption in Middleware):
//   - /api/nodes/{id}/proxy[/...]              the control-tunnel proxy (dashboard/cameras/…)
//   - /api/nodes/{id}/.../webrtc/...           live-view WebRTC signaling
//   - /api/nodes/{id}/recording-stream/...     range-streamed recording playback
//
// Node CRUD/adopt/release/wipe (/api/nodes, /api/nodes/{id}), grants (/api/nodes/access),
// enroll, and self-dropped are NOT matched and stay rate-limited. No other app registers
// these path shapes.
func isNodeFanoutPath(p string) bool {
	const prefix = "/api/nodes/"
	if !strings.HasPrefix(p, prefix) {
		return false
	}
	rest := p[len(prefix):] // "{id}/..." (or just "{id}")
	slash := strings.IndexByte(rest, '/')
	if slash < 0 {
		return false // "/api/nodes/{id}" — node CRUD, stays limited
	}
	after := rest[slash:] // "/proxy/...", "/cameras/{cam}/webrtc/offer", "/recording-stream/{seg}", "/access", …
	switch {
	case after == "/proxy" || strings.HasPrefix(after, "/proxy/"):
		return true
	case strings.HasPrefix(after, "/recording-stream/"):
		return true
	case strings.Contains(after, "/webrtc/"):
		return true
	default:
		return false
	}
}

func (m *RateLimitMidware) resolveAccessTier(r *http.Request) (apiaccessenums.AccessTier, string, error) {
	entries, err := m.loadEndpointTiers(r.Context())
	if err != nil {
		return apiaccessenums.AuthOnly, r.URL.Path, err
	}

	requestHost := ""
	requestPath := ""
	if r != nil {
		requestHost = r.Host
		if r.URL != nil {
			requestPath = r.URL.Path
		}
	}

	var match *endpointTierEntry
	for idx := range entries {
		entry := entries[idx]
		if !hostMatches(entry.Host, requestHost) || !pathMatches(entry.Path, requestPath) {
			continue
		}
		if match == nil || len(strings.TrimRight(entry.Path, "/")) > len(strings.TrimRight(match.Path, "/")) {
			match = &entry
		}
	}

	if match == nil {
		return apiaccessenums.AuthOnly, requestPath, nil
	}
	return match.AccessTier, match.Path, nil
}

func (m *RateLimitMidware) loadEndpointTiers(ctx context.Context) ([]endpointTierEntry, error) {
	cacheKey := memcacheenums.GetString(memcacheenums.Mware_RateLimit_ApiEndpointTiers)
	var entries []endpointTierEntry
	found, err := m.store.Get(ctx, cacheKey, &entries)
	if err != nil {
		log.Printf("rate-limit endpoint tier cache get warning key=%s err=%v", cacheKey, err)
	} else if found {
		return entries, nil
	}

	endpoints, _, err := m.endpoints.Get(ctx, 0, 0, nil, nil)
	if err != nil {
		return nil, err
	}

	entries = make([]endpointTierEntry, 0, len(endpoints))
	for _, endpoint := range endpoints {
		if endpoint == nil || !endpoint.IsActive {
			continue
		}
		if !apiaccessenums.IsValidAccessTier(int32(endpoint.AccessTier)) {
			continue
		}
		entries = append(entries, endpointTierEntry{
			Host:       endpoint.Host,
			Path:       endpoint.Path,
			AccessTier: endpoint.AccessTier,
		})
	}

	if err := m.store.Set(ctx, cacheKey, entries, m.config.EndpointCacheTTL); err != nil {
		log.Printf("rate-limit endpoint tier cache set warning key=%s err=%v", cacheKey, err)
	}
	return entries, nil
}

func (m *RateLimitMidware) tierConfig(accessTier apiaccessenums.AccessTier) RateLimitTierConfig {
	switch accessTier {
	case apiaccessenums.DevOnly:
		return m.config.DevOnly
	case apiaccessenums.Public:
		return m.config.Public
	default:
		return m.config.AuthOnly
	}
}

func (m *RateLimitMidware) identityForRequest(r *http.Request, accessTier apiaccessenums.AccessTier) string {
	if accessTier != apiaccessenums.Public && m.auth != nil {
		if claims, err := m.auth.ClaimsFromRequest(r); err == nil && claims != nil && claims.Id > 0 {
			return fmt.Sprintf("user:%d", claims.Id)
		}
	}
	return "ip:" + m.clientIP(r)
}

// clientIP resolves the address to rate-limit by. It honors X-Forwarded-For /
// X-Real-IP ONLY when the immediate peer (RemoteAddr) is a configured trusted
// proxy; otherwise it uses the peer address directly. This prevents a directly-
// reachable client from spoofing a forwarding header to mint an unlimited number
// of buckets (which would defeat both the rate limiter and the login lockout
// that shares this keying), while still giving per-client buckets behind a real
// reverse proxy.
func (m *RateLimitMidware) clientIP(r *http.Request) string {
	return ClientIP(r, m.trustedProxies)
}

func rateLimitKey(accessTier apiaccessenums.AccessTier, identity string, path string) string {
	tierName := "auth"
	switch accessTier {
	case apiaccessenums.DevOnly:
		tierName = "dev"
	case apiaccessenums.Public:
		tierName = "public"
	}

	path = strings.TrimSpace(path)
	if path == "" {
		path = "unknown"
	}
	path = strings.Trim(path, "/")
	path = strings.ReplaceAll(path, "/", ":")
	if path == "" {
		path = "root"
	}
	return fmt.Sprintf("ratelimit:%s:%s:%s", tierName, sanitizeRateLimitKeyPart(identity), sanitizeRateLimitKeyPart(path))
}

func sanitizeRateLimitKeyPart(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	value = strings.ReplaceAll(value, ":", "_")
	value = strings.ReplaceAll(value, " ", "_")
	if value == "" {
		return "unknown"
	}
	return value
}

func setRateLimitHeaders(w http.ResponseWriter, result cache.SlidingWindowResult) {
	w.Header().Set("X-RateLimit-Limit", strconv.FormatInt(result.Limit, 10))
	w.Header().Set("X-RateLimit-Remaining", strconv.FormatInt(result.Remaining, 10))
	if result.ResetAfter > 0 {
		w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(time.Now().Add(result.ResetAfter).Unix(), 10))
	}
}

func normalizeHost(host string) string {
	host = strings.TrimSpace(strings.ToLower(host))
	if host == "*" {
		return host
	}
	if parsedHost, _, err := net.SplitHostPort(host); err == nil {
		return strings.Trim(strings.ToLower(parsedHost), "[]")
	}
	return strings.Trim(host, "[]")
}
