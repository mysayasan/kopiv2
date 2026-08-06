package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mysayasan/kopiv2/domain/notification"
	"github.com/mysayasan/kopiv2/infra/llm"
	"github.com/mysayasan/kopiv2/infra/telemetry"
)

// "Ask the fleet" — the chat side of the agent.
//
// The design is route → fetch → summarize, deliberately NOT an agentic tool
// loop. The models this must work with are 1-2B parameter CPU models, and those
// cannot reliably drive multi-step tool calling; what they CAN do is answer a
// question over a compact document. So the server assembles one grounding
// bundle — fleet status, windowed stats, per-source anomalies, the latest
// digest's findings, the recent high-severity events — and the model gets
// exactly one completion over it, under a system prompt that forbids answering
// from anything else.
//
// Grounding is CENTRAL-ONLY this release: everything comes from the control
// plane's own tables. Fanning out to nodes over the control tunnel could stall
// a reply behind a 30s-per-node timeout; per-node drill-down is a follow-up.

// ChatRequestBody is the POST /api/agent/chat payload.
type ChatRequestBody struct {
	// Question is required, ≤ chatMaxQuestionChars.
	Question string `json:"question"`
	// History carries the prior turns for conversational context. Only the last
	// chatMaxHistoryTurns are used, each truncated — history is context, not data.
	History []llm.Message `json:"history,omitempty"`
	// Lang is the reply language (en|ms|zh|ar); defaults to en.
	Lang string `json:"lang,omitempty"`
	// WindowDays bounds the grounding window (default 7, max 31).
	WindowDays int `json:"windowDays,omitempty"`
	// TZOffsetMin is the viewer's timezone offset in minutes (JS convention,
	// positive east of UTC), so buckets and "today" align with their clock.
	TZOffsetMin int `json:"tzOffset,omitempty"`
}

const (
	chatMaxQuestionChars = 2000
	chatMaxHistoryTurns  = 4
	chatMaxHistoryChars  = 400
	chatDefaultDays      = 7
	chatMaxDays          = 31
	// chatMaxBundleBytes caps the serialized grounding document. Bytes are the
	// enforceable proxy for tokens (~4 bytes/token for JSON): 8 KiB ≈ 2.3k
	// tokens, which with prompt+history+completion fits an 8k context with room.
	chatMaxBundleBytes = 8 << 10
	chatFleetCap       = 50
	chatRecentCap      = 15
	chatAnomalyCap     = 10
	chatTitleCap       = 80
)

// GroundingBundle is the single document the model may answer from.
type GroundingBundle struct {
	GeneratedAt int64        `json:"generatedAt"`
	Window      GroundWindow `json:"window"`
	// Fleet is every adopted node's live state (capped; count noted when trimmed).
	Fleet        []GroundNode    `json:"fleet"`
	FleetTotal   int             `json:"fleetTotal"`
	Stats        *GroundStats    `json:"stats,omitempty"`
	Anomalies    []GroundAnomaly `json:"anomalies,omitempty"`
	LatestDigest []Finding       `json:"latestDigestFindings,omitempty"`
	// Recent is the newest warning/critical events, ids included so the model
	// can cite them.
	Recent []GroundNotif `json:"recentEvents,omitempty"`
	// Truncated lists sections that were dropped/trimmed to fit the size cap, so
	// the model can say "I only see part of the data" instead of guessing.
	Truncated []string `json:"truncated,omitempty"`
}

type GroundWindow struct {
	From int64 `json:"from"`
	To   int64 `json:"to"`
	Days int   `json:"days"`
}

type GroundNode struct {
	NodeId       string `json:"nodeId"`
	Name         string `json:"name,omitempty"`
	Kind         string `json:"kind,omitempty"`
	Status       string `json:"status"`
	Connected    bool   `json:"connected"`
	CertDaysLeft *int   `json:"certDaysLeft,omitempty"`
	LastSeenAt   int64  `json:"lastSeenAt,omitempty"`
}

