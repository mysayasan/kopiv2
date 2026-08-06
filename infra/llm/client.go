// Package llm is a minimal OpenAI-compatible chat-completions client.
//
// It exists so the suite can talk to ANY local inference server that speaks the
// de-facto /v1/chat/completions wire format — llama.cpp's llama-server, Ollama,
// vLLM, LM Studio — without taking an SDK dependency. Everything here is stdlib.
//
// Design constraints, in order:
//  1. The caller is an air-gapped control plane. The client never decides where
//     to connect — the base URL is operator configuration — and it never retries
//     on its own (the features above it degrade deterministically instead).
//  2. An LLM must never wedge the app: every request carries the caller's ctx,
//     bodies are size-capped, and a malformed stream chunk is skipped, not fatal.
//  3. Small models on CPU are slow, so streaming is first-class: ChatStream
//     surfaces deltas the moment the server flushes them.
package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Message is one chat turn. Role is "system", "user", or "assistant".
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatRequest shapes one completion call. Model overrides the client's default
// when set. MaxTokens 0 lets the server decide; Temperature 0 is sent verbatim
// (deterministic-ish output, which is what summarization wants).
type ChatRequest struct {
	Model       string
	Messages    []Message
	Temperature float64
	MaxTokens   int
}

// Usage is the token accounting the server reported (zero when it reports none —
// llama-server includes it, some proxies do not).
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

// ChatResult is the completed answer.
type ChatResult struct {
	Content string
	Usage   Usage
}

const (
	// maxResponseBytes caps a non-streaming response body. A completion is a few
	// KiB; anything near this cap is a misbehaving server.
	maxResponseBytes = 1 << 20
	// maxStreamLineBytes caps one SSE line of a streaming response.
	maxStreamLineBytes = 256 << 10
	// probeTimeout bounds Probe regardless of the client's request timeout.
	probeTimeout = 8 * time.Second
	// errBodyPreview is how much of an error response body is surfaced in errors.
	errBodyPreview = 300
)

// Client talks to one OpenAI-compatible endpoint. Construct with New.
type Client struct {
	// BaseURL is the API base ending at the version segment, e.g.
	// "http://127.0.0.1:49540/v1". POSTs go to BaseURL + "/chat/completions".
	BaseURL string
	// APIKey, when set, is sent as "Authorization: Bearer <key>".
	APIKey string
	// Model is the default model name sent when ChatRequest.Model is empty.
	Model string
	// HTTP is the transport; its Timeout is the per-request ceiling.
	HTTP *http.Client
}

// New builds a client. baseURL may be given with or without the trailing "/v1"
// — it is normalized to end in "/v1" because every server this client targets
// mounts the OpenAI surface there.
func New(baseURL, apiKey, model string, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	return &Client{
		BaseURL: NormalizeBaseURL(baseURL),
		APIKey:  apiKey,
		Model:   model,
		HTTP:    &http.Client{Timeout: timeout},
	}
}

// NormalizeBaseURL trims trailing slashes and appends "/v1" when absent, so
// operators can paste "http://host:11434", "http://host:11434/" or
// "http://host:11434/v1" interchangeably.
func NormalizeBaseURL(baseURL string) string {
	u := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if u == "" {
		return u
	}
	if !strings.HasSuffix(u, "/v1") {
		u += "/v1"
	}
	return u
}

// wire types (the subset of the OpenAI schema this client reads/writes).

