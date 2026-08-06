package apis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/myseliasan/entities"
	"github.com/mysayasan/kopiv2/apps/myseliasan/services"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
	"github.com/mysayasan/kopiv2/domain/utils/middlewares"
)

// maxAgentBody caps agent POST bodies (a chat request with history is a few KiB).
const maxAgentBody = 64 << 10

type agentApi struct {
	session   *middlewares.AccessSessionMidware
	digests   *services.DigestService
	chat      *services.ChatService
	llm       *services.LLMManager
	installer *services.LLMInstaller
	sidecar   *services.LLMSidecar
	audit     services.IAuditService
	digestCfg func() AgentDigestStatus
}

// AgentDigestStatus is the digest half of GET /status, resolved by the app wiring
// (which owns the config and the schedule state).
type AgentDigestStatus struct {
	Enabled     bool   `json:"enabled"`
	LocalHour   int    `json:"localHour"`
	WindowHours int    `json:"windowHours"`
	LastRunDate string `json:"lastRunDate,omitempty"`
	// The weekly cadence runs alongside the daily one on its own guard.
	WeeklyEnabled     bool   `json:"weeklyEnabled"`
	Weekday           int    `json:"weekday"`
	LastWeeklyRunDate string `json:"lastWeeklyRunDate,omitempty"`
}

// NewAgentApi mounts the fleet AI agent:
//
//	GET  /api/agent/status            — LLM/digest state (matrix-gated read)
//	GET  /api/agent/digests           — stored digests, newest-first (?limit=&offset=)
//	GET  /api/agent/digests/latest    — the most recent digest
//	POST /api/agent/digests/generate  — generate a digest now (?kind=weekly for the 7-day one)
//	POST /api/agent/chat              — ask-the-fleet; SSE stream (?stream=false for JSON)
//
// Superadmin-only management (self-gated, like the settings editor):
//
//	POST /api/agent/llm/test             — probe an endpoint before saving it
//	POST /api/agent/llm/install/binary   — download the pinned llama.cpp release
//	POST /api/agent/llm/install/model    — download the pinned default model
//	GET  /api/agent/llm/install/status   — install progress (UI polls)
//	POST /api/agent/llm/import           — install from an operator-picked file {kind, path}
//	POST /api/agent/llm/sidecar/restart  — nudge a failed sidecar to relaunch now
func NewAgentApi(
	router *mux.Router,
	auth middlewares.AuthMidware,
	session *middlewares.AccessSessionMidware,
	digests *services.DigestService,
	chat *services.ChatService,
	llmMgr *services.LLMManager,
	installer *services.LLMInstaller,
	sidecar *services.LLMSidecar,
	audit services.IAuditService,
	digestCfg func() AgentDigestStatus,
) {
	h := &agentApi{
		session: session, digests: digests, chat: chat,
		llm: llmMgr, installer: installer, sidecar: sidecar, audit: audit,
		digestCfg: digestCfg,
	}
	g := router.PathPrefix("/agent").Subrouter()
	g.Use(auth.Middleware)
	g.Use(session.Middleware)
	g.HandleFunc("/status", h.status).Methods("GET")
	g.HandleFunc("/digests", h.listDigests).Methods("GET")
	g.HandleFunc("/digests/latest", h.latestDigest).Methods("GET")
	g.HandleFunc("/digests/generate", h.generateDigest).Methods("POST")
	g.HandleFunc("/chat", h.chatHandler).Methods("POST")
	g.HandleFunc("/llm/test", h.requireSuper(h.testLLM)).Methods("POST")
	g.HandleFunc("/llm/install/binary", h.requireSuper(h.installBinary)).Methods("POST")
	g.HandleFunc("/llm/install/model", h.requireSuper(h.installModel)).Methods("POST")
	g.HandleFunc("/llm/install/status", h.requireSuper(h.installStatus)).Methods("GET")
	g.HandleFunc("/llm/import", h.requireSuper(h.importArtifact)).Methods("POST")
	g.HandleFunc("/llm/sidecar/restart", h.requireSuper(h.restartSidecar)).Methods("POST")
}

func (a *agentApi) requireSuper(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !a.session.IsSuperadmin(r) {
			controllers.SendError(w, controllers.ErrLimitedAccess, "superadmin access required")
			return
		}
		next(w, r)
	}
}

// --- status ------------------------------------------------------------------

func (a *agentApi) status(w http.ResponseWriter, r *http.Request) {
	out := map[string]any{
		"llm":    a.llm.Status(),
		"digest": a.digestCfg(),
		"downloads": map[string]any{
			"allowed":   a.installer.DownloadsAllowed(),
			"supported": a.installer.DownloadSupported(),
		},
	}
	controllers.SendResult(w, out, "succeed")
}

// --- digests -----------------------------------------------------------------

// digestDTO is a stored digest with FindingsJson decoded, so the SPA renders
// findings without re-parsing strings.
type digestDTO struct {
	*entities.AgentDigest
	Findings []services.Finding `json:"findings"`
}

