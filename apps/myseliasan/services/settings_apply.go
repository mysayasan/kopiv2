package services

import (
	"fmt"
	"strings"

	"github.com/mysayasan/kopiv2/infra/config"
)

// This file is the typed half of the settings editor: it validates a section's merged
// values and writes them onto the strongly-typed AppConfigModel. Values arrive either as
// native Go types (from read()) or JSON-decoded types (float64 for numbers), so every
// getter coerces both.

// validateSection rejects clearly unsafe values before anything is written. It is
// deliberately lenient about values an operator legitimately might want (e.g. disabling a
// block), and strict only where a bad value would break boot or security.
func validateSection(section string, data map[string]any) error {
	g := boundGetters(data)
	switch section {
	case "localAuth":
		if g.b("localAuth.enabled") && strings.TrimSpace(g.s("localAuth.username")) == "" {
			return fmt.Errorf("username is required when local login is enabled")
		}
	case "sso":
		if g.i("sso.sessionTtlSeconds") < 0 || g.i("sso.policyCacheTtlSeconds") < 0 {
			return fmt.Errorf("SSO TTLs cannot be negative")
		}
	case "pairing":
		for _, key := range []string{"pairing.mtlsPort", "pairing.controlPort", "pairing.mediaPort"} {
			if p := g.i(key); p != 0 && (p < 1 || p > 65535) {
				return fmt.Errorf("%s must be between 1 and 65535", strings.TrimPrefix(key, "pairing."))
			}
		}
		if addr := strings.TrimSpace(g.s("pairing.multicastAddr")); addr != "" && !strings.Contains(addr, ":") {
			return fmt.Errorf("multicast address must be host:port")
		}
	case "agent":
		mode := strings.ToLower(strings.TrimSpace(g.s("agent.llm.mode")))
		if mode != "" && mode != "off" && mode != "external" && mode != "sidecar" {
			return fmt.Errorf("LLM mode must be off, external, or sidecar")
		}
		if mode == "external" {
			ep := strings.TrimSpace(g.s("agent.llm.endpoint"))
			if ep == "" {
				return fmt.Errorf("an endpoint URL is required for external LLM mode")
			}
			if !strings.HasPrefix(ep, "http://") && !strings.HasPrefix(ep, "https://") {
				return fmt.Errorf("LLM endpoint must be an http(s) URL")
			}
		}
		if h := g.i("agent.digest.localHour"); h < 0 || h > 23 {
			return fmt.Errorf("digest hour must be between 0 and 23")
		}
		if wh := g.i("agent.digest.windowHours"); wh < 0 || wh > 168 {
			return fmt.Errorf("digest window must be between 1 and 168 hours (0 uses the default)")
		}
		if g.i("agent.digest.retentionDays") < 0 {
			return fmt.Errorf("digest retention cannot be negative")
		}
		if wd := g.i("agent.digest.weekday"); wd < 0 || wd > 6 {
			return fmt.Errorf("digest weekday must be between 0 (Sunday) and 6 (Saturday)")
		}
		if lang := strings.ToLower(strings.TrimSpace(g.s("agent.digest.language"))); lang != "" &&
			lang != "en" && lang != "ms" && lang != "zh" && lang != "ar" {
			return fmt.Errorf("digest language must be en, ms, zh, or ar")
		}
		if ts := g.i("agent.llm.timeoutSeconds"); ts < 0 || ts > 600 {
			return fmt.Errorf("LLM timeout must be between 0 and 600 seconds")
		}
		if g.i("agent.llm.maxTokens") < 0 {
			return fmt.Errorf("LLM max tokens cannot be negative")
		}
		if p := g.i("agent.llm.sidecar.port"); p != 0 && (p < 1 || p > 65535) {
			return fmt.Errorf("sidecar port must be between 1 and 65535")
		}
	case "security":
		if len(strings.TrimSpace(g.s("jwt.secret"))) < 16 {
			return fmt.Errorf("JWT secret must be at least 16 characters")
		}
		if strings.TrimSpace(g.s("allowOrigins")) == "" {
			return fmt.Errorf("allowed origins cannot be empty")
		}
		if strings.TrimSpace(g.s("tls.certPath")) == "" || strings.TrimSpace(g.s("tls.keyPath")) == "" {
			return fmt.Errorf("TLS certificate and key paths are required")
		}
		for _, tier := range []string{"devOnly", "authOnly", "public"} {
			if g.i("rateLimit."+tier+".requests") < 0 || g.i("rateLimit."+tier+".windowSeconds") < 0 {
				return fmt.Errorf("rate-limit values cannot be negative")
			}
		}
	case "storage":
		if strings.TrimSpace(g.s("fileStorage.path")) == "" {
			return fmt.Errorf("file storage path is required")
		}
		if g.i("cache.ttlSeconds") < 0 {
			return fmt.Errorf("cache TTL cannot be negative")
		}
		// An unrecognised provider is refused rather than defaulted: the host would fail
		// to build a locker and abort the boot, and discovering that from a settings save
		// is far kinder than discovering it from a process that will not come back up.
		if lp := strings.ToLower(strings.TrimSpace(g.s("transaction.lockProvider"))); lp != "" {
			switch lp {
			case "memory", "inmemory", "redis", "rediscluster", "redis-cluster", "default":
			default:
				return fmt.Errorf("lock provider must be memory or redis")
			}
		}
	case "logging":
		if g.i("logging.maxLineBytes") < 0 {
			return fmt.Errorf("max log line bytes cannot be negative")
		}
		if g.i("logging.maxFileSizeMb") < 0 {
			return fmt.Errorf("max log file size cannot be negative (use 0 for uncapped)")
		}
		if mp := strings.TrimSpace(g.s("telemetry.prometheus.metricsPath")); mp != "" && !strings.HasPrefix(mp, "/") {
			return fmt.Errorf("metrics path must start with '/'")
		}
	default:
		return fmt.Errorf("unknown settings section %q", section)
	}
	return nil
}

