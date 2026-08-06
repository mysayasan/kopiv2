package llm

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNormalizeBaseURL(t *testing.T) {
	cases := map[string]string{
		"http://127.0.0.1:11434":     "http://127.0.0.1:11434/v1",
		"http://127.0.0.1:11434/":    "http://127.0.0.1:11434/v1",
		"http://127.0.0.1:11434/v1":  "http://127.0.0.1:11434/v1",
		"http://127.0.0.1:11434/v1/": "http://127.0.0.1:11434/v1",
		"  http://h/v1  ":            "http://h/v1",
		"":                           "",
	}
	for in, want := range cases {
		if got := NormalizeBaseURL(in); got != want {
			t.Errorf("NormalizeBaseURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestChatHappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer sk-test" {
			t.Errorf("missing bearer, got %q", got)
		}
		fmt.Fprint(w, `{"choices":[{"message":{"content":"hello fleet"}}],"usage":{"prompt_tokens":10,"completion_tokens":3}}`)
	}))
	defer srv.Close()

	c := New(srv.URL, "sk-test", "test-model", 5*time.Second)
	res, err := c.Chat(context.Background(), ChatRequest{Messages: []Message{{Role: "user", Content: "hi"}}})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if res.Content != "hello fleet" {
		t.Fatalf("content = %q", res.Content)
	}
	if res.Usage.PromptTokens != 10 || res.Usage.CompletionTokens != 3 {
		t.Fatalf("usage = %+v", res.Usage)
	}
}

func TestChatServerErrorSurfacesBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `model not loaded`, http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := New(srv.URL, "", "m", 5*time.Second)
	_, err := c.Chat(context.Background(), ChatRequest{Messages: []Message{{Role: "user", Content: "hi"}}})
	if err == nil || !strings.Contains(err.Error(), "model not loaded") {
		t.Fatalf("expected error carrying the body preview, got %v", err)
	}
}

func TestChatNoChoices(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"choices":[]}`)
	}))
	defer srv.Close()
	c := New(srv.URL, "", "m", 5*time.Second)
	if _, err := c.Chat(context.Background(), ChatRequest{}); err == nil {
		t.Fatal("expected error for empty choices")
	}
}

func streamHandler(lines []string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		for _, l := range lines {
			fmt.Fprintf(w, "%s\n\n", l)
			fl.Flush()
		}
	}
}

func TestChatStreamReassemblesDeltas(t *testing.T) {
	srv := httptest.NewServer(streamHandler([]string{
		`data: {"choices":[{"delta":{"content":"The "}}]}`,
		`data: {"choices":[{"delta":{"content":"fleet "}}]}`,
		`: keep-alive comment`,
		`data: {"choices":[{"delta":{"content":"is fine."}}]}`,
		`data: {"choices":[],"usage":{"prompt_tokens":42,"completion_tokens":4}}`,
		`data: [DONE]`,
	}))
	defer srv.Close()

	c := New(srv.URL, "", "m", 5*time.Second)
	var got []string
	res, err := c.ChatStream(context.Background(), ChatRequest{}, func(d string) error {
		got = append(got, d)
		return nil
	})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	if res.Content != "The fleet is fine." {
		t.Fatalf("content = %q", res.Content)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 deltas, got %d (%v)", len(got), got)
	}
	if res.Usage.PromptTokens != 42 {
		t.Fatalf("usage not captured from trailing chunk: %+v", res.Usage)
	}
}

func TestChatStreamToleratesMissingDoneAndGarbage(t *testing.T) {
	srv := httptest.NewServer(streamHandler([]string{
		`data: {"choices":[{"delta":{"content":"ok"}}]}`,
		`data: {not json at all`,
		`data: {"choices":[{"delta":{"content":"!"}}]}`,
		// no [DONE]; the server just closes.
	}))
	defer srv.Close()

	c := New(srv.URL, "", "m", 5*time.Second)
	res, err := c.ChatStream(context.Background(), ChatRequest{}, nil)
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	if res.Content != "ok!" {
		t.Fatalf("content = %q (garbage chunk must be skipped)", res.Content)
	}
}

func TestChatStreamOnDeltaAbort(t *testing.T) {
	srv := httptest.NewServer(streamHandler([]string{
		`data: {"choices":[{"delta":{"content":"a"}}]}`,
		`data: {"choices":[{"delta":{"content":"b"}}]}`,
		`data: [DONE]`,
	}))
	defer srv.Close()

	c := New(srv.URL, "", "m", 5*time.Second)
	abort := errors.New("client went away")
	res, err := c.ChatStream(context.Background(), ChatRequest{}, func(d string) error { return abort })
	if !errors.Is(err, abort) {
		t.Fatalf("expected the onDelta error back, got %v", err)
	}
	if res.Content != "a" {
		t.Fatalf("partial content should be kept, got %q", res.Content)
	}
}

func TestChatStreamCtxTimeoutKeepsPartial(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n")
		fl.Flush()
		<-release // hold the stream open past the ctx deadline
	}))
	defer srv.Close()
	defer close(release)

	c := New(srv.URL, "", "m", 30*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	res, err := c.ChatStream(ctx, ChatRequest{}, nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected DeadlineExceeded, got %v", err)
	}
	if res.Content != "partial" {
		t.Fatalf("partial content should survive a timeout, got %q", res.Content)
	}
}

func TestProbeModelsOKAndAuthRejection(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			if r.Header.Get("Authorization") == "Bearer good" {
				fmt.Fprint(w, `{"data":[]}`)
				return
			}
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	if err := New(srv.URL, "good", "m", 5*time.Second).Probe(context.Background()); err != nil {
		t.Fatalf("Probe with good key: %v", err)
	}
	err := New(srv.URL, "bad", "m", 5*time.Second).Probe(context.Background())
	if err == nil || !strings.Contains(err.Error(), "API key") {
		t.Fatalf("expected API-key rejection, got %v", err)
	}
}

func TestProbeFallsBackToCompletion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, `{"choices":[{"message":{"content":"p"}}]}`)
	}))
	defer srv.Close()
	if err := New(srv.URL, "", "m", 5*time.Second).Probe(context.Background()); err != nil {
		t.Fatalf("Probe fallback: %v", err)
	}
}

func TestProbeUnreachable(t *testing.T) {
	// A port nothing listens on — connection refused, promptly.
	c := New("http://127.0.0.1:1", "", "m", 2*time.Second)
	if err := c.Probe(context.Background()); err == nil {
		t.Fatal("expected unreachable error")
	}
}
