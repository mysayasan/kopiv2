package services

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/mysayasan/kopiv2/apps/myseliasan/entities"
	"github.com/mysayasan/kopiv2/infra/atrest"
	"github.com/mysayasan/kopiv2/infra/cache"
	"github.com/mysayasan/kopiv2/infra/config"
	"github.com/mysayasan/kopiv2/infra/config/configfile"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
	"github.com/mysayasan/kopiv2/infra/mailer"
)

// Settings exposes a SAFE SUBSET of the app's config.json as an in-app, RBAC-gated
// editor. The scope deliberately excludes the blocks that would take the app offline if
// mis-set (db, server, bootstrap): those stay file-only.
//
// Persistence model (decided against a pure DB store because myseliasan's config is
// infra-level, read once by the shared host at boot — see infra/config/configfile):
//   - The authoritative CURRENT value lives in config.json, which the host re-reads on
//     restart; deps.Config mirrors it in memory. Get reads from deps.Config.
//   - Save validates, updates the in-memory config so the UI reflects the pending value,
//     writes the changed leaves back into config.json, and reports needsRestart — the
//     change only takes effect once the process relaunches.
//   - A one-time DB snapshot of the original config (settingsDefaultsKey) backs Reset, so
//     "restore defaults" still works after config.json has been overwritten.
//
// Secrets (passwords, jwt secret, client secret) are never returned; Get sends "" plus a
// "<field>Set" boolean, and Save keeps the current value when the incoming secret is blank.

const (
	// settingsDefaultsKey stores the encrypted JSON snapshot of the original editable
	// config, captured once on first run and never overwritten. Reset restores from it.
	settingsDefaultsKey = "settings.defaults"

	// secretKept is the sentinel Get returns for a secret that IS set. It is never the real
	// value; Save treats an empty (or sentinel) incoming secret as "leave unchanged".
	secretPlaceholder = ""
)

// SaveResult is returned from Save/Reset so the caller/UI knows a restart is pending.
type SaveResult struct {
	NeedsRestart bool `json:"needsRestart"`
}

// ISettingsService is the in-app config editor over the safe config.json subset.
type ISettingsService interface {
	// Sections lists the editable section ids in display order.
	Sections() []string
	// Get returns one section's current values (secrets masked). Unknown id -> error.
	Get(section string) (map[string]any, error)
	// GetAll returns every section keyed by id (secrets masked).
	GetAll() map[string]any
	// Save validates and persists one section, returning whether a restart is needed.
	Save(ctx context.Context, section string, body json.RawMessage) (SaveResult, error)
	// Reset restores one section to the captured first-run defaults.
	Reset(ctx context.Context, section string) (SaveResult, error)
	// TestCache attempts a live Redis connection with the supplied settings, so an operator
	// can verify them before saving. A blank password falls back to the stored one. Returns
	// nil on a successful ping.
	TestCache(ctx context.Context, body json.RawMessage) error
	// TestMail sends a real message through the relay in the request body (blank
	// password uses the stored one), so an operator can verify the mail path
	// before an incident depends on it. Returns nil on delivery.
	TestMail(ctx context.Context, body json.RawMessage) error
}

type settingsService struct {
	cfg     *config.AppConfigModel
	cfgPath string
	repo    dbsql.IGenericRepo[entities.ControlSetting]
	cipher  *atrest.Cipher
	logf    func(format string, args ...any)
	mu      sync.Mutex
}

// sectionOrder is the canonical display + iteration order.
var sectionOrder = []string{"localAuth", "sso", "pairing", "agent", "notification", "security", "storage", "logging"}

// sectionSecrets maps each section to the dotted (root-relative) leaf paths that are
// secret, so masking and keep-if-blank are data-driven.
var sectionSecrets = map[string][]string{
	"localAuth":    {"localAuth.password"},
	"sso":          {"sso.clientSecret"},
	"agent":        {"agent.llm.apiKey"},
	"security":     {"jwt.secret"},
	"storage":      {"cache.redis.password"},
	"notification": {"smtp.password"},
}