type GroundStats struct {
	Total      int64                    `json:"total"`
	PrevTotal  int64                    `json:"prevTotal"`
	Critical   int64                    `json:"critical"`
	Warning    int64                    `json:"warning"`
	BySource   []notification.CountItem `json:"bySource,omitempty"`
	ByCategory []notification.CountItem `json:"byCategory,omitempty"`
}

type GroundAnomaly struct {
	Source    string `json:"source"`
	Count     int64  `json:"count"`
	PrevCount int64  `json:"prevCount"`
	Direction string `json:"direction"`
}

type GroundNotif struct {
	Id        int64  `json:"id"`
	CreatedAt int64  `json:"createdAt"`
	Severity  string `json:"severity"`
	Source    string `json:"source"`
	Title     string `json:"title"`
}

// ChatService runs grounded completions.
type ChatService struct {
	notif   digestNotifSource
	fleet   digestFleetSource
	digests *DigestService
	// connected is the control channel's liveness oracle (nil-safe).
	connected func(nodeID string) bool
	llm       *LLMManager
	metrics   telemetry.Metrics
	logf      func(format string, args ...any)
}

// NewChatService wires the chat layer.
func NewChatService(
	notif digestNotifSource,
	fleet digestFleetSource,
	digests *DigestService,
	connected func(nodeID string) bool,
	llmMgr *LLMManager,
	metrics telemetry.Metrics,
	logf func(string, ...any),
) *ChatService {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &ChatService{
		notif: notif, fleet: fleet, digests: digests,
		connected: connected, llm: llmMgr, metrics: metrics, logf: logf,
	}
}

// ErrChatBadRequest marks client errors (empty/oversized question).
var ErrChatBadRequest = errors.New("bad chat request")

// Validate normalizes the request and rejects malformed ones.
func (c *ChatService) Validate(req *ChatRequestBody) error {
	req.Question = strings.TrimSpace(req.Question)
	if req.Question == "" {
		return fmt.Errorf("%w: question is required", ErrChatBadRequest)
	}
	if len(req.Question) > chatMaxQuestionChars {
		return fmt.Errorf("%w: question exceeds %d characters", ErrChatBadRequest, chatMaxQuestionChars)
	}
	if req.WindowDays <= 0 {
		req.WindowDays = chatDefaultDays
	}
	if req.WindowDays > chatMaxDays {
		req.WindowDays = chatMaxDays
	}
	switch req.Lang {
	case "en", "ms", "zh", "ar":
	default:
		req.Lang = "en"
	}
	if len(req.History) > chatMaxHistoryTurns {
		req.History = req.History[len(req.History)-chatMaxHistoryTurns:]
	}
	for i := range req.History {
		if len(req.History[i].Content) > chatMaxHistoryChars {
			req.History[i].Content = req.History[i].Content[:chatMaxHistoryChars]
		}
		// History roles are constrained to the two conversational ones — a client
		// must not be able to smuggle a second system prompt in.
		if req.History[i].Role != "assistant" {
			req.History[i].Role = "user"
		}
	}
	return nil
}

