package services

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mysayasan/kopiv2/apps/myseliasan/entities"
	"github.com/mysayasan/kopiv2/infra/atrest"
	"github.com/mysayasan/kopiv2/infra/cache"
	"github.com/mysayasan/kopiv2/infra/config"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// Settings exposes a SAFE SUBSET of the app's config.json as an in-app, RBAC-gated
// editor. The scope deliberately excludes the blocks that would take the app offline if
// mis-set (db, server, bootstrap): those stay file-only.
//
// Persistence model (decided against a pure DB store because myseliasan's config is
// infra-level, read once by the shared host at boot — see settings_materialize.go):
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
var sectionOrder = []string{"localAuth", "sso", "pairing", "agent", "security", "storage", "logging"}

// sectionSecrets maps each section to the dotted (root-relative) leaf paths that are
// secret, so masking and keep-if-blank are data-driven.
var sectionSecrets = map[string][]string{
	"localAuth": {"localAuth.password"},
	"sso":       {"sso.clientSecret"},
	"agent":     {"agent.llm.apiKey"},
	"security":  {"jwt.secret"},
	"storage":   {"cache.redis.password"},
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
	if err := materializeConfig(s.cfgPath, flattenPatches(nil, data)); err != nil {
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
		return map[string]any{"agent": map[string]any{
			"digest": map[string]any{
				"enabled":       boolValue(s.cfg.Agent.Digest.Enabled, true),
				"localHour":     intValue(s.cfg.Agent.Digest.LocalHour, 7),
				"windowHours":   s.cfg.Agent.Digest.WindowHours,
				"retentionDays": s.cfg.Agent.Digest.RetentionDays,
				"language":      s.cfg.Agent.Digest.Language,
			},
			"llm": map[string]any{
				"mode":           s.cfg.Agent.LLM.Mode,
				"endpoint":       s.cfg.Agent.LLM.Endpoint,
				"apiKey":         s.cfg.Agent.LLM.APIKey,
				"model":          s.cfg.Agent.LLM.Model,
				"timeoutSeconds": s.cfg.Agent.LLM.TimeoutSeconds,
				"maxTokens":      s.cfg.Agent.LLM.MaxTokens,
				"sidecar": map[string]any{
					"port":       s.cfg.Agent.LLM.Sidecar.Port,
					"ctxSize":    s.cfg.Agent.LLM.Sidecar.CtxSize,
					"threads":    s.cfg.Agent.LLM.Sidecar.Threads,
					"binaryPath": s.cfg.Agent.LLM.Sidecar.BinaryPath,
					"modelPath":  s.cfg.Agent.LLM.Sidecar.ModelPath,
				},
			},
			"allowDownloads": boolValue(s.cfg.Agent.AllowDownloads, true),
		}}, nil
	case "security":
		return map[string]any{
			"jwt":         map[string]any{"secret": s.cfg.Jwt.Secret},
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

// flattenPatches turns a nested map into leaf configPatches. Scalars and non-map values
// become one patch each; nested maps recurse. prefix is the root path (nil at the top).
func flattenPatches(prefix []string, data map[string]any) []configPatch {
	var patches []configPatch
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		path := append(append([]string{}, prefix...), k)
		if child, ok := data[k].(map[string]any); ok {
			patches = append(patches, flattenPatches(path, child)...)
			continue
		}
		patches = append(patches, configPatch{path: path, value: data[k]})
	}
	return patches
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
	setPath(m, strings.Split(path, "."), value)
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