// NewSettingsService builds the editor over deps.Config + config.json and captures the
// first-run defaults snapshot (best-effort; a failure only disables Reset, logged via logf).
func NewSettingsService(cfg *config.AppConfigModel, cfgPath string, db dbsql.IDbCrud, cipher *atrest.Cipher, logf func(format string, args ...any)) ISettingsService {
	s := &settingsService{
		cfg:     cfg,
		cfgPath: cfgPath,
		repo:    dbsql.NewGenericRepo[entities.ControlSetting](db),
		cipher:  cipher,
		logf:    logf,
	}
	if err := s.ensureDefaults(context.Background()); err != nil && logf != nil {
		logf("capture settings defaults: %v", err)
	}
	return s
}

func (s *settingsService) Sections() []string {
	out := make([]string, len(sectionOrder))
	copy(out, sectionOrder)
	return out
}

func (s *settingsService) Get(section string) (map[string]any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := s.read(section)
	if err != nil {
		return nil, err
	}
	return maskCopy(data, sectionSecrets[section]), nil
}

func (s *settingsService) GetAll() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]any{}
	for _, id := range sectionOrder {
		data, err := s.read(id)
		if err != nil {
			continue
		}
		out[id] = maskCopy(data, sectionSecrets[id])
	}
	return out
}

func (s *settingsService) Save(ctx context.Context, section string, body json.RawMessage) (SaveResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	current, err := s.read(section)
	if err != nil {
		return SaveResult{}, err
	}
	var incoming map[string]any
	if len(body) > 0 {
		if err := json.Unmarshal(body, &incoming); err != nil {
			return SaveResult{}, fmt.Errorf("invalid request body: %w", err)
		}
	}
	// Project the request onto the known section shape: only recognized leaves survive
	// (dropping the UI's masked "<field>Set" helpers and anything else), and any field the
	// client omitted keeps its current value.
	merged := projectOntoShape(current, incoming)
	// A blank incoming secret means "keep the current value", so restore it over the merge.
	for _, path := range sectionSecrets[section] {
		if v, ok := leafString(incoming, path); !ok || strings.TrimSpace(v) == "" {
			if cur, ok := leafAny(current, path); ok {
				setLeaf(merged, path, cur)
			}
		}
	}
	return s.commit(ctx, section, merged)
}

func (s *settingsService) Reset(ctx context.Context, section string) (SaveResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	defaults, err := s.loadDefaults(ctx)
	if err != nil {
		return SaveResult{}, err
	}
	raw, ok := defaults[section].(map[string]any)
	if !ok {
		return SaveResult{}, fmt.Errorf("no defaults captured for %q", section)
	}
	// Project onto the current shape so a snapshot from an older schema still resets cleanly.
	current, err := s.read(section)
	if err != nil {
		return SaveResult{}, err
	}
	return s.commit(ctx, section, projectOntoShape(current, raw))
}