// BuildGrounding assembles the bundle, enforcing the serialized size cap by
// dropping sections in reverse priority (recent → anomalies → digest → fleet
// tail) and recording what was dropped.
func (c *ChatService) BuildGrounding(ctx context.Context, req ChatRequestBody) (GroundingBundle, error) {
	now := time.Now()
	to := now.Unix()
	from := to - int64(req.WindowDays)*86400
	tzOffsetSec := int64(req.TZOffsetMin) * 60

	bundle := GroundingBundle{
		GeneratedAt: to,
		Window:      GroundWindow{From: from, To: to, Days: req.WindowDays},
	}

	// Fleet.
	if c.fleet != nil {
		nodes, err := c.fleet.List(ctx)
		if err == nil {
			bundle.FleetTotal = len(nodes)
			for i, n := range nodes {
				if i >= chatFleetCap {
					bundle.Truncated = append(bundle.Truncated, "fleet")
					break
				}
				gn := GroundNode{
					NodeId: n.NodeId, Name: n.Name, Kind: n.Kind,
					Status: n.Status, LastSeenAt: n.LastSeenAt,
				}
				if c.connected != nil {
					gn.Connected = c.connected(n.NodeId)
				}
				if n.CertExpiresAt > 0 {
					days := int((n.CertExpiresAt - to) / 86400)
					gn.CertDaysLeft = &days
				}
				bundle.Fleet = append(bundle.Fleet, gn)
			}
		}
	}

	// Stats + per-source anomalies.
	if c.notif != nil {
		if stats, err := c.notif.Stats(ctx, from, to, 86400, tzOffsetSec); err == nil && stats != nil {
			bundle.Stats = &GroundStats{
				Total: stats.Total, PrevTotal: stats.PrevTotal,
				Critical: stats.Critical, Warning: stats.Warning,
				BySource:   capItems(stats.BySource, 10),
				ByCategory: capItems(stats.ByCategory, 10),
			}
			for _, f := range sourceAnomalyFindings(ctx, c.notif, stats, from, to, tzOffsetSec) {
				if len(bundle.Anomalies) >= chatAnomalyCap {
					break
				}
				bundle.Anomalies = append(bundle.Anomalies, GroundAnomaly{
					Source:    str(f.Params["source"]),
					Count:     i64(f.Params["count"]),
					PrevCount: i64(f.Params["prevCount"]),
					Direction: str(f.Params["direction"]),
				})
			}
		}
		bundle.Recent = c.recentSevere(ctx, from)
	}

	// Latest digest findings (codes + params only — the narrative is prose and
	// would double-spend tokens).
	if c.digests != nil {
		if latest, err := c.digests.Latest(ctx); err == nil && latest != nil {
			var findings []Finding
			if json.Unmarshal([]byte(latest.FindingsJson), &findings) == nil {
				bundle.LatestDigest = findings
			}
		}
	}

	// Enforce the byte cap, cheapest-to-lose first.
	for _, drop := range []func(*GroundingBundle) string{
		func(b *GroundingBundle) string { b.Recent = nil; return "recentEvents" },
		func(b *GroundingBundle) string { b.Anomalies = nil; return "anomalies" },
		func(b *GroundingBundle) string { b.LatestDigest = nil; return "latestDigestFindings" },
		func(b *GroundingBundle) string {
			if len(b.Fleet) > 10 {
				b.Fleet = b.Fleet[:10]
			}
			return "fleet"
		},
	} {
		if bundleSize(bundle) <= chatMaxBundleBytes {
			break
		}
		bundle.Truncated = append(bundle.Truncated, drop(&bundle))
	}
	return bundle, nil
}

// recentSevere pages the feed newest-first for warning/critical rows in-window.
func (c *ChatService) recentSevere(ctx context.Context, from int64) []GroundNotif {
	var out []GroundNotif
	const page = 200
	for offset := uint64(0); offset < 1000 && len(out) < chatRecentCap; offset += page {
		items, _, err := c.notif.List(ctx, page, offset, 0, false, "", "")
		if err != nil || len(items) == 0 {
			return out
		}
		for _, n := range items {
			if n.CreatedAt < from {
				return out
			}
			if n.Source == digestOwnSource {
				continue // the digest's own feed entries are conclusions, not evidence
			}
			sev := strings.ToLower(n.Severity)
			if sev != string(notification.Warning) && sev != string(notification.Critical) {
				continue
			}
			title := n.Title
			if len(title) > chatTitleCap {
				title = title[:chatTitleCap]
			}
			out = append(out, GroundNotif{
				Id: n.Id, CreatedAt: n.CreatedAt, Severity: sev, Source: n.Source, Title: title,
			})
			if len(out) >= chatRecentCap {
				return out
			}
		}
		if len(items) < page {
			break
		}
	}
	return out
}

