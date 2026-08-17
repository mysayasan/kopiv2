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
		if strings.TrimSpace(g.s("sso.issuer")) == "" {
			return fmt.Errorf("issuer is required")
		}
		if strings.TrimSpace(g.s("sso.audience")) == "" {
			return fmt.Errorf("audience is required — relying apps are matched against it")
		}
		for _, key := range []string{"sso.sessionTtlSeconds", "sso.authCodeTtlSeconds", "sso.accessTokenTtlSeconds", "sso.policyCacheTtlSeconds"} {
			if g.i(key) < 0 {
				return fmt.Errorf("SSO TTLs cannot be negative")
			}
		}
		// An authorization code is redeemed by the relying app within seconds of being
		// issued. A long-lived one is a credential sitting in a redirect URL, and therefore
		// in browser history, proxy logs and any Referer along the way — so cap it rather
		// than trust the operator not to add a zero.
		if ttl := g.i("sso.authCodeTtlSeconds"); ttl > 600 {
			return fmt.Errorf("authorization-code TTL must be 600 seconds or less; it is redeemed immediately, and a long-lived code is a credential left sitting in a URL")
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
		// A zero maxAttempts taken literally locks a user out before their first attempt is
		// even evaluated. Refused rather than silently defaulted: an operator who typed 0
		// meant something, and it was not that.
		if g.b("loginSecurity.enabled") && g.i("loginSecurity.maxAttempts") < 2 {
			return fmt.Errorf("max attempts must be at least 2 when the lockout is enabled — 0 or 1 locks an account out on its first try")
		}
		if g.i("loginSecurity.windowSeconds") < 0 || g.i("loginSecurity.lockoutSeconds") < 0 || g.i("loginSecurity.failedDelayMs") < 0 {
			return fmt.Errorf("lockout values cannot be negative")
		}
		// Floored, not merely non-negative. A "policy" admitting a 4-character password is
		// worse than none: it reads as due diligence on the settings page while permitting
		// exactly what it appears to forbid.
		if n := g.i("passwordPolicy.minLength"); n < 8 {
			return fmt.Errorf("minimum password length must be at least 8")
		}
		switch strings.TrimSpace(g.s("mfa.policy")) {
		case config.MfaPolicyOff, config.MfaPolicyOptional, config.MfaPolicyRequired:
		default:
			return fmt.Errorf("MFA policy must be one of: %s, %s, %s", config.MfaPolicyOff, config.MfaPolicyOptional, config.MfaPolicyRequired)
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
		cfg.SSO.AuthCodeTTLSeconds = g.i("sso.authCodeTtlSeconds")
		cfg.SSO.AccessTokenTTLSeconds = g.i("sso.accessTokenTtlSeconds")
		cfg.SSO.InternalToken = g.s("sso.internalToken")
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
		// Pointer-valued Enabled flags: the pointer is what distinguishes "absent, so use
		// the safe default" from "explicitly set". Saving through the editor is always an
		// explicit choice, so take the address of a local rather than leaving nil.
		lockoutEnabled := g.b("loginSecurity.enabled")
		cfg.LoginSecurity.Enabled = &lockoutEnabled
		cfg.LoginSecurity.MaxAttempts = g.i("loginSecurity.maxAttempts")
		cfg.LoginSecurity.WindowSeconds = g.i("loginSecurity.windowSeconds")
		cfg.LoginSecurity.LockoutSeconds = g.i("loginSecurity.lockoutSeconds")
		cfg.LoginSecurity.LockoutMaxSeconds = g.i("loginSecurity.lockoutMaxSeconds")
		cfg.LoginSecurity.FailedDelayMs = g.i("loginSecurity.failedDelayMs")
		cfg.LoginSecurity.NotifyOnLockout = g.b("loginSecurity.notifyOnLockout")
		cfg.PasswordPolicy.MinLength = g.i("passwordPolicy.minLength")
		cfg.PasswordPolicy.RequireUpper = g.b("passwordPolicy.requireUpper")
		cfg.PasswordPolicy.RequireLower = g.b("passwordPolicy.requireLower")
		cfg.PasswordPolicy.RequireDigit = g.b("passwordPolicy.requireDigit")
		cfg.PasswordPolicy.RequireSymbol = g.b("passwordPolicy.requireSymbol")
		blockCommon := g.b("passwordPolicy.blockCommon")
		cfg.PasswordPolicy.BlockCommon = &blockCommon
		cfg.Mfa.Policy = strings.TrimSpace(g.s("mfa.policy"))
		cfg.Mfa.ApplyToDirectory = g.b("mfa.applyToDirectory")
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