// TestCache pings Redis with the given settings. Blank address/password fall back to the
// currently stored values, so an operator can test an existing config, or a new one they’re
// about to save, without persisting first.
// TestMail sends a real message through the relay described by the request body
// (blank password uses the stored one) so an operator can verify the mail path
// BEFORE relying on it. It mirrors TestCache, and for the same reason: the moment
// an alerting path is discovered to be broken must not be the incident it was
// supposed to report.
//
// It deliberately sends through the SAME infra/mailer the delivery channel uses,
// rather than merely opening a socket to the host. A relay that accepts a
// connection and then refuses the sender, the credential, or the recipient is the
// common failure, and a connectivity probe would call all three a success.
func (s *settingsService) TestMail(ctx context.Context, body json.RawMessage) error {
	var req struct {
		Host        string `json:"host"`
		Port        int    `json:"port"`
		From        string `json:"from"`
		Username    string `json:"username"`
		Password    string `json:"password"`
		UseStartTls bool   `json:"useStartTls"`
		To          string `json:"to"`
	}
	if len(body) > 0 {
		if err := json.Unmarshal(body, &req); err != nil {
			return fmt.Errorf("invalid request body: %w", err)
		}
	}

	s.mu.Lock()
	host := strings.TrimSpace(req.Host)
	if host == "" {
		host = strings.TrimSpace(s.cfg.Smtp.Host)
	}
	port := req.Port
	if port <= 0 {
		port = s.cfg.Smtp.Port
	}
	from := strings.TrimSpace(req.From)
	if from == "" {
		from = strings.TrimSpace(s.cfg.Smtp.From)
	}
	username := strings.TrimSpace(req.Username)
	if username == "" {
		username = strings.TrimSpace(s.cfg.Smtp.Username)
	}
	password := req.Password
	if strings.TrimSpace(password) == "" {
		password = s.cfg.Smtp.Password
	}
	to := splitList(req.To)
	if len(to) == 0 {
		to = splitList(s.cfg.Notification.Email.To)
	}
	prefix := strings.TrimSpace(s.cfg.Notification.Email.SubjectPrefix)
	s.mu.Unlock()

	if host == "" {
		return fmt.Errorf("an SMTP relay host is required")
	}
	if len(to) == 0 {
		return fmt.Errorf("at least one recipient is required")
	}
	if username != "" && !req.UseStartTls {
		return fmt.Errorf("STARTTLS must be enabled when an SMTP username is set - credentials are never sent over a cleartext link")
	}
	if port <= 0 {
		port = 587
	}

	subject := "Test notification"
	if prefix != "" {
		subject = prefix + " " + subject
	}
	m := mailer.New(mailer.Config{
		Enabled: true, Host: host, Port: port, From: from,
		Username: username, Password: password, UseStartTls: req.UseStartTls,
	})
	err := m.SendMessage(mailer.Message{
		To:      to,
		Subject: subject,
		Body:    "This is a test message from the myseliasan control plane. If you received it, fleet notifications can reach you by email.",
		Headers: map[string]string{"X-Kopiv2-Category": "system", "X-Kopiv2-Severity": "info"},
	})
	// A partial rejection means the relay works and some address does not. Report
	// that rather than a bare success, or the operator "verifies" a configuration
	// half of which silently delivers nothing.
	if re, ok := err.(*mailer.RecipientError); ok && re.Delivered() {
		return fmt.Errorf("delivered, but the relay rejected: %s", strings.Join(re.Addresses(), ", "))
	}
	return err
}

func (s *settingsService) TestCache(ctx context.Context, body json.RawMessage) error {
	var req struct {
		Address            string `json:"address"`
		Password           string `json:"password"`
		DB                 int    `json:"db"`
		UseTLS             bool   `json:"useTls"`
		ConnectTimeoutMs   int    `json:"connectTimeoutMs"`
		OperationTimeoutMs int    `json:"operationTimeoutMs"`
	}
	if len(body) > 0 {
		if err := json.Unmarshal(body, &req); err != nil {
			return fmt.Errorf("invalid request body: %w", err)
		}
	}

	s.mu.Lock()
	address := strings.TrimSpace(req.Address)
	if address == "" {
		address = s.cfg.Cache.Redis.Address
	}
	password := req.Password
	if strings.TrimSpace(password) == "" {
		password = s.cfg.Cache.Redis.Password
	}
	s.mu.Unlock()

	if strings.TrimSpace(address) == "" {
		return fmt.Errorf("Redis address is required")
	}
	connect := time.Duration(req.ConnectTimeoutMs) * time.Millisecond
	if connect <= 0 {
		connect = 2 * time.Second
	}
	op := time.Duration(req.OperationTimeoutMs) * time.Millisecond
	if op <= 0 {
		op = 2 * time.Second
	}

	store := cache.NewRedisStore(cache.RedisConfig{
		Address:          address,
		Password:         password,
		DB:               req.DB,
		UseTLS:           req.UseTLS,
		ConnectTimeout:   connect,
		OperationTimeout: op,
	})
	defer store.Close()

	pingCtx, cancel := context.WithTimeout(ctx, connect+op+time.Second)
	defer cancel()
	if err := store.Ping(pingCtx); err != nil {
		return fmt.Errorf("connection failed: %w", err)
	}
	return nil
}