// SystemPrompt is the strict-grounding instruction, parameterized by language.
func SystemPrompt(lang string) string {
	return "You are the fleet assistant for a MySeliaSan physical-security control plane. " +
		"You will receive one JSON document (FLEET DATA) and a question about the fleet. Rules:\n" +
		"1. Answer ONLY from FLEET DATA. If the answer is not in it, say you do not have that data. Never guess.\n" +
		"2. When you reference an event or node, cite its id in brackets, e.g. [notif 4211] or [node cam-lobby].\n" +
		"3. Every number must appear verbatim in FLEET DATA. Never invent counts, times, or names.\n" +
		"4. If FLEET DATA lists truncated sections, mention that your view may be partial when relevant.\n" +
		"5. Be concise: at most 8 sentences, or a short list.\n" +
		"6. Reply in " + languageName(lang) + "."
}

// Stream runs one grounded completion, forwarding deltas to emit. Errors before
// any delta are returned directly (the API maps them to status codes); after
// the first delta the caller surfaces them as a stream error event.
func (c *ChatService) Stream(ctx context.Context, req ChatRequestBody, emit func(delta string) error) (llm.ChatResult, error) {
	if err := c.Validate(&req); err != nil {
		c.countChat("bad_request")
		return llm.ChatResult{}, err
	}
	client, err := c.llm.Client()
	if err != nil {
		if errors.Is(err, ErrLLMDisabled) {
			c.countChat("llm_unavailable")
		} else {
			c.countChat("llm_unavailable")
		}
		return llm.ChatResult{}, err
	}

	bundle, err := c.BuildGrounding(ctx, req)
	if err != nil {
		c.countChat("llm_error")
		return llm.ChatResult{}, fmt.Errorf("build grounding: %w", err)
	}
	bundleJSON, err := json.Marshal(bundle)
	if err != nil {
		c.countChat("llm_error")
		return llm.ChatResult{}, fmt.Errorf("encode grounding: %w", err)
	}

	messages := make([]llm.Message, 0, len(req.History)+2)
	messages = append(messages, llm.Message{
		Role:    "system",
		Content: SystemPrompt(req.Lang) + "\nFLEET DATA:\n" + string(bundleJSON),
	})
	messages = append(messages, req.History...)
	question := req.Question
	if req.Lang != "en" {
		// Small models weight the final instruction heaviest and routinely ignore a
		// reply-language rule buried in the system prompt (live-bench observed), so
		// the language ask is repeated right next to the question.
		question += "\n(Answer in " + languageName(req.Lang) + ".)"
	}
	messages = append(messages, llm.Message{Role: "user", Content: question})

	res, err := client.ChatStream(ctx, llm.ChatRequest{
		Messages:    messages,
		Temperature: 0.2,
		MaxTokens:   c.llm.MaxTokens(),
	}, emit)

	switch {
	case err == nil:
		c.countChat("ok")
		c.countLLM("chat", nil)
	case errors.Is(err, context.DeadlineExceeded):
		c.countChat("timeout")
		c.countLLM("chat", err)
	default:
		c.countChat("llm_error")
		c.countLLM("chat", err)
	}
	return res, err
}

func (c *ChatService) countChat(outcome string) {
	if c.metrics != nil {
		c.metrics.Inc(MetricAgentChatRequestsTotal, telemetry.Labels{"outcome": outcome})
	}
}

func (c *ChatService) countLLM(purpose string, err error) {
	if c.metrics == nil {
		return
	}
	outcome := "ok"
	if err != nil {
		outcome = "error"
		if errors.Is(err, context.DeadlineExceeded) {
			outcome = "timeout"
		}
	}
	c.metrics.Inc(MetricAgentLLMRequestsTotal, telemetry.Labels{"purpose": purpose, "outcome": outcome})
}

func bundleSize(b GroundingBundle) int {
	raw, err := json.Marshal(b)
	if err != nil {
		return chatMaxBundleBytes + 1
	}
	return len(raw)
}

func capItems(items []notification.CountItem, n int) []notification.CountItem {
	if len(items) > n {
		return items[:n]
	}
	return items
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

func i64(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	case float64:
		return int64(n)
	}
	return 0
}