// applyToConfig writes a validated section's values onto the live config model so the UI
// reflects the pending change immediately (the host still needs a restart to consume them).
func applyToConfig(cfg *config.AppConfigModel, section string, data map[string]any) error {
	g := boundGetters(data)
	switch section {
	case "localAuth":
		cfg.LocalAuth.Enabled = g.b("localAuth.enabled")
		cfg.LocalAuth.Username = g.s("localAuth.username")
		cfg.LocalAuth.Password = g.s("localAuth.password")
	case "sso":
		cfg.SSO.Issuer = g.s("sso.issuer")
		cfg.SSO.Audience = g.s("sso.audience")
		cfg.SSO.SessionTTLSeconds = g.i("sso.sessionTtlSeconds")
		cfg.SSO.PolicyCacheTTLSeconds = g.i("sso.policyCacheTtlSeconds")
		cfg.SSO.ProviderBaseURL = g.s("sso.providerBaseUrl")
		cfg.SSO.CACertPath = g.s("sso.caCertPath")
		cfg.SSO.ClientID = g.s("sso.clientId")
		cfg.SSO.ClientSecret = g.s("sso.clientSecret")
		cfg.SSO.RedirectBaseURL = g.s("sso.redirectBaseUrl")
		cfg.SSO.RedirectPath = g.s("sso.redirectPath")
	case "pairing":
		enabled := g.b("pairing.enabled")
		cfg.Pairing.Enabled = &enabled
		cfg.Pairing.MulticastAddr = g.s("pairing.multicastAddr")
		cfg.Pairing.ReplayWindowSeconds = g.i("pairing.replayWindowSeconds")
		cfg.Pairing.MTLSPort = g.i("pairing.mtlsPort")
		cfg.Pairing.ControlPort = g.i("pairing.controlPort")
		cfg.Pairing.MediaPort = g.i("pairing.mediaPort")
		cfg.Pairing.CertTTLHours = g.i("pairing.certTtlHours")
		cfg.Pairing.RenewBeforeHours = g.i("pairing.renewBeforeHours")
		cfg.Pairing.HeartbeatIntervalSeconds = g.i("pairing.heartbeatIntervalSeconds")
		cfg.Pairing.ParentBaseURL = g.s("pairing.parentBaseUrl")
	case "agent":
		digestEnabled := g.b("agent.digest.enabled")
		cfg.Agent.Digest.Enabled = &digestEnabled
		digestHour := g.i("agent.digest.localHour")
		cfg.Agent.Digest.LocalHour = &digestHour
		cfg.Agent.Digest.WindowHours = g.i("agent.digest.windowHours")
		cfg.Agent.Digest.RetentionDays = g.i("agent.digest.retentionDays")
		cfg.Agent.Digest.Language = strings.ToLower(strings.TrimSpace(g.s("agent.digest.language")))
		weeklyEnabled := g.b("agent.digest.weeklyEnabled")
		cfg.Agent.Digest.WeeklyEnabled = &weeklyEnabled
		cfg.Agent.Digest.Weekday = g.i("agent.digest.weekday")
		cfg.Agent.LLM.Mode = strings.ToLower(strings.TrimSpace(g.s("agent.llm.mode")))
		cfg.Agent.LLM.Endpoint = strings.TrimSpace(g.s("agent.llm.endpoint"))
		cfg.Agent.LLM.APIKey = g.s("agent.llm.apiKey")
		cfg.Agent.LLM.Model = strings.TrimSpace(g.s("agent.llm.model"))
		cfg.Agent.LLM.TimeoutSeconds = g.i("agent.llm.timeoutSeconds")
		cfg.Agent.LLM.MaxTokens = g.i("agent.llm.maxTokens")
		cfg.Agent.LLM.Sidecar.Port = g.i("agent.llm.sidecar.port")
		cfg.Agent.LLM.Sidecar.CtxSize = g.i("agent.llm.sidecar.ctxSize")
		cfg.Agent.LLM.Sidecar.Threads = g.i("agent.llm.sidecar.threads")
		cfg.Agent.LLM.Sidecar.BinaryPath = strings.TrimSpace(g.s("agent.llm.sidecar.binaryPath"))
		cfg.Agent.LLM.Sidecar.ModelPath = strings.TrimSpace(g.s("agent.llm.sidecar.modelPath"))
		allowDownloads := g.b("agent.allowDownloads")
		cfg.Agent.AllowDownloads = &allowDownloads
	case "security":
		cfg.Jwt.Secret = g.s("jwt.secret")
		cfg.AllowOrigin = g.s("allowOrigins")
		cfg.Tls.CertPath = g.s("tls.certPath")
		cfg.Tls.KeyPath = g.s("tls.keyPath")
		cfg.SecurityHeaders.ContentSecurityPolicy = g.s("securityHeaders.contentSecurityPolicy")
		cfg.RateLimit.Enabled = g.b("rateLimit.enabled")
		cfg.RateLimit.DefaultWindowSeconds = g.i("rateLimit.defaultWindowSeconds")
		applyTier(&cfg.RateLimit.DevOnly, g, "rateLimit.devOnly")
		applyTier(&cfg.RateLimit.AuthOnly, g, "rateLimit.authOnly")
		applyTier(&cfg.RateLimit.Public, g, "rateLimit.public")
	case "storage":
		cfg.FileStorage.Path = g.s("fileStorage.path")
		cfg.FileStorage.Cleanup.Enabled = g.b("fileStorage.cleanup.enabled")
		cfg.FileStorage.Cleanup.FrequencySeconds = g.i("fileStorage.cleanup.frequencySeconds")
		cfg.FileStorage.Cleanup.BatchSize = g.i("fileStorage.cleanup.batchSize")
		cfg.Cache.Provider = g.s("cache.provider")
		cfg.Cache.TTLSeconds = g.i("cache.ttlSeconds")
		cfg.Cache.KeyPrefix = g.s("cache.keyPrefix")
		cfg.Cache.Redis.Address = g.s("cache.redis.address")
		cfg.Cache.Redis.Password = g.s("cache.redis.password")
		cfg.Cache.Redis.DB = g.i("cache.redis.db")
		cfg.Cache.Redis.UseTLS = g.b("cache.redis.useTls")
		cfg.Cache.Redis.ConnectTimeoutMs = g.i("cache.redis.connectTimeoutMs")
		cfg.Cache.Redis.OperationTimeoutMs = g.i("cache.redis.operationTimeoutMs")
		cfg.Transaction.LockProvider = g.s("transaction.lockProvider")
	case "logging":
		cfg.Logging.Enabled = g.b("logging.enabled")
		cfg.Logging.Path = g.s("logging.path")
		cfg.Logging.MaxLineBytes = g.i("logging.maxLineBytes")
		cfg.Logging.MaxFileSizeMb = g.i("logging.maxFileSizeMb")
		cfg.Logging.Cleanup.Enabled = g.b("logging.cleanup.enabled")
		cfg.Logging.Cleanup.MaxRetentionDays = g.i("logging.cleanup.maxRetentionDays")
		cfg.Logging.Cleanup.FrequencyMinutes = g.i("logging.cleanup.frequencyMinutes")
		cfg.ApiLog.Cleanup.Enabled = g.b("apiLog.cleanup.enabled")
		cfg.ApiLog.Cleanup.MaxRetentionDays = g.i("apiLog.cleanup.maxRetentionDays")
		cfg.ApiLog.Cleanup.FrequencyMinutes = g.i("apiLog.cleanup.frequencyMinutes")
		cfg.Telemetry.Enabled = g.b("telemetry.enabled")
		cfg.Telemetry.Prometheus.Enabled = g.b("telemetry.prometheus.enabled")
		cfg.Telemetry.Prometheus.MetricsPath = g.s("telemetry.prometheus.metricsPath")
		cfg.Telemetry.Prometheus.ApiDurationThresholdMs = g.i64("telemetry.prometheus.apiDurationThresholdMs")
	default:
		return fmt.Errorf("unknown settings section %q", section)
	}
	return nil
}

func applyTier(t *config.RateLimitTierConfigModel, g getters, prefix string) {
	t.Enabled = g.b(prefix + ".enabled")
	t.Requests = g.i(prefix + ".requests")
	t.WindowSeconds = g.i(prefix + ".windowSeconds")
}

// getters is a small typed accessor over a nested map, coercing JSON/native numbers.
type getters struct{ data map[string]any }

func boundGetters(data map[string]any) getters { return getters{data: data} }

func (g getters) s(path string) string {
	v, ok := leafAny(g.data, path)
	if !ok {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func (g getters) b(path string) bool {
	v, ok := leafAny(g.data, path)
	if !ok {
		return false
	}
	b, _ := v.(bool)
	return b
}

func (g getters) i(path string) int { return int(g.i64(path)) }

func (g getters) i64(path string) int64 {
	v, ok := leafAny(g.data, path)
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int:
		return int64(n)
	case int64:
		return n
	}
	return 0
}