// commit validates the merged section values, applies them to the in-memory config, and
// materializes the changed leaves into config.json. It always reports needsRestart because
// every editable block is read by the host only at boot.
func (s *settingsService) commit(ctx context.Context, section string, data map[string]any) (SaveResult, error) {
	if err := validateSection(section, data); err != nil {
		return SaveResult{}, err
	}
	if err := applyToConfig(s.cfg, section, data); err != nil {
		return SaveResult{}, err
	}
	if err := configfile.Materialize(s.cfgPath, configfile.Flatten(nil, data)); err != nil {
		return SaveResult{}, fmt.Errorf("write config: %w", err)
	}
	return SaveResult{NeedsRestart: true}, nil
}

// read builds the root-relative, unmasked nested map for one section from the live config.
func (s *settingsService) read(section string) (map[string]any, error) {
	switch section {
	case "localAuth":
		return map[string]any{"localAuth": map[string]any{
			"enabled":  s.cfg.LocalAuth.Enabled,
			"username": s.cfg.LocalAuth.Username,
			"password": s.cfg.LocalAuth.Password,
		}}, nil
	case "sso":
		return map[string]any{"sso": map[string]any{
			"issuer":                s.cfg.SSO.Issuer,
			"audience":              s.cfg.SSO.Audience,
			"sessionTtlSeconds":     s.cfg.SSO.SessionTTLSeconds,
			"policyCacheTtlSeconds": s.cfg.SSO.PolicyCacheTTLSeconds,
			"providerBaseUrl":       s.cfg.SSO.ProviderBaseURL,
			"caCertPath":            s.cfg.SSO.CACertPath,
			"clientId":              s.cfg.SSO.ClientID,
			"clientSecret":          s.cfg.SSO.ClientSecret,
			"redirectBaseUrl":       s.cfg.SSO.RedirectBaseURL,
			"redirectPath":          s.cfg.SSO.RedirectPath,
		}}, nil
	case "pairing":
		return map[string]any{"pairing": map[string]any{
			"enabled":                  boolValue(s.cfg.Pairing.Enabled, true),
			"multicastAddr":            s.cfg.Pairing.MulticastAddr,
			"replayWindowSeconds":      s.cfg.Pairing.ReplayWindowSeconds,
			"mtlsPort":                 s.cfg.Pairing.MTLSPort,
			"controlPort":              s.cfg.Pairing.ControlPort,
			"mediaPort":                s.cfg.Pairing.MediaPort,
			"certTtlHours":             s.cfg.Pairing.CertTTLHours,
			"renewBeforeHours":         s.cfg.Pairing.RenewBeforeHours,
			"heartbeatIntervalSeconds": s.cfg.Pairing.HeartbeatIntervalSeconds,
			"parentBaseUrl":            s.cfg.Pairing.ParentBaseURL,
		}}, nil
	case "agent":
		// Zero means "use the built-in default" everywhere in this block, so the
		// editor shows the EFFECTIVE value rather than a bare 0 — an operator
		// reads "0" as disabled/unbounded, which is the opposite of the truth
		// (0 look-back is really 24h, 0 timeout is really 60s). Saving then
		// writes the value explicitly, which is also the clearer config.json.
		// Threads is the one honest 0: llama-server's own "pick for me".
		return map[string]any{"agent": map[string]any{
			"digest": map[string]any{
				"enabled":       boolValue(s.cfg.Agent.Digest.Enabled, true),
				"localHour":     intValue(s.cfg.Agent.Digest.LocalHour, 7),
				"windowHours":   orDefault(s.cfg.Agent.Digest.WindowHours, 24),
				"retentionDays": orDefault(s.cfg.Agent.Digest.RetentionDays, 180),
				"language":      defaultStr(s.cfg.Agent.Digest.Language, "en"),
				"weeklyEnabled": boolValue(s.cfg.Agent.Digest.WeeklyEnabled, false),
				"weekday":       s.cfg.Agent.Digest.Weekday,
			},
			"llm": map[string]any{
				"mode":           defaultStr(s.cfg.Agent.LLM.Mode, "off"),
				"endpoint":       s.cfg.Agent.LLM.Endpoint,
				"apiKey":         s.cfg.Agent.LLM.APIKey,
				"model":          s.cfg.Agent.LLM.Model,
				"timeoutSeconds": orDefault(s.cfg.Agent.LLM.TimeoutSeconds, 60),
				"maxTokens":      orDefault(s.cfg.Agent.LLM.MaxTokens, 768),
				"sidecar": map[string]any{
					"port":       orDefault(s.cfg.Agent.LLM.Sidecar.Port, 49540),
					"ctxSize":    orDefault(s.cfg.Agent.LLM.Sidecar.CtxSize, 8192),
					"threads":    s.cfg.Agent.LLM.Sidecar.Threads,
					"binaryPath": s.cfg.Agent.LLM.Sidecar.BinaryPath,
					"modelPath":  s.cfg.Agent.LLM.Sidecar.ModelPath,
				},
			},
			"allowDownloads": boolValue(s.cfg.Agent.AllowDownloads, true),
		}}, nil
	case "notification":
		// The relay and the recipients are edited together because neither is any
		// use alone, even though they live in different config blocks: `smtp` is
		// shared with the rest of the suite, `notification.email` is this app's
		// routing. Splitting them across two screens would let an operator save a
		// half-configured mail path and see no error until an alert failed to
		// arrive — which is the one moment nobody is watching a settings screen.
		return map[string]any{
			"smtp": map[string]any{
				"enabled":     s.cfg.Smtp.Enabled,
				"host":        s.cfg.Smtp.Host,
				"port":        orDefault(s.cfg.Smtp.Port, 587),
				"from":        s.cfg.Smtp.From,
				"username":    s.cfg.Smtp.Username,
				"password":    s.cfg.Smtp.Password,
				"useStartTls": s.cfg.Smtp.UseStartTls,
			},
			"notification": map[string]any{
				"email": map[string]any{
					"enabled":       s.cfg.Notification.Email.Enabled,
					"to":            s.cfg.Notification.Email.To,
					"subjectPrefix": s.cfg.Notification.Email.SubjectPrefix,
					"minSeverity":   defaultStr(s.cfg.Notification.Email.MinSeverity, "warning"),
					"categories":    s.cfg.Notification.Email.Categories,
				},
			},
		}, nil
	case "security":
		return map[string]any{
			"jwt":          map[string]any{"secret": s.cfg.Jwt.Secret},
			"allowOrigins": s.cfg.AllowOrigin,
			"tls": map[string]any{
				"certPath": s.cfg.Tls.CertPath,
				"keyPath":  s.cfg.Tls.KeyPath,
			},
			"securityHeaders": map[string]any{
				"contentSecurityPolicy": s.cfg.SecurityHeaders.ContentSecurityPolicy,
			},
			"rateLimit": map[string]any{
				"enabled":              s.cfg.RateLimit.Enabled,
				"defaultWindowSeconds": s.cfg.RateLimit.DefaultWindowSeconds,
				"devOnly":              tierMap(s.cfg.RateLimit.DevOnly),
				"authOnly":             tierMap(s.cfg.RateLimit.AuthOnly),
				"public":               tierMap(s.cfg.RateLimit.Public),
			},
		}, nil
	case "storage":
		return map[string]any{
			"fileStorage": map[string]any{
				"path": s.cfg.FileStorage.Path,
				"cleanup": map[string]any{
					"enabled":          s.cfg.FileStorage.Cleanup.Enabled,
					"frequencySeconds": s.cfg.FileStorage.Cleanup.FrequencySeconds,
					"batchSize":        s.cfg.FileStorage.Cleanup.BatchSize,
				},
			},
			"cache": map[string]any{
				"provider":   s.cfg.Cache.Provider,
				"ttlSeconds": s.cfg.Cache.TTLSeconds,
				"keyPrefix":  s.cfg.Cache.KeyPrefix,
				"redis": map[string]any{
					"address":            s.cfg.Cache.Redis.Address,
					"password":           s.cfg.Cache.Redis.Password,
					"db":                 s.cfg.Cache.Redis.DB,
					"useTls":             s.cfg.Cache.Redis.UseTLS,
					"connectTimeoutMs":   s.cfg.Cache.Redis.ConnectTimeoutMs,
					"operationTimeoutMs": s.cfg.Cache.Redis.OperationTimeoutMs,
				},
			},
			// Beside the cache because the two decide different halves of the same
			// question: the cache decides whether SESSIONS are shared, the lock decides
			// whether the scheduled work (rollups, purges, digests, heartbeat) runs once
			// for the deployment or once per instance. Offering only the cache would let an
			// operator reach a half-clustered install that signs users in correctly while
			// quietly duplicating everything in the background.
			"transaction": map[string]any{
				"lockProvider": s.cfg.Transaction.LockProvider,
			},
		}, nil
	case "logging":
		return map[string]any{
			"logging": map[string]any{
				"enabled":      s.cfg.Logging.Enabled,
				"path":         s.cfg.Logging.Path,
				"maxLineBytes": s.cfg.Logging.MaxLineBytes,
				// 0 = uncapped. Exposed because the allow-list here is field-level, so a
				// config knob absent from this map is unreachable from the only in-app
				// config editor in the suite.
				"maxFileSizeMb": s.cfg.Logging.MaxFileSizeMb,
				"cleanup": map[string]any{
					"enabled":          s.cfg.Logging.Cleanup.Enabled,
					"maxRetentionDays": s.cfg.Logging.Cleanup.MaxRetentionDays,
					"frequencyMinutes": s.cfg.Logging.Cleanup.FrequencyMinutes,
				},
			},
			"apiLog": map[string]any{
				"cleanup": map[string]any{
					"enabled":          s.cfg.ApiLog.Cleanup.Enabled,
					"maxRetentionDays": s.cfg.ApiLog.Cleanup.MaxRetentionDays,
					"frequencyMinutes": s.cfg.ApiLog.Cleanup.FrequencyMinutes,
				},
			},
			"telemetry": map[string]any{
				"enabled": s.cfg.Telemetry.Enabled,
				"prometheus": map[string]any{
					"enabled":                s.cfg.Telemetry.Prometheus.Enabled,
					"metricsPath":            s.cfg.Telemetry.Prometheus.MetricsPath,
					"apiDurationThresholdMs": s.cfg.Telemetry.Prometheus.ApiDurationThresholdMs,
				},
			},
		}, nil
	}
	return nil, fmt.Errorf("unknown settings section %q", section)
}