type wireRequest struct {
	Model       string        `json:"model"`
	Messages    []Message     `json:"messages"`
	Temperature float64       `json:"temperature"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
	Stream      bool          `json:"stream,omitempty"`
	StreamOpts  *wireStreamOp `json:"stream_options,omitempty"`
}

type wireStreamOp struct {
	IncludeUsage bool `json:"include_usage"`
}

type wireResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
	} `json:"choices"`
	Usage *Usage `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Chat performs one non-streaming completion.
func (c *Client) Chat(ctx context.Context, req ChatRequest) (ChatResult, error) {
	resp, err := c.post(ctx, req, false)
	if err != nil {
		return ChatResult{}, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return ChatResult{}, fmt.Errorf("llm: read response: %w", err)
	}
	var wire wireResponse
	if err := json.Unmarshal(body, &wire); err != nil {
		return ChatResult{}, fmt.Errorf("llm: malformed response: %w", err)
	}
	if wire.Error != nil && wire.Error.Message != "" {
		return ChatResult{}, fmt.Errorf("llm: server error: %s", wire.Error.Message)
	}
	if len(wire.Choices) == 0 {
		return ChatResult{}, fmt.Errorf("llm: response carried no choices")
	}
	out := ChatResult{Content: wire.Choices[0].Message.Content}
	if wire.Usage != nil {
		out.Usage = *wire.Usage
	}
	return out, nil
}

// ChatStream performs a streaming completion, invoking onDelta for every content
// fragment as it arrives. Returning an error from onDelta aborts the request
// (that error is returned). The full concatenated text is returned either way.
//
// The wire format is the standard SSE-ish stream: lines of "data: {chunk}"
// terminated by "data: [DONE]". Servers that omit [DONE] and just close the
// stream are tolerated; a malformed chunk line is skipped rather than fatal. If
// the ctx is cancelled mid-stream, the accumulated text is returned along with
// the ctx error, so a caller can keep partial output on a timeout.
func (c *Client) ChatStream(ctx context.Context, req ChatRequest, onDelta func(delta string) error) (ChatResult, error) {
	resp, err := c.post(ctx, req, true)
	if err != nil {
		return ChatResult{}, err
	}
	defer resp.Body.Close()

	var (
		text  strings.Builder
		usage Usage
	)
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64<<10), maxStreamLineBytes)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || !strings.HasPrefix(line, "data:") {
			continue // SSE comments, event: lines, keep-alives
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "[DONE]" {
			break
		}
		var wire wireResponse
		if err := json.Unmarshal([]byte(payload), &wire); err != nil {
			continue // one garbled chunk must not kill the whole answer
		}
		if wire.Error != nil && wire.Error.Message != "" {
			return ChatResult{Content: text.String(), Usage: usage},
				fmt.Errorf("llm: server error mid-stream: %s", wire.Error.Message)
		}
		if wire.Usage != nil {
			usage = *wire.Usage
		}
		if len(wire.Choices) == 0 {
			continue // the final usage-only chunk has no choices
		}
		delta := wire.Choices[0].Delta.Content
		if delta == "" {
			continue
		}
		text.WriteString(delta)
		if onDelta != nil {
			if err := onDelta(delta); err != nil {
				return ChatResult{Content: text.String(), Usage: usage}, err
			}
		}
	}
	if err := scanner.Err(); err != nil {
		// Prefer the ctx error: "context deadline exceeded" tells the caller what
		// actually happened where the raw net error ("body closed") does not.
		if ctx.Err() != nil {
			return ChatResult{Content: text.String(), Usage: usage}, ctx.Err()
		}
		return ChatResult{Content: text.String(), Usage: usage}, fmt.Errorf("llm: stream read: %w", err)
	}
	return ChatResult{Content: text.String(), Usage: usage}, nil
}

// Probe cheaply verifies the endpoint is an alive OpenAI-compatible server:
// GET {base}/models, falling back to a one-token completion for servers that do
// not implement the models listing. Bounded by probeTimeout regardless of the
// client's configured request timeout.
func (c *Client) Probe(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/models", nil)
	if err != nil {
		return fmt.Errorf("llm: probe: %w", err)
	}
	c.setHeaders(httpReq)
	resp, err := c.HTTP.Do(httpReq)
	if err != nil {
		return fmt.Errorf("llm: endpoint unreachable: %w", err)
	}
	io.Copy(io.Discard, io.LimitReader(resp.Body, maxResponseBytes)) //nolint:errcheck
	resp.Body.Close()
	if resp.StatusCode < 400 {
		return nil
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("llm: endpoint rejected the API key (%s)", resp.Status)
	}

	// No /models (404 and friends): prove liveness with a minimal completion.
	_, err = c.Chat(ctx, ChatRequest{
		Messages:  []Message{{Role: "user", Content: "ping"}},
		MaxTokens: 1,
	})
	return err
}

func (c *Client) post(ctx context.Context, req ChatRequest, stream bool) (*http.Response, error) {
	model := req.Model
	if model == "" {
		model = c.Model
	}
	wire := wireRequest{
		Model:       model,
		Messages:    req.Messages,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
		Stream:      stream,
	}
	if stream {
		wire.StreamOpts = &wireStreamOp{IncludeUsage: true}
	}
	body, err := json.Marshal(wire)
	if err != nil {
		return nil, fmt.Errorf("llm: encode request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("llm: build request: %w", err)
	}
	c.setHeaders(httpReq)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTP.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("llm: request failed: %w", err)
	}
	if resp.StatusCode >= 400 {
		preview, _ := io.ReadAll(io.LimitReader(resp.Body, errBodyPreview))
		resp.Body.Close()
		return nil, fmt.Errorf("llm: %s: %s", resp.Status, strings.TrimSpace(string(preview)))
	}
	return resp, nil
}

func (c *Client) setHeaders(req *http.Request) {
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	req.Header.Set("User-Agent", "kopiv2-llm/1.0")
}