func toDigestDTO(d *entities.AgentDigest) digestDTO {
	dto := digestDTO{AgentDigest: d}
	_ = json.Unmarshal([]byte(d.FindingsJson), &dto.Findings)
	dto.FindingsJson = "" // decoded copy replaces the raw payload
	return dto
}

func (a *agentApi) listDigests(w http.ResponseWriter, r *http.Request) {
	limit := queryUint(r, "limit", 20)
	offset := queryUint(r, "offset", 0)
	rows, total, err := a.digests.List(r.Context(), limit, offset)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	items := make([]digestDTO, 0, len(rows))
	for _, d := range rows {
		items = append(items, toDigestDTO(d))
	}
	controllers.SendResult(w, map[string]any{"items": items, "total": total}, "succeed")
}

func (a *agentApi) latestDigest(w http.ResponseWriter, r *http.Request) {
	d, err := a.digests.Latest(r.Context())
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	if d == nil {
		controllers.SendResult(w, nil, "succeed")
		return
	}
	controllers.SendResult(w, toDigestDTO(d), "succeed")
}

// extendWriteDeadline lifts the server's default 30s WriteTimeout for this one
// response. CPU inference is legitimately slower than that — a polished digest
// or a long chat completion takes minutes-class time on small hosts — and the
// alternative (raising the global timeout) would blunt the slowloris defense
// for every other endpoint. Best-effort: an error leaves the default in place.
func extendWriteDeadline(w http.ResponseWriter, d time.Duration) {
	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Now().Add(d))
}

// generateDigest runs a digest now. ?kind=weekly produces the 7-day management
// summary on demand; anything else is the default 24h operational one. "daily"
// is deliberately NOT accepted — that kind belongs to the scheduler, and letting
// a manual run claim it would muddy which digests were actually scheduled.
func (a *agentApi) generateDigest(w http.ResponseWriter, r *http.Request) {
	extendWriteDeadline(w, 3*time.Minute) // narrator is instant; the LLM polish is not
	kind := "manual"
	if strings.EqualFold(r.URL.Query().Get("kind"), "weekly") {
		kind = "weekly"
	}
	d, err := a.digests.Generate(r.Context(), kind, operatorUserId(r))
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	a.record(r, "agent.digest.generate", strconv.FormatInt(d.Id, 10), nil)
	controllers.SendResult(w, toDigestDTO(d), "succeed")
}

// --- chat --------------------------------------------------------------------

// chatHandler answers a grounded question. Default is a Server-Sent-Events
// stream (event: meta / delta / done / error) flushed per token batch, because
// CPU inference is slow and perceived latency is the UX; ?stream=false returns
// one JSON object for tests and curl.
func (a *agentApi) chatHandler(w http.ResponseWriter, r *http.Request) {
	var req services.ChatRequestBody
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxAgentBody))
	if err != nil {
		httpJSONError(w, http.StatusBadRequest, "bad_request", "request body too large")
		return
	}
	if err := json.Unmarshal(body, &req); err != nil {
		httpJSONError(w, http.StatusBadRequest, "bad_request", "malformed JSON body")
		return
	}

	// Pre-flight failures map to clean status codes BEFORE any stream starts.
	if err := a.chat.Validate(&req); err != nil {
		httpJSONError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if _, err := a.llm.Client(); err != nil {
		switch {
		case errors.Is(err, services.ErrLLMNotReady):
			httpJSONError(w, http.StatusServiceUnavailable, "llm_starting", "the language model is not ready yet")
		default:
			httpJSONError(w, http.StatusConflict, "llm_disabled", "no language model is enabled on this control plane")
		}
		return
	}

	a.record(r, "agent.chat", "", map[string]any{
		"question": truncate(req.Question, 200),
		"lang":     req.Lang,
	})

	// A CPU model may stream (or compute, in JSON mode) for well over the server's
	// 30s write timeout; the request ctx still cancels everything if the browser leaves.
	extendWriteDeadline(w, 10*time.Minute)

	if strings.EqualFold(r.URL.Query().Get("stream"), "false") {
		res, err := a.chat.Stream(r.Context(), req, nil)
		if err != nil {
			httpJSONError(w, http.StatusBadGateway, "llm_error", err.Error())
			return
		}
		controllers.SendResult(w, map[string]any{"answer": res.Content, "usage": res.Usage}, "succeed")
		return
	}

	fl, ok := w.(http.Flusher)
	if !ok {
		httpJSONError(w, http.StatusInternalServerError, "no_stream", "streaming unsupported by this connection")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	sse := func(event string, payload any) {
		data, err := json.Marshal(payload)
		if err != nil {
			return
		}
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
		fl.Flush()
	}
	sse("meta", map[string]any{"model": a.llm.ModelName(), "windowDays": req.WindowDays})

	res, err := a.chat.Stream(r.Context(), req, func(delta string) error {
		sse("delta", map[string]string{"text": delta})
		return nil
	})
	if err != nil {
		code := "llm_error"
		if errors.Is(err, r.Context().Err()) && r.Context().Err() != nil {
			return // the browser went away; nobody is listening
		}
		if strings.Contains(err.Error(), "context deadline exceeded") {
			code = "timeout"
		}
		sse("error", map[string]string{"code": code, "message": err.Error()})
		return
	}
	sse("done", map[string]any{"usage": res.Usage, "chars": len(res.Content)})
}

// --- LLM management (superadmin) --------------------------------------------

func (a *agentApi) testLLM(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Endpoint string `json:"endpoint"`
		APIKey   string `json:"apiKey"`
		Model    string `json:"model"`
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxAgentBody))
	if err == nil {
		_ = json.Unmarshal(body, &req)
	}
	a.record(r, "agent.llm.test", hostOf(req.Endpoint), nil)
	if err := a.llm.Test(r.Context(), req.Endpoint, req.APIKey, req.Model); err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, map[string]any{"ok": true}, "succeed")
}