func tierMap(t config.RateLimitTierConfigModel) map[string]any {
	return map[string]any{
		"enabled":       t.Enabled,
		"requests":      t.Requests,
		"windowSeconds": t.WindowSeconds,
	}
}

// ensureDefaults captures the first-run editable config once. Never overwrites an existing
// snapshot, so restarts (which reload the possibly-edited config) can't clobber it.
func (s *settingsService) ensureDefaults(ctx context.Context) error {
	row, err := s.repo.GetByUnique(ctx, "", "key", settingsDefaultsKey)
	if err != nil && !isNoResultFoundErr(err) {
		return err
	}
	if row != nil {
		return nil // already captured
	}
	snapshot := map[string]any{}
	for _, id := range sectionOrder {
		data, rerr := s.read(id)
		if rerr != nil {
			return rerr
		}
		snapshot[id] = data
	}
	blob, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	stored, err := encodeSecret(s.cipher, string(blob))
	if err != nil {
		return err
	}
	now := time.Now().Unix()
	_, err = s.repo.Create(ctx, "", entities.ControlSetting{Key: settingsDefaultsKey, Value: stored, CreatedAt: now, UpdatedAt: now})
	return err
}

func (s *settingsService) loadDefaults(ctx context.Context) (map[string]any, error) {
	row, err := s.repo.GetByUnique(ctx, "", "key", settingsDefaultsKey)
	if err != nil {
		if isNoResultFoundErr(err) {
			return nil, fmt.Errorf("no defaults snapshot available")
		}
		return nil, err
	}
	if row == nil {
		return nil, fmt.Errorf("no defaults snapshot available")
	}
	var snapshot map[string]any
	if err := json.Unmarshal([]byte(decodeSecret(s.cipher, row.Value)), &snapshot); err != nil {
		return nil, fmt.Errorf("decode defaults snapshot: %w", err)
	}
	return snapshot, nil
}

