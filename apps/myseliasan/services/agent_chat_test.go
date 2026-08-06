package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mysayasan/kopiv2/apps/myseliasan/entities"
	sharedentities "github.com/mysayasan/kopiv2/domain/entities"
	"github.com/mysayasan/kopiv2/domain/notification"
	"github.com/mysayasan/kopiv2/infra/config"
	"github.com/mysayasan/kopiv2/infra/llm"
)

func newTestChatService(notif *fakeNotifSource, fleet *fakeFleetSource, llmMgr *LLMManager) *ChatService {
	return NewChatService(notif, fleet, nil, func(string) bool { return true }, llmMgr, nil, nil)
}

func TestChatValidate(t *testing.T) {
	c := newTestChatService(&fakeNotifSource{}, &fakeFleetSource{}, nil)

	req := &ChatRequestBody{Question: "  hello  ", Lang: "xx", WindowDays: 99}
	if err := c.Validate(req); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if req.Question != "hello" || req.Lang != "en" || req.WindowDays != chatMaxDays {
		t.Fatalf("normalization wrong: %+v", req)
	}

	if err := c.Validate(&ChatRequestBody{Question: "   "}); !errors.Is(err, ErrChatBadRequest) {
		t.Fatalf("empty question must be bad request, got %v", err)
	}
	if err := c.Validate(&ChatRequestBody{Question: strings.Repeat("x", chatMaxQuestionChars+1)}); !errors.Is(err, ErrChatBadRequest) {
		t.Fatalf("oversized question must be bad request, got %v", err)
	}
}

func TestChatValidateHistory(t *testing.T) {
	c := newTestChatService(&fakeNotifSource{}, &fakeFleetSource{}, nil)
	req := &ChatRequestBody{Question: "q"}
	for i := 0; i < 8; i++ {
		req.History = append(req.History, llm.Message{Role: "system", Content: strings.Repeat("a", 1000)})
	}
	if err := c.Validate(req); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if len(req.History) != chatMaxHistoryTurns {
		t.Fatalf("history not trimmed: %d", len(req.History))
	}
	for _, h := range req.History {
		if h.Role == "system" {
			t.Fatal("a client-smuggled system role must be demoted")
		}
		if len(h.Content) > chatMaxHistoryChars {
			t.Fatal("history content not truncated")
		}
	}
}

func TestBuildGroundingRespectsByteCap(t *testing.T) {
	// 500 nodes + a large feed: the serialized bundle must still fit the cap and
	// record what it dropped.
	fleet := &fakeFleetSource{}
	for i := 0; i < 500; i++ {
		fleet.nodes = append(fleet.nodes, &entities.ManagedNode{
			NodeId: fmt.Sprintf("node-%03d-with-a-rather-long-identifier", i),
			Name:   fmt.Sprintf("A very descriptively named appliance number %d", i),
			Status: "online", CertExpiresAt: time.Now().Unix() + 90*86400,
		})
	}
	notif := &fakeNotifSource{stats: map[int64]*notification.Stats{}}
	for i := int64(0); i < 300; i++ {
		notif.rows = append(notif.rows, &sharedentities.Notification{
			Id: i, Severity: "critical", Source: "node:x",
			Title:     strings.Repeat("Long alert title ", 10),
			CreatedAt: time.Now().Unix(),
		})
	}

	c := newTestChatService(notif, fleet, nil)
	req := ChatRequestBody{Question: "q"}
	if err := c.Validate(&req); err != nil {
		t.Fatal(err)
	}
	bundle, err := c.BuildGrounding(context.Background(), req)
	if err != nil {
		t.Fatalf("BuildGrounding: %v", err)
	}
	size := bundleSize(bundle)
	if size > chatMaxBundleBytes {
		t.Fatalf("bundle %d bytes exceeds the %d cap", size, chatMaxBundleBytes)
	}
	if bundle.FleetTotal != 500 {
		t.Fatalf("fleetTotal must report the real count, got %d", bundle.FleetTotal)
	}
	if len(bundle.Truncated) == 0 {
		t.Fatal("truncation must be recorded so the model knows its view is partial")
	}
	// Recent-event titles are clipped.
	for _, r := range bundle.Recent {
		if len(r.Title) > chatTitleCap {
			t.Fatal("recent title not clipped")
		}
	}
}

func TestChatStreamGroundedHappyPath(t *testing.T) {
	var gotSystem string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []struct{ Role, Content string } `json:"messages"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		if len(body.Messages) > 0 {
			gotSystem = body.Messages[0].Content
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		for _, chunk := range []string{"Node ", "[node gate-cam] ", "is lost."} {
			fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":%q}}]}\n\n", chunk)
			fl.Flush()
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	fleet := &fakeFleetSource{nodes: []*entities.ManagedNode{
		{NodeId: "gate-cam", Name: "Gate cam", Status: "lost"},
	}}
	c := newTestChatService(&fakeNotifSource{stats: map[int64]*notification.Stats{}}, fleet,
		externalLLM(srv.URL))

	var deltas []string
	res, err := c.Stream(context.Background(), ChatRequestBody{Question: "which node is down?", Lang: "en"},
		func(d string) error { deltas = append(deltas, d); return nil })
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if res.Content != "Node [node gate-cam] is lost." || len(deltas) != 3 {
		t.Fatalf("streamed answer wrong: %q (%d deltas)", res.Content, len(deltas))
	}
	// The system prompt must embed the grounding document and the rules.
	if !strings.Contains(gotSystem, "FLEET DATA") || !strings.Contains(gotSystem, "gate-cam") {
		t.Fatalf("system prompt not grounded: %.200s", gotSystem)
	}
	if !strings.Contains(gotSystem, "Answer ONLY from FLEET DATA") {
		t.Fatal("grounding rule missing from system prompt")
	}
}

func TestChatStreamLLMOff(t *testing.T) {
	c := newTestChatService(&fakeNotifSource{stats: map[int64]*notification.Stats{}}, &fakeFleetSource{},
		NewLLMManager(config.AgentLLMConfigModel{Mode: "off"}, nil))
	_, err := c.Stream(context.Background(), ChatRequestBody{Question: "hi"}, nil)
	if !errors.Is(err, ErrLLMDisabled) {
		t.Fatalf("expected ErrLLMDisabled, got %v", err)
	}
}

func TestChatStreamSidecarNotReady(t *testing.T) {
	sidecar := NewLLMSidecar(SidecarConfig{Enabled: true}, t.TempDir(), nil)
	c := newTestChatService(&fakeNotifSource{stats: map[int64]*notification.Stats{}}, &fakeFleetSource{},
		NewLLMManager(config.AgentLLMConfigModel{Mode: "sidecar"}, sidecar))
	_, err := c.Stream(context.Background(), ChatRequestBody{Question: "hi"}, nil)
	if !errors.Is(err, ErrLLMNotReady) {
		t.Fatalf("expected ErrLLMNotReady, got %v", err)
	}
}

func TestSystemPromptLanguages(t *testing.T) {
	for lang, want := range map[string]string{
		"en": "English", "ms": "Malay", "zh": "Chinese", "ar": "Arabic",
	} {
		if p := SystemPrompt(lang); !strings.Contains(p, want) {
			t.Errorf("SystemPrompt(%s) missing %q", lang, want)
		}
	}
}