func (a *agentApi) installBinary(w http.ResponseWriter, r *http.Request) {
	// context.Background(), NOT r.Context(): the download outlives this request
	// (the UI polls /install/status); it is bounded by the installer's own client
	// timeout instead.
	if err := a.installer.StartBinaryDownload(context.Background()); err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	a.record(r, "agent.llm.install.binary", "download", nil)
	controllers.SendResult(w, map[string]any{"started": true}, "succeed")
}

// installModel starts a pinned model download; body {tier:"default"|"large"}
// (empty = default). The large tier is the quality upgrade for hosts with RAM.
func (a *agentApi) installModel(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Tier string `json:"tier"`
	}
	if body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxAgentBody)); err == nil && len(body) > 0 {
		_ = json.Unmarshal(body, &req)
	}
	if err := a.installer.StartModelDownload(context.Background(), req.Tier); err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	a.record(r, "agent.llm.install.model", "download:"+defaultStrA(req.Tier, "default"), nil)
	controllers.SendResult(w, map[string]any{"started": true}, "succeed")
}

func defaultStrA(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}

func (a *agentApi) installStatus(w http.ResponseWriter, r *http.Request) {
	controllers.SendResult(w, a.installer.Status(), "succeed")
}

// importArtifact installs an operator-picked file (from the settings file
// picker): {kind: "binary"|"model", path}. The computed sha256 lands in the
// install log and the audit metadata — imports are unpinned by design (any
// build/model an air-gapped operator carries in), so provenance is recorded
// instead of enforced.
func (a *agentApi) importArtifact(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Kind string `json:"kind"`
		Path string `json:"path"`
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxAgentBody))
	if err != nil || json.Unmarshal(body, &req) != nil || strings.TrimSpace(req.Path) == "" {
		controllers.SendError(w, controllers.ErrBadRequest, "kind and path are required")
		return
	}
	var installed string
	switch req.Kind {
	case "binary":
		installed, err = a.installer.ImportBinaryArchive(r.Context(), req.Path)
	case "model":
		installed, err = a.installer.ImportModel(r.Context(), req.Path)
	default:
		controllers.SendError(w, controllers.ErrBadRequest, `kind must be "binary" or "model"`)
		return
	}
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	a.record(r, "agent.llm.import", req.Kind, map[string]any{"path": req.Path, "installed": installed})
	controllers.SendResult(w, map[string]any{"path": installed}, "succeed")
}

func (a *agentApi) restartSidecar(w http.ResponseWriter, r *http.Request) {
	if a.sidecar == nil {
		controllers.SendError(w, controllers.ErrBadRequest, "no sidecar is configured")
		return
	}
	// Pause+Resume kills a wedged child and nudges the supervisor immediately.
	a.sidecar.Pause()
	a.sidecar.Resume()
	a.record(r, "agent.llm.sidecar.restart", "", nil)
	controllers.SendResult(w, map[string]any{"ok": true}, "succeed")
}

// --- helpers -----------------------------------------------------------------

func (a *agentApi) record(r *http.Request, action, target string, metadata map[string]any) {
	if a.audit == nil {
		return
	}
	roleId, actor := operatorIdentity(r)
	a.audit.Record(r.Context(), services.AuditEntry{
		Action:     action,
		ActorId:    operatorUserId(r),
		ActorEmail: actor,
		ActorRole:  roleId,
		TargetType: "agent",
		TargetId:   target,
		Metadata:   metadata,
		ClientIp:   clientIP(r),
	})
}

// httpJSONError writes a plain {code,message} error with a specific HTTP status
// (the shared controllers helpers cover 400/500 but not 409/503).
func httpJSONError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"code": code, "message": message})
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func hostOf(endpoint string) string {
	e := strings.TrimSpace(endpoint)
	if idx := strings.Index(e, "://"); idx > 0 {
		e = e[idx+3:]
	}
	if idx := strings.IndexAny(e, "/?#"); idx >= 0 {
		e = e[:idx]
	}
	return e
}
