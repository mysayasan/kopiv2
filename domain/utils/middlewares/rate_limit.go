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
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
	"github.com/mysayasan/kopiv2/infra/cache"
)

type apiEndpointLister interface {
	Get(ctx context.Context, limit uint64, offset uint64) ([]*entities.ApiEndpoint, uint64, error)
}

type RateLimitTierConfig struct {
	Enabled  bool
	Requests int64
	Window   time.Duration
}

type RateLimitConfig struct {
	Enabled          bool
	EndpointCacheTTL time.Duration
	DevOnly          RateLimitTierConfig
	AuthOnly         RateLimitTierConfig
	Public           RateLimitTierConfig
}

type RateLimitMidware struct {
	endpoints apiEndpointLister
	store     cache.Store
	auth      *AuthMidware
	config    RateLimitConfig
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
		endpoints: endpoints,
		store:     store,
		auth:      auth,
		config:    config,
	}
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

	endpoints, _, err := m.endpoints.Get(ctx, 0, 0)
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
	return "ip:" + clientIPFromRequest(r)
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
