package services

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"time"

	"github.com/mysayasan/kopiv2/infra/config"
	"github.com/mysayasan/kopiv2/infra/llm"
)

// LLMManager is the one façade the digest and chat layers see. It owns the
// "which client, if any" decision so those layers stay mode-agnostic:
//
//	mode "off"      → Enabled() false, Client() errors
//	mode "external" → client for the operator's endpoint (any OpenAI-compatible server)
//	mode "sidecar"  → client for the supervised llama-server, but only while READY
//
// Nothing here retries or blocks beyond the configured timeout — the callers
// degrade deterministically when Client() errors or a completion fails.
type LLMManager struct {
	cfg     config.AgentLLMConfigModel
	sidecar *LLMSidecar
}

// AgentLLMStatus is the UI-facing summary of the language layer.
type AgentLLMStatus struct {
	Mode         string `json:"mode"`
	Ready        bool   `json:"ready"`
	SidecarState string `json:"sidecarState,omitempty"`
	LastError    string `json:"lastError,omitempty"`
	Model        string `json:"model,omitempty"`
	// Endpoint is host-only for external mode (never leaks credentials/paths);
	// empty for sidecar (loopback is an implementation detail).
	Endpoint string `json:"endpoint,omitempty"`
}

// ErrLLMDisabled distinguishes "the operator turned this off" from a fault.
var ErrLLMDisabled = errors.New("llm is disabled")

// ErrLLMNotReady means the sidecar exists but cannot serve yet (loading/failed).
var ErrLLMNotReady = errors.New("llm is not ready")

// NewLLMManager builds the façade. sidecar may be nil when mode != sidecar.
func NewLLMManager(cfg config.AgentLLMConfigModel, sidecar *LLMSidecar) *LLMManager {
	return &LLMManager{cfg: cfg, sidecar: sidecar}
}

// Mode returns the normalized configured mode ("off" when unset).
func (m *LLMManager) Mode() string {
	mode := strings.ToLower(strings.TrimSpace(m.cfg.Mode))
	if mode == "" {
		return "off"
	}
	return mode
}

// Enabled reports whether a completion could be attempted right now.
func (m *LLMManager) Enabled() bool {
	switch m.Mode() {
	case "external":
		return strings.TrimSpace(m.cfg.Endpoint) != ""
	case "sidecar":
		if m.sidecar == nil {
			return false
		}
		state, _ := m.sidecar.Status()
		return state == StateReady
	}
	return false
}

// Client returns a ready-to-use client for the active mode, or a typed error
// (ErrLLMDisabled / ErrLLMNotReady) the API layer maps to 409/503.
func (m *LLMManager) Client() (*llm.Client, error) {
	timeout := time.Duration(m.cfg.TimeoutSeconds) * time.Second
	switch m.Mode() {
	case "external":
		endpoint := strings.TrimSpace(m.cfg.Endpoint)
		if endpoint == "" {
			return nil, ErrLLMDisabled
		}
		return llm.New(endpoint, m.cfg.APIKey, m.cfg.Model, timeout), nil
	case "sidecar":
		if m.sidecar == nil {
			return nil, ErrLLMDisabled
		}
		state, _ := m.sidecar.Status()
		if state != StateReady {
			return nil, ErrLLMNotReady
		}
		return llm.New(m.sidecar.Endpoint(), "", m.ModelName(), timeout), nil
	}
	return nil, ErrLLMDisabled
}

// MaxTokens is the configured completion cap (default 768).
func (m *LLMManager) MaxTokens() int {
	if m.cfg.MaxTokens > 0 {
		return m.cfg.MaxTokens
	}
	return 768
}

// ModelName is the display/request model name: the configured name, else the
// sidecar model file's base name (llama-server ignores the request model field,
// but logs and digests record what actually answered).
func (m *LLMManager) ModelName() string {
	if name := strings.TrimSpace(m.cfg.Model); name != "" {
		return name
	}
	if m.Mode() == "sidecar" && m.sidecar != nil {
		if _, model := m.sidecar.ResolvedPaths(); model != "" {
			return strings.TrimSuffix(filepath.Base(model), filepath.Ext(model))
		}
	}
	return ""
}

// Status summarizes the language layer for /api/agent/status.
func (m *LLMManager) Status() AgentLLMStatus {
	out := AgentLLMStatus{Mode: m.Mode(), Model: m.ModelName()}
	switch out.Mode {
	case "external":
		out.Ready = m.Enabled()
		out.Endpoint = hostOnly(m.cfg.Endpoint)
	case "sidecar":
		if m.sidecar != nil {
			state, lastErr := m.sidecar.Status()
			out.SidecarState = string(state)
			out.LastError = lastErr
			out.Ready = state == StateReady
		} else {
			out.SidecarState = string(StateOff)
		}
	}
	return out
}

// Test probes the SUBMITTED endpoint values (not the saved config), so an
// operator can verify before hitting Save — same contract as the cache test.
func (m *LLMManager) Test(ctx context.Context, endpoint, apiKey, model string) error {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		// Blank endpoint tests the ACTIVE configuration instead (covers sidecar).
		client, err := m.Client()
		if err != nil {
			return err
		}
		return client.Probe(ctx)
	}
	if apiKey == "" {
		// Blank submitted key falls back to the stored one, mirroring the
		// settings keep-if-blank contract so a saved key can be re-tested.
		apiKey = m.cfg.APIKey
	}
	return llm.New(endpoint, apiKey, model, 10*time.Second).Probe(ctx)
}

// hostOnly reduces a URL to scheme://host for display.
func hostOnly(rawURL string) string {
	raw := strings.TrimSpace(rawURL)
	if raw == "" {
		return ""
	}
	rest := raw
	scheme := ""
	if idx := strings.Index(raw, "://"); idx > 0 {
		scheme = raw[:idx+3]
		rest = raw[idx+3:]
	}
	if idx := strings.IndexAny(rest, "/?#"); idx >= 0 {
		rest = rest[:idx]
	}
	return scheme + rest
}
