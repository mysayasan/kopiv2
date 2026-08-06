package apis

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mysayasan/kopiv2/apps/myseliasan/entities"
	"github.com/mysayasan/kopiv2/apps/myseliasan/services"
	"github.com/mysayasan/kopiv2/infra/config"
)

// newAgentTestApi builds an agentApi over an LLM manager in the given mode (an
// httptest endpoint for "external", nothing for "off"). The chat service reads
// nil sources safely because ChatService nil-guards its optional dependencies.
func newAgentTestApi(llmCfg config.AgentLLMConfigModel) (*agentApi, *recordingAudit) {
	mgr := services.NewLLMManager(llmCfg, nil)
	chat := services.NewChatService(nil, nil, nil, nil, nil, mgr, nil, nil)
	audit := &recordingAudit{}
	return &agentApi{
		chat:  chat,
		llm:   mgr,
		audit: audit,
	}, audit
}

func TestChatHandlerLLMOff(t *testing.T) {
	h, _ := newAgentTestApi(config.AgentLLMConfigModel{Mode: "off"})
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/agent/chat", strings.NewReader(`{"question":"anything?"}`))
	h.chatHandler(w, r)

	if w.Result().StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want 409", w.Result().StatusCode)
	}
	var body map[string]string
	json.Unmarshal(w.Body.Bytes(), &body)
	if body["code"] != "llm_disabled" {
		t.Fatalf("code = %q, want llm_disabled", body["code"])
	}
}

func TestChatHandlerBadRequest(t *testing.T) {
	h, _ := newAgentTestApi(config.AgentLLMConfigModel{Mode: "off"})
	for name, body := range map[string]string{
		"empty question": `{"question":"   "}`,
		"malformed json": `{"question": `,
	} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/api/agent/chat", strings.NewReader(body))
		h.chatHandler(w, r)
		if w.Result().StatusCode != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", name, w.Result().StatusCode)
		}
	}
}

func TestChatHandlerStreamsSSE(t *testing.T) {
	llmSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		for _, c := range []string{"All ", "quiet."} {
			fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":%q}}]}\n\n", c)
			fl.Flush()
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer llmSrv.Close()

	h, audit := newAgentTestApi(config.AgentLLMConfigModel{
		Mode: "external", Endpoint: llmSrv.URL, Model: "m", TimeoutSeconds: 5,
	})
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/agent/chat", strings.NewReader(`{"question":"status?","lang":"en"}`))
	h.chatHandler(w, r)

	res := w.Result()
	if res.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", res.StatusCode)
	}
	if ct := res.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type = %q", ct)
	}
	out := w.Body.String()
	for _, want := range []string{"event: meta", "event: delta", `{"text":"All "}`, "event: done"} {
		if !strings.Contains(out, want) {
			t.Fatalf("stream missing %q in:\n%s", want, out)
		}
	}
	// The chat is audited with the truncated question, never the full transcript.
	if len(audit.entries) != 1 || audit.entries[0].Action != "agent.chat" {
		t.Fatalf("audit entries = %+v", audit.entries)
	}
	if q, _ := audit.entries[0].Metadata["question"].(string); q != "status?" {
		t.Fatalf("audit question = %q", q)
	}
}

func TestChatHandlerNonStreamJSON(t *testing.T) {
	llmSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"choices":[{"message":{"content":"ignored"}}]}`) // non-stream shape
	}))
	defer llmSrv.Close()
	// ?stream=false goes through ChatStream server-side? No — Stream(nil emit) still
	// uses the streaming wire format, so serve a stream-shaped reply instead.
	llmStream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"Fine.\"}}]}\n\ndata: [DONE]\n\n")
	}))
	defer llmStream.Close()

	h, _ := newAgentTestApi(config.AgentLLMConfigModel{
		Mode: "external", Endpoint: llmStream.URL, Model: "m", TimeoutSeconds: 5,
	})
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/agent/chat?stream=false", strings.NewReader(`{"question":"ok?"}`))
	h.chatHandler(w, r)
	if w.Result().StatusCode != 200 {
		t.Fatalf("status = %d, body=%s", w.Result().StatusCode, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Fine.") {
		t.Fatalf("answer missing: %s", w.Body.String())
	}
}

func TestAgentManagementRequiresSuperadmin(t *testing.T) {
	h, _ := newAgentTestApi(config.AgentLLMConfigModel{Mode: "off"})
	for _, tc := range []struct {
		name    string
		handler http.HandlerFunc
	}{
		{"install binary", h.installBinary},
		{"install model", h.installModel},
		{"import", h.importArtifact},
		{"test", h.testLLM},
		{"sidecar restart", h.restartSidecar},
	} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/api/agent/llm/x", strings.NewReader(`{}`))
		h.requireSuper(tc.handler)(w, r)
		if w.Result().StatusCode == 200 {
			t.Errorf("%s: served without a superadmin session", tc.name)
		}
	}
}

func TestDigestDTODecodesFindings(t *testing.T) {
	findings := []services.Finding{{Code: "all_quiet", Severity: "info", Params: map[string]any{"windowHours": 24}}}
	raw, _ := json.Marshal(findings)
	dto := toDigestDTO(&entities.AgentDigest{Id: 1, FindingsJson: string(raw), Severity: "info"})
	if len(dto.Findings) != 1 || dto.Findings[0].Code != "all_quiet" {
		t.Fatalf("findings not decoded: %+v", dto.Findings)
	}
	if dto.FindingsJson != "" {
		t.Fatal("raw findings json must be stripped from the DTO")
	}
}