// --- nested-map helpers -----------------------------------------------------

// projectOntoShape rebuilds a value that has EXACTLY shape's structure: every leaf comes
// from data when present (and type-compatible), otherwise from shape. Keys in data that are
// not in shape are dropped — this is what strips the UI's "<field>Set" helpers and rejects
// any stray field, so materialize can never write an unrecognized key into config.json.
func projectOntoShape(shape, data map[string]any) map[string]any {
	out := make(map[string]any, len(shape))
	for k, sv := range shape {
		if sMap, ok := sv.(map[string]any); ok {
			dMap, _ := data[k].(map[string]any)
			if dMap == nil {
				dMap = map[string]any{}
			}
			out[k] = projectOntoShape(sMap, dMap)
			continue
		}
		if dv, ok := data[k]; ok {
			out[k] = dv
		} else {
			out[k] = sv
		}
	}
	return out
}

func deepCopyMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		if vm, ok := v.(map[string]any); ok {
			out[k] = deepCopyMap(vm)
		} else {
			out[k] = v
		}
	}
	return out
}

// maskCopy returns a deep copy of data with each secret leaf blanked and a sibling
// "<leaf>Set" boolean added so the UI can show whether a value exists.
func maskCopy(data map[string]any, secrets []string) map[string]any {
	out := deepCopyMap(data)
	for _, path := range secrets {
		cur, has := leafAny(out, path)
		set := has && fmt.Sprintf("%v", cur) != ""
		setLeaf(out, path, secretPlaceholder)
		parts := strings.Split(path, ".")
		parts[len(parts)-1] = parts[len(parts)-1] + "Set"
		setLeaf(out, strings.Join(parts, "."), set)
	}
	return out
}

func leafAny(m map[string]any, path string) (any, bool) {
	parts := strings.Split(path, ".")
	cur := m
	for i, p := range parts {
		if i == len(parts)-1 {
			v, ok := cur[p]
			return v, ok
		}
		next, ok := cur[p].(map[string]any)
		if !ok {
			return nil, false
		}
		cur = next
	}
	return nil, false
}

func leafString(m map[string]any, path string) (string, bool) {
	v, ok := leafAny(m, path)
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

func setLeaf(m map[string]any, path string, value any) {
	configfile.SetPath(m, strings.Split(path, "."), value)
}

func boolValue(p *bool, def bool) bool {
	if p == nil {
		return def
	}
	return *p
}

func intValue(p *int, def int) int {
	if p == nil {
		return def
	}
	return *p
}

// orDefault substitutes the effective default for a zero-valued int, so the
// settings editor never shows a bare "0" for a knob the code treats as "unset".
func orDefault(v, def int) int {
	if v <= 0 {
		return def
	}
	return v
}
