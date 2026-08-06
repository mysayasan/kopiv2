package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/mysayasan/kopiv2/apps/myseliasan/entities"
	sharedentities "github.com/mysayasan/kopiv2/domain/entities"
	"github.com/mysayasan/kopiv2/domain/notification"
	"github.com/mysayasan/kopiv2/infra/config"
	"github.com/mysayasan/kopiv2/infra/control"
	"github.com/mysayasan/kopiv2/infra/llm"
)

func newTestChatService(notif *fakeNotifSource, fleet *fakeFleetSource, llmMgr *LLMManager) *ChatService {
	return NewChatService(notif, fleet, nil, func(string) bool { return true }, nil, llmMgr, nil, nil)
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

type fakeSender struct {
	status int
	body   string
	err    error
	gotReq *control.Request
}

func (f *fakeSender) SendRequest(_ context.Context, _ string, req control.Request) (control.Response, error) {
	f.gotReq = &req
	if f.err != nil {
		return control.Response{}, f.err
	}
	return control.Response{Status: f.status, Body: []byte(f.body)}, nil
}

func TestMatchNodeInQuestion(t *testing.T) {
	nodes := []*entities.ManagedNode{
		{NodeId: "abc-123", Name: "Gate"},
		{NodeId: "def-456", Name: "Gate camera 2"},
		{NodeId: "gh", Name: "X"}, // too short to ever match
	}
	if got := matchNodeInQuestion("what happened on gate camera 2 last night?", nodes); got == nil || got.NodeId != "def-456" {
		t.Fatalf("longest name must win, got %+v", got)
	}
	if got := matchNodeInQuestion("show me abc-123 events", nodes); got == nil || got.NodeId != "abc-123" {
		t.Fatalf("id match failed, got %+v", got)
	}
	if got := matchNodeInQuestion("how is the fleet?", nodes); got != nil {
		t.Fatalf("no node named, got %+v", got)
	}
}

func TestChatNodeDrillDown(t *testing.T) {
	fleet := &fakeFleetSource{nodes: []*entities.ManagedNode{{NodeId: "cam-yard", Name: "Yard cam", Status: "online"}}}
	sender := &fakeSender{status: 200, body: `{"result":{"items":[
		{"severity":"critical","title":"Person detected","createdAt":1785970000},
		{"severity":"info","title":"Health check ok","createdAt":1785960000}]}}`}
	c := NewChatService(&fakeNotifSource{stats: map[int64]*notification.Stats{}}, fleet, nil,
		func(string) bool { return true }, sender, nil, nil, nil)

	req := ChatRequestBody{Question: "what did yard cam see today?"}
	if err := c.Validate(&req); err != nil {
		t.Fatal(err)
	}
	bundle, err := c.BuildGrounding(context.Background(), req)
	if err != nil {
		t.Fatalf("BuildGrounding: %v", err)
	}
	nd := bundle.NodeDetail
	if nd == nil || nd.NodeId != "cam-yard" || nd.Unreachable {
		t.Fatalf("node detail missing/wrong: %+v", nd)
	}
	if len(nd.Recent) != 2 || nd.Recent[0].Title != "Person detected" {
		t.Fatalf("node events not parsed: %+v", nd.Recent)
	}
	// Tunnel request must be read-only viewer, on the notifications path.
	if sender.gotReq.Role != "viewer" || sender.gotReq.Method != "GET" ||
		!strings.HasPrefix(sender.gotReq.Path, "/api/notifications") {
		t.Fatalf("tunnel request wrong: %+v", sender.gotReq)
	}

	// Disconnected node → honest Unreachable, no tunnel call needed.
	c2 := NewChatService(&fakeNotifSource{stats: map[int64]*notification.Stats{}}, fleet, nil,
		func(string) bool { return false }, sender, nil, nil, nil)
	bundle, _ = c2.BuildGrounding(context.Background(), req)
	if bundle.NodeDetail == nil || !bundle.NodeDetail.Unreachable {
		t.Fatalf("offline node must be marked unreachable: %+v", bundle.NodeDetail)
	}
}

// Every timestamp reaching the model must already be formatted. A live bench
// caught a 7B turning a raw epoch into a date three years wrong.
func TestGroundingCarriesNoRawEpochs(t *testing.T) {
	now := time.Now().Unix()
	fleet := &fakeFleetSource{nodes: []*entities.ManagedNode{
		{NodeId: "cam-a", Name: "Cam A", Status: "lost", LastSeenAt: now - 7200},
	}}
	notif := &fakeNotifSource{stats: map[int64]*notification.Stats{}}
	notif.rows = []*sharedentities.Notification{
		{Id: 7, Severity: "critical", Source: "node:cam-a", Title: "Intrusion", CreatedAt: now - 600},
	}
	c := newTestChatService(notif, fleet, nil)
	req := ChatRequestBody{Question: "status?"}
	if err := c.Validate(&req); err != nil {
		t.Fatal(err)
	}
	bundle, err := c.BuildGrounding(context.Background(), req)
	if err != nil {
		t.Fatalf("BuildGrounding: %v", err)
	}
	if bundle.GeneratedAt == "" || !strings.Contains(bundle.GeneratedAt, "-") {
		t.Fatalf("generatedAt not formatted: %q", bundle.GeneratedAt)
	}
	if len(bundle.Fleet) == 0 || !strings.Contains(bundle.Fleet[0].LastSeenAt, "-") {
		t.Fatalf("lastSeenAt not formatted: %+v", bundle.Fleet)
	}
	if len(bundle.Recent) == 0 || !strings.Contains(bundle.Recent[0].CreatedAt, "-") {
		t.Fatalf("recent createdAt not formatted: %+v", bundle.Recent)
	}
	// Belt and braces: no bare 10-digit epoch anywhere in the serialized document.
	raw, _ := json.Marshal(bundle)
	if regexp.MustCompile(`:\s*1[6-9]\d{8}\b`).Match(raw) {
		t.Fatalf("raw unix timestamp leaked into the bundle:\n%s", raw)
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
