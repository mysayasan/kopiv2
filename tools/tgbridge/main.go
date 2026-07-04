// Command tgbridge relays a Telegram chat to the headless Claude Code CLI,
// so you can drive coding sessions from your phone the way you would in VSCode.
//
// It maintains a registry of named Claude sessions and an "active" pointer per
// chat, so you can switch between independent conversation threads (like tabs).
//
// Dependency-free (stdlib only). Uses Telegram long-polling (no open ports).
//
// Config is read from a JSON file (default tools/tgbridge/config.json, override
// with -config). Copy config.json.example to config.json and fill it in; the
// real config.json is gitignored so your bot token never hits git.
//
// Run from the repo root:  go run ./tools/tgbridge
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// httpClient forces IPv4 dialing to avoid a broken IPv6 Happy-Eyeballs fallback
// (api.telegram.org resolves but the v6 path stalls on some networks). Timeout
// is longer than the 50s getUpdates long-poll. Honors HTTPS_PROXY if set.
var httpClient = &http.Client{
	Timeout: 70 * time.Second,
	Transport: &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			if network == "tcp" {
				network = "tcp4" // force IPv4
			}
			d := &net.Dialer{Timeout: 15 * time.Second}
			return d.DialContext(ctx, network, addr)
		},
	},
}

// ---------------------------------------------------------------------------
// config
// ---------------------------------------------------------------------------

// Config mirrors tools/tgbridge/config.json.
type Config struct {
	BotToken      string   `json:"bot_token"`       // from @BotFather
	AllowedUserID string   `json:"allowed_user_id"` // YOUR numeric Telegram user id (from @userinfobot)
	WorkDir       string   `json:"workdir"`         // default project dir Claude runs in
	AllowedTools  []string `json:"allowed_tools"`   // optional; overrides the default allowlist
	ProjectsDir   string   `json:"projects_dir"`    // optional; override the ~/.claude/projects/<encoded> dir
	VoiceNotify   *bool    `json:"voice_notify"`    // optional; spoken completion voice note (default true)
	VoiceTarget   string   `json:"voice_target"`    // optional; pc | phone | both (default pc)
	FFmpegPath    string   `json:"ffmpeg_path"`     // optional; else looked up on PATH (needed for the effect/voice note)
	ChromePath    string   `json:"chrome_path"`     // optional; else auto-detected (for screenshots)
	OutboxDir     string   `json:"outbox_dir"`      // optional; else <state dir>/outbox — files here are sent to the phone
	StreamMode    *bool    `json:"stream_mode"`     // optional; bidirectional streaming w/ live text + permission buttons (default true)
	PlayIncoming  *bool    `json:"play_incoming_voice"` // optional; play voice notes you send on the PC speakers (default true)
	VoiceInput    string   `json:"voice_input"`         // optional; "play" (default) or "task" (transcribe voice → prompt)
	TranscribeCmd string   `json:"transcribe_cmd"`      // optional; external STT command (gets the wav path as last arg)
	STTApiURL     string   `json:"stt_api_url"`         // optional; OpenAI-compatible /audio/transcriptions endpoint (local or cloud)
	STTApiKey     string   `json:"stt_api_key"`         // optional; bearer key for the STT endpoint (defaults URL to OpenAI if set)
	STTModel      string   `json:"stt_model"`           // optional; STT model name (default whisper-1)
	ChromeUserDir string   `json:"chrome_user_data_dir"` // optional; persistent Chrome profile for authenticated screenshots
	CameraDevice  string   `json:"camera_device"`        // optional; DirectShow webcam name for /camshot (else first video device)
}

var (
	botToken    string
	allowedID   string
	workDir     string
	projectsDir string // optional override; else derived from workDir
	voiceNotify = true  // spoken completion voice note; toggle with /voice
	voiceTarget = "pc"  // where the voice plays: pc | phone | both
	ffmpegPath  string  // optional explicit ffmpeg path; else PATH lookup
	chromePath  string  // chrome/edge for screenshots; auto-detected if unset
	outboxDir   string  // files dropped here are auto-sent to the phone
	sysPrompt   string  // appended to every turn; tells the agent about the outbox
	streamMode    = true   // bidirectional streaming: live text + permission buttons
	playIncoming  = true   // play voice notes you send on the PC speakers
	voiceMode     = "play"      // "play" or "task" (transcribe voice → prompt)
	transcribeCmd string        // external STT command; else STT API; else Windows STT
	sttURL        string        // OpenAI-compatible transcription endpoint
	sttKey        string        // bearer key for sttURL
	sttModel      = "whisper-1" // STT model name
	chromeUserDir string        // persistent Chrome profile for authenticated screenshots
	cameraDevice  string        // DirectShow webcam name for /camshot (else auto-detect first)
)

// pending holds in-flight permission requests awaiting a Telegram button tap,
// keyed by the CLI's control-request id.
var pending = struct {
	sync.Mutex
	m map[string]chan bool
}{m: map[string]chan bool{}}

// asks holds the option text behind each ASK question button (Telegram callback
// data is limited to 64 bytes, so we key by a short token and store the real
// choice here). Tapping a button feeds the stored text back into the session.
var asks = struct {
	sync.Mutex
	m map[string]string
}{m: map[string]string{}}

func api() string { return "https://api.telegram.org/bot" + botToken }

// Shell commands the agent is allowed to run unattended from your phone.
// acceptEdits auto-approves file edits; Bash still needs an explicit allowlist
// because there is no interactive prompt in headless mode. Override via config.
var allowedTools = []string{
	"Bash(go build:*)",
	"Bash(go test:*)",
	"Bash(git status:*)",
	"Bash(git diff:*)",
}

// ---------------------------------------------------------------------------
// persistent state: named sessions + per-chat active pointer
// ---------------------------------------------------------------------------

type Session struct {
	Label    string `json:"label"`
	ID       string `json:"id"`                  // claude session_id, filled after first turn
	WorkDir  string `json:"workdir"`             // project dir for this session
	Model    string `json:"model,omitempty"`     // "", opus, sonnet, haiku, fable
	PermMode string `json:"perm_mode,omitempty"` // "" => acceptEdits
	Effort   string `json:"effort,omitempty"`    // "", low, medium, high, xhigh, max
	LastUsed string `json:"last_used"`
	// usage accounting, accumulated from each run's result event (see /usage)
	CostUSD float64 `json:"cost_usd,omitempty"`
	InTok   int64   `json:"in_tok,omitempty"`  // input + cache tokens
	OutTok  int64   `json:"out_tok,omitempty"` // output tokens
	Turns   int     `json:"turns,omitempty"`
}

type State struct {
	Sessions []*Session       `json:"sessions"`
	Active   map[int64]string `json:"active"` // chat_id -> session label
	// lifetime usage totals (survive session deletion) — see /usage
	TotalCostUSD float64 `json:"total_cost_usd,omitempty"`
	TotalInTok   int64   `json:"total_in_tok,omitempty"`
	TotalOutTok  int64   `json:"total_out_tok,omitempty"`
}

var (
	mu    sync.Mutex
	state = State{Active: map[int64]string{}}
	file  = "tgbridge_state.json"
)

func load() {
	if b, err := os.ReadFile(file); err == nil {
		json.Unmarshal(b, &state)
	}
	if state.Active == nil {
		state.Active = map[int64]string{}
	}
}

func save() {
	if b, err := json.Marshal(state); err == nil {
		os.WriteFile(file, b, 0600)
	}
}

func find(label string) *Session {
	for _, s := range state.Sessions {
		if s.Label == label {
			return s
		}
	}
	return nil
}

func activeSession(chat int64) *Session { return find(state.Active[chat]) }

// ---------------------------------------------------------------------------
// Telegram API (only the fields we use)
// ---------------------------------------------------------------------------

type tgUpdate struct {
	UpdateID int64 `json:"update_id"`
	Message  *struct {
		Chat struct {
			ID int64 `json:"id"`
		} `json:"chat"`
		From struct {
			ID int64 `json:"id"`
		} `json:"from"`
		Text  string `json:"text"`
		Voice *struct {
			FileID string `json:"file_id"`
		} `json:"voice"`
	} `json:"message"`
	CallbackQuery *struct {
		ID   string `json:"id"`
		From struct {
			ID int64 `json:"id"`
		} `json:"from"`
		Message struct {
			Chat struct {
				ID int64 `json:"id"`
			} `json:"chat"`
		} `json:"message"`
		Data string `json:"data"`
	} `json:"callback_query"`
}

func getUpdates(offset int64) ([]tgUpdate, error) {
	u := fmt.Sprintf("%s/getUpdates?timeout=50&offset=%d", api(), offset)
	resp, err := httpClient.Get(u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var out struct {
		OK          bool       `json:"ok"`
		Description string     `json:"description"`
		Result      []tgUpdate `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if !out.OK {
		return nil, fmt.Errorf("telegram API: %s", out.Description)
	}
	return out.Result, nil
}

// send chunks around Telegram's 4096-char message cap.
func send(chat int64, text string) {
	if text == "" {
		return
	}
	for len(text) > 0 {
		chunk := text
		if len(chunk) > 3900 {
			chunk, text = chunk[:3900], chunk[3900:]
		} else {
			text = ""
		}
		httpClient.PostForm(api()+"/sendMessage", url.Values{
			"chat_id": {strconv.FormatInt(chat, 10)},
			"text":    {chunk},
		})
	}
}

func sendKeyboard(chat int64, text string, rows [][]map[string]string) {
	markup, _ := json.Marshal(map[string]any{"inline_keyboard": rows})
	httpClient.PostForm(api()+"/sendMessage", url.Values{
		"chat_id":      {strconv.FormatInt(chat, 10)},
		"text":         {text},
		"reply_markup": {string(markup)},
	})
}

func answerCallback(id string) {
	httpClient.PostForm(api()+"/answerCallbackQuery", url.Values{"callback_query_id": {id}})
}

// sendReturningID sends a message and returns its message_id (for live editing).
func sendReturningID(chat int64, text string) (int64, error) {
	resp, err := httpClient.PostForm(api()+"/sendMessage", url.Values{
		"chat_id": {strconv.FormatInt(chat, 10)},
		"text":    {text},
	})
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	var out struct {
		OK     bool `json:"ok"`
		Result struct {
			MessageID int64 `json:"message_id"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return 0, err
	}
	if !out.OK {
		return 0, fmt.Errorf("sendMessage failed")
	}
	return out.Result.MessageID, nil
}

// editMessage replaces the text of an existing message (used for live streaming).
func editMessage(chat, msgID int64, text string) {
	httpClient.PostForm(api()+"/editMessageText", url.Values{
		"chat_id":    {strconv.FormatInt(chat, 10)},
		"message_id": {strconv.FormatInt(msgID, 10)},
		"text":       {text},
	})
}

// ---------------------------------------------------------------------------
// session UI
// ---------------------------------------------------------------------------

func showSessions(chat int64) {
	var rows [][]map[string]string

	// bridge-managed (named) sessions
	adopted := map[string]bool{}
	for i, s := range state.Sessions {
		if s.ID != "" {
			adopted[s.ID] = true
		}
		mark := "   "
		if s.Label == state.Active[chat] {
			mark = "✅ "
		}
		rows = append(rows, []map[string]string{{
			"text":          mark + s.Label,
			"callback_data": "sw:" + strconv.Itoa(i), // index keeps payload < 64 bytes
		}})
	}

	// existing Claude sessions on disk (VSCode/CLI) — tap to resume
	for _, d := range discoverSessions(12) {
		if adopted[d.ID] {
			continue
		}
		rows = append(rows, []map[string]string{{
			"text":          "↩ " + d.Label,
			"callback_data": "res:" + d.ID, // 4 + 36-char uuid < 64 bytes
		}})
	}

	if len(rows) == 0 {
		send(chat, "No sessions found. Create one:  /new <name>")
		return
	}
	sendKeyboard(chat, "Sessions — tap to switch (↩ = resume an existing Claude session):", rows)
}

// choice option lists (mirror the claude CLI's accepted values)
var (
	modelOpts  = []string{"opus", "sonnet", "haiku", "fable"}
	modeOpts   = []string{"default", "acceptEdits", "plan", "bypassPermissions", "dontAsk", "auto"}
	effortOpts = []string{"low", "medium", "high", "xhigh", "max"}
)

func orDefault(v string) string {
	if v == "" {
		return "(default)"
	}
	return v
}

// def returns v, or fallback when v is empty.
func def(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

// choiceKeyboard renders options 2-per-row, marking the current one with ✅.
func choiceKeyboard(chat int64, title, prefix, current string, opts []string) {
	var rows [][]map[string]string
	var row []map[string]string
	for i, o := range opts {
		label := o
		if o == current {
			label = "✅ " + o
		}
		row = append(row, map[string]string{"text": label, "callback_data": prefix + o})
		if len(row) == 2 || i == len(opts)-1 {
			rows = append(rows, row)
			row = nil
		}
	}
	sendKeyboard(chat, title, rows)
}

// setSetting applies a model/mode/effort choice to the active session.
func setSetting(chat int64, kind, val string) {
	s := activeSession(chat)
	if s == nil {
		send(chat, "No active session — /new <name> or /sessions first.")
		return
	}
	mu.Lock()
	switch kind {
	case "model":
		s.Model = val
	case "mode":
		s.PermMode = val
	case "effort":
		s.Effort = val
	}
	mu.Unlock()
	save()
	send(chat, "✅ "+kind+" → "+val+"   ["+s.Label+"]")
}

// ---------------------------------------------------------------------------
// discovery of existing on-disk Claude sessions
// ---------------------------------------------------------------------------

type discovered struct {
	ID    string
	Label string
	Mod   time.Time
}

// claudeProjectsDir returns where Claude Code stores this project's sessions.
// Claude encodes the project path by replacing \ / : with '-'.
func claudeProjectsDir() string {
	if projectsDir != "" {
		return projectsDir
	}
	home, _ := os.UserHomeDir()
	enc := strings.NewReplacer(`\`, "-", "/", "-", ":", "-").Replace(workDir)
	return filepath.Join(home, ".claude", "projects", enc)
}

// discoverSessions lists on-disk sessions, most-recent first, up to limit.
func discoverSessions(limit int) []discovered {
	entries, err := os.ReadDir(claudeProjectsDir())
	if err != nil {
		return nil
	}
	var out []discovered
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".jsonl")
		label := sessionLabel(filepath.Join(claudeProjectsDir(), e.Name()))
		if label == "" {
			label = id[:8]
		}
		out = append(out, discovered{ID: id, Label: label, Mod: info.ModTime()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Mod.After(out[j].Mod) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// sessionLabel reads the first real user message from a session file for a label.
func sessionLabel(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	for sc.Scan() {
		var line struct {
			Type    string `json:"type"`
			Message struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(sc.Bytes(), &line) != nil || line.Type != "user" {
			continue
		}
		// content is either a plain string or an array of typed blocks
		var s string
		if json.Unmarshal(line.Message.Content, &s) == nil {
			if t := cleanLabel(s); t != "" {
				return t
			}
			continue
		}
		var blocks []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if json.Unmarshal(line.Message.Content, &blocks) == nil {
			for _, b := range blocks {
				if b.Type == "text" {
					if t := cleanLabel(b.Text); t != "" {
						return t
					}
				}
			}
		}
	}
	return ""
}

// cleanLabel trims a message to a short button label, skipping IDE/system blocks.
func cleanLabel(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || strings.HasPrefix(s, "<") { // skip <ide_opened_file> etc.
		return ""
	}
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > 40 {
		s = s[:40] + "…"
	}
	return s
}

const help = `🧠 SESSIONS
/sessions          list + switch (tap; ↩ = resume an existing Claude session)
/new <name>        create a session and switch to it
/switch <name>     switch active session
/rename <name>     rename the active session
/delete <name>     delete a session
/cd <dir>          set the active session's working directory

⚙️ RUN SETTINGS (per-session)
/status            show model / mode / effort
/model             pick model (opus, sonnet, haiku, fable)
/mode              pick permission mode (default, acceptEdits, plan, bypassPermissions, dontAsk, auto)
/effort            pick effort (low, medium, high, xhigh, max)
/stop              interrupt the running task (or tap ⏹ Interrupt)
/usage             Claude token + cost usage (this session + lifetime)

📦 CODE
/diff              git status + diff of the active session's repo
/get <path>        send a file from the PC to your phone

📸 CAMERA / SCREENSHOTS
/shot [url] [WxH]  screenshot a URL (headless browser); no url = grab the PC screen
/camshot [dev]     grab a still from the webcam (default = camera 0; dev = index or name)
(you can also just ask me to "screenshot the app")

🎙 VOICE
/voice             toggle the spoken completion voice note
/voicemode         switch voice notes between PLAY-on-PC and TRANSCRIBE-to-task
/playvoice         toggle playing voice notes on the PC speakers
/testask           demo an options question with tappable buttons

🖥 MACHINE
/health            CPU / RAM / disk / battery / uptime
/lock              lock the PC (immediate)
/hibernate         hibernate the PC (confirm)
/shutdown          shut down the PC (confirm)
/restart           restart the PC (confirm)
/autostart on|off  run the bridge automatically at logon

/help              this message

Any other text is sent to the ACTIVE session.`

// ---------------------------------------------------------------------------
// claude driver
// ---------------------------------------------------------------------------

type claudeEvent struct {
	Type      string          `json:"type"` // system | assistant | user | result
	SessionID string          `json:"session_id"`
	Result    string          `json:"result"`  // final text (type=result)
	Message   json.RawMessage `json:"message"` // assistant/user content
	// usage/cost fields, present on the final type=result event
	TotalCostUSD float64 `json:"total_cost_usd"`
	NumTurns     int     `json:"num_turns"`
	Usage        struct {
		InputTokens              int64 `json:"input_tokens"`
		OutputTokens             int64 `json:"output_tokens"`
		CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
		CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
	} `json:"usage"`
}

// recordUsage folds a result event's cost/token counts into the session and the
// lifetime totals (see /usage). Safe to call with a nil session.
func recordUsage(s *Session, ev claudeEvent) {
	in := ev.Usage.InputTokens + ev.Usage.CacheReadInputTokens + ev.Usage.CacheCreationInputTokens
	mu.Lock()
	if s != nil {
		s.CostUSD += ev.TotalCostUSD
		s.InTok += in
		s.OutTok += ev.Usage.OutputTokens
		s.Turns += ev.NumTurns
	}
	state.TotalCostUSD += ev.TotalCostUSD
	state.TotalInTok += in
	state.TotalOutTok += ev.Usage.OutputTokens
	mu.Unlock()
	save()
}

type assistantMsg struct {
	Content []struct {
		Type string `json:"type"` // text | tool_use
		Name string `json:"name"` // tool name (tool_use)
	} `json:"content"`
}

func runClaude(chat int64, prompt string) {
	mu.Lock()
	s := activeSession(chat)
	var prevID, dir, model, effort, permMode, label string
	if s != nil {
		prevID, dir, model, effort, permMode, label = s.ID, s.WorkDir, s.Model, s.Effort, s.PermMode, s.Label
	}
	mu.Unlock()
	if s == nil {
		return
	}
	if dir == "" {
		dir = workDir
	}
	if permMode == "" {
		permMode = "acceptEdits"
	}
	go voiceAnnounceStart(chat, label) // task-start voice (async, don't delay the run)

	args := []string{
		"-p",
		"--output-format", "stream-json",
		"--verbose", // required for stream-json
		"--permission-mode", permMode,
	}
	if sysPrompt != "" {
		args = append(args, "--append-system-prompt", sysPrompt)
	}
	if model != "" {
		args = append(args, "--model", model)
	}
	if effort != "" {
		args = append(args, "--effort", effort)
	}
	for _, t := range allowedTools {
		args = append(args, "--allowedTools", t)
	}
	if prevID != "" {
		args = append(args, "--resume", prevID)
	}

	// LookPath resolves claude.cmd on Windows / claude on unix; exec it directly.
	bin, err := exec.LookPath("claude")
	if err != nil {
		send(chat, "⚠️ claude CLI not found on PATH: "+err.Error())
		return
	}
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(prompt) // pass user text via stdin (no quoting/injection)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		send(chat, "⚠️ "+err.Error())
		return
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		send(chat, "⚠️ failed to start claude: "+err.Error())
		return
	}
	setRunning(chat, cmd, nil) // one-shot mode: no live stdin to interrupt over
	typingDone := make(chan struct{})
	go typingLoop(chat, typingDone)

	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 1<<20), 8<<20) // JSON lines can be large
	var newID string
	var res claudeEvent
	var fullText string

	for sc.Scan() {
		var ev claudeEvent
		if json.Unmarshal(sc.Bytes(), &ev) != nil {
			continue
		}
		if ev.SessionID != "" {
			newID = ev.SessionID
		}
		switch ev.Type {
		case "assistant":
			var m assistantMsg
			json.Unmarshal(ev.Message, &m)
			for _, c := range m.Content {
				if c.Type == "tool_use" {
					send(chat, "🔧 "+c.Name) // tool-call visibility
				}
			}
		case "result":
			res = ev
			fullText = ev.Result
			send(chat, ev.Result) // final answer
		}
	}
	cmd.Wait()
	close(typingDone)
	clearRunning(chat)
	recordUsage(s, res)

	if newID != "" {
		mu.Lock()
		s.ID = newID
		s.LastUsed = time.Now().Format(time.RFC3339)
		mu.Unlock()
		save()
	}
	maybeAskButtons(chat, fullText)
	if stderr.Len() > 0 {
		send(chat, "stderr: "+stderr.String())
	}

	// deliver any files the agent dropped in the outbox (screenshots, etc.)
	flushOutbox(chat)

	// spoken completion notification (PC speakers by default; see voice_target)
	voiceAnnounce(chat, label)
}

// ---------------------------------------------------------------------------
// streaming mode (EXPERIMENTAL bidirectional control protocol)
// ---------------------------------------------------------------------------

// ctrlRequest is a control message the CLI sends us (e.g. permission prompts).
type ctrlRequest struct {
	Type      string `json:"type"`
	RequestID string `json:"request_id"`
	Request   struct {
		Subtype  string          `json:"subtype"`
		ToolName string          `json:"tool_name"`
		Input    json.RawMessage `json:"input"`
	} `json:"request"`
}

// streamEvent carries incremental output when --include-partial-messages is on.
type streamEvent struct {
	Event struct {
		Type  string `json:"type"`
		Delta struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"delta"`
		ContentBlock struct {
			Type string `json:"type"`
			Name string `json:"name"`
		} `json:"content_block"`
	} `json:"event"`
}

// toolSummary renders a short human description of a tool call for the prompt.
func toolSummary(name string, input json.RawMessage) string {
	var m map[string]any
	json.Unmarshal(input, &m)
	switch name {
	case "Bash":
		if c, ok := m["command"].(string); ok {
			return "Bash: " + c
		}
	case "Edit", "Write", "Read":
		if p, ok := m["file_path"].(string); ok {
			return name + ": " + p
		}
	}
	return name
}

// writeControlResponse answers a can_use_tool control request over stdin.
func writeControlResponse(w io.Writer, cr ctrlRequest, allow bool) {
	inner := map[string]any{}
	if allow {
		var input any
		json.Unmarshal(cr.Request.Input, &input)
		inner["behavior"] = "allow"
		inner["updatedInput"] = input
	} else {
		inner["behavior"] = "deny"
		inner["message"] = "Denied via Telegram"
	}
	msg := map[string]any{
		"type": "control_response",
		"response": map[string]any{
			"subtype":    "success",
			"request_id": cr.RequestID,
			"response":   inner,
		},
	}
	b, _ := json.Marshal(msg)
	w.Write(append(b, '\n'))
}

// runClaudeStream drives Claude in bidirectional streaming mode: live-updating
// text, 🔧 tool markers, and Allow/Deny permission buttons. EXPERIMENTAL — it
// relies on the CLI's internal control protocol; every raw control_request is
// logged to the console so the wire format can be corrected during testing.
func runClaudeStream(chat int64, prompt string) {
	mu.Lock()
	s := activeSession(chat)
	var prevID, dir, model, effort, permMode, label string
	if s != nil {
		prevID, dir, model, effort, permMode, label = s.ID, s.WorkDir, s.Model, s.Effort, s.PermMode, s.Label
	}
	mu.Unlock()
	if s == nil {
		return
	}
	if dir == "" {
		dir = workDir
	}
	if permMode == "" {
		permMode = "default" // "default" => tools prompt, so you get Allow/Deny buttons
	}
	go voiceAnnounceStart(chat, label) // task-start voice (async, don't delay the run)

	args := []string{
		"--print",
		"--input-format", "stream-json",
		"--output-format", "stream-json",
		"--verbose",
		"--include-partial-messages",
		"--permission-mode", permMode,
	}
	if sysPrompt != "" {
		args = append(args, "--append-system-prompt", sysPrompt)
	}
	if model != "" {
		args = append(args, "--model", model)
	}
	if effort != "" {
		args = append(args, "--effort", effort)
	}
	for _, t := range allowedTools {
		args = append(args, "--allowedTools", t)
	}
	if prevID != "" {
		args = append(args, "--resume", prevID)
	}

	bin, err := exec.LookPath("claude")
	if err != nil {
		send(chat, "⚠️ claude CLI not found on PATH: "+err.Error())
		return
	}
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	stdin, err := cmd.StdinPipe()
	if err != nil {
		send(chat, "⚠️ "+err.Error())
		return
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		send(chat, "⚠️ "+err.Error())
		return
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		send(chat, "⚠️ failed to start claude: "+err.Error())
		return
	}
	setRunning(chat, cmd, stdin) // stream mode: stdin stays open for graceful interrupt
	typingDone := make(chan struct{})
	go typingLoop(chat, typingDone)

	// send the user's message as the first stream-json input line
	um := map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": prompt}}
	umb, _ := json.Marshal(um)
	stdin.Write(append(umb, '\n'))

	// live-message state (one growing Telegram bubble per assistant text block)
	var live string
	var liveID int64
	var lastEdit time.Time
	var streamedAny bool
	var fullText strings.Builder // whole assistant transcript, scanned for ASK: at the end
	flush := func() {
		t := live
		if strings.TrimSpace(t) == "" {
			return
		}
		if len(t) > 3900 {
			t = t[:3900]
		}
		if liveID == 0 {
			if id, e := sendReturningID(chat, t); e == nil {
				liveID = id
			}
		} else {
			editMessage(chat, liveID, t)
		}
		lastEdit = time.Now()
		streamedAny = true
	}
	addText := func(s string) {
		fullText.WriteString(s)
		live += s
		if len(live) > 3900 { // overflow: finalize this bubble, start a new one
			head := live[:3900]
			if liveID == 0 {
				if id, e := sendReturningID(chat, head); e == nil {
					liveID = id
				}
			} else {
				editMessage(chat, liveID, head)
			}
			live = live[3900:]
			liveID = 0
			lastEdit = time.Now()
			streamedAny = true
			return
		}
		if time.Since(lastEdit) > 1200*time.Millisecond {
			flush()
		}
	}
	finalizeBlock := func() {
		flush()
		live = ""
		liveID = 0
	}

	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	var newID string
	var res claudeEvent

loop:
	for sc.Scan() {
		line := sc.Bytes()
		var head struct {
			Type      string `json:"type"`
			SessionID string `json:"session_id"`
		}
		if json.Unmarshal(line, &head) != nil {
			continue
		}
		if head.SessionID != "" {
			newID = head.SessionID
		}
		switch head.Type {
		case "control_request":
			var cr ctrlRequest
			json.Unmarshal(line, &cr)
			fmt.Println("control_request:", string(line)) // observe real shape
			if cr.Request.Subtype != "can_use_tool" {
				continue // other control subtypes ignored for now
			}
			finalizeBlock()
			ch := make(chan bool, 1)
			pending.Lock()
			pending.m[cr.RequestID] = ch
			pending.Unlock()
			sendKeyboard(chat, "Allow tool?\n"+toolSummary(cr.Request.ToolName, cr.Request.Input),
				[][]map[string]string{{
					{"text": "✅ Allow", "callback_data": "perm:allow:" + cr.RequestID},
					{"text": "⛔ Deny", "callback_data": "perm:deny:" + cr.RequestID},
				}})
			allow := false
			select {
			case allow = <-ch:
			case <-time.After(5 * time.Minute):
				pending.Lock()
				delete(pending.m, cr.RequestID)
				pending.Unlock()
			}
			writeControlResponse(stdin, cr, allow)

		case "stream_event":
			var se streamEvent
			json.Unmarshal(line, &se)
			switch se.Event.Type {
			case "content_block_start":
				if se.Event.ContentBlock.Type == "tool_use" {
					finalizeBlock()
					send(chat, "🔧 "+se.Event.ContentBlock.Name)
				}
			case "content_block_delta":
				if se.Event.Delta.Type == "text_delta" {
					addText(se.Event.Delta.Text)
				}
			case "content_block_stop":
				finalizeBlock()
			}

		case "result":
			finalizeBlock()
			json.Unmarshal(line, &res)
			if !streamedAny {
				send(chat, res.Result)
				fullText.WriteString(res.Result)
			}
			break loop
		}
	}
	stdin.Close()
	cmd.Wait()
	close(typingDone)
	clearRunning(chat)
	recordUsage(s, res)

	if newID != "" {
		mu.Lock()
		s.ID = newID
		s.LastUsed = time.Now().Format(time.RFC3339)
		mu.Unlock()
		save()
	}
	if stderr.Len() > 0 {
		send(chat, "stderr: "+stderr.String())
	}
	flushOutbox(chat)
	maybeAskButtons(chat, fullText.String())
	voiceAnnounce(chat, label)
}

// ---------------------------------------------------------------------------
// spoken completion notification
// ---------------------------------------------------------------------------

// voiceFilter turns the plain TTS into a clear, high-pitched robotic voice:
// pitch up ~25% (asetrate trick, same speaking speed via atempo), highpass to
// drop low rumble + treble boost for clarity, light tremolo for the robot edge.
const voiceFilter = "aresample=22050,asetrate=27563,aresample=44100,atempo=0.80,highpass=f=180,treble=g=5,tremolo=f=22:d=0.3"

// voiceAnnounce / voiceAnnounceStart speak the task end / start messages.
func voiceAnnounce(chat int64, label string) {
	voiceSpeak(chat, fmt.Sprintf("Session %s has completed its task.", label))
}
func voiceAnnounceStart(chat int64, label string) {
	voiceSpeak(chat, fmt.Sprintf("Session %s started a task.", label))
}

// voiceSpeak speaks a phrase in a female robotic voice. Depending on voiceTarget
// it plays on the PC speakers ("pc"), sends a Telegram voice note ("phone"), or
// both. Best-effort: any failure is logged and skipped.
func voiceSpeak(chat int64, phrase string) {
	if !voiceNotify {
		return
	}
	toPC := voiceTarget == "pc" || voiceTarget == "both"
	toPhone := voiceTarget == "phone" || voiceTarget == "both"

	tmp, err := os.MkdirTemp("", "tgvoice")
	if err != nil {
		return
	}
	defer os.RemoveAll(tmp)

	raw := filepath.Join(tmp, "raw.wav")
	if err := speak(phrase, raw); err != nil {
		fmt.Println("tts error:", err)
		return
	}

	ff := ffmpegPath
	if ff == "" {
		if p, e := exec.LookPath("ffmpeg"); e == nil {
			ff = p
		}
	}
	// Without ffmpeg we can still play the plain (non-robotic) voice locally,
	// but can't build the effect or the Telegram-compatible ogg.
	if ff == "" {
		fmt.Println("ffmpeg not found (set ffmpeg_path in config); playing plain voice")
		if toPC {
			playLocal(raw)
		}
		return
	}

	filtered := filepath.Join(tmp, "voice.wav")
	if out, e := exec.Command(ff, "-y", "-i", raw, "-af", voiceFilter, filtered).CombinedOutput(); e != nil {
		fmt.Println("ffmpeg filter error:", e, string(out))
		return
	}

	if toPC {
		if e := playLocal(filtered); e != nil {
			fmt.Println("local playback error:", e)
		}
	}
	if toPhone {
		ogg := filepath.Join(tmp, "voice.ogg")
		if out, e := exec.Command(ff, "-y", "-i", filtered, "-c:a", "libopus", "-b:a", "64k", ogg).CombinedOutput(); e != nil {
			fmt.Println("ffmpeg ogg error:", e, string(out))
			return
		}
		if e := sendVoice(chat, ogg); e != nil {
			fmt.Println("sendVoice error:", e)
		}
	}
}

// playLocal plays a WAV file through the PC's default audio device (blocks until
// finished). SoundPlayer only handles WAV, hence the filtered output stays WAV.
func playLocal(wavPath string) error {
	ps := fmt.Sprintf(`(New-Object System.Media.SoundPlayer '%s').PlaySync()`, wavPath)
	return exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", ps).Run()
}

// speak synthesizes text to a WAV via the Windows SAPI female voice.
func speak(text, outWav string) error {
	esc := strings.ReplaceAll(text, "'", "''") // escape for PowerShell single-quoted string
	ps := fmt.Sprintf(`Add-Type -AssemblyName System.Speech;`+
		`$s = New-Object System.Speech.Synthesis.SpeechSynthesizer;`+
		`try { $s.SelectVoiceByHints([System.Speech.Synthesis.VoiceGender]::Female) } catch {};`+
		`$s.SetOutputToWaveFile('%s'); $s.Speak('%s'); $s.Dispose()`, outWav, esc)
	return exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", ps).Run()
}

// uploadFile posts a file to a Telegram send* endpoint via multipart form.
func uploadFile(chat int64, endpoint, field, path, caption string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	w.WriteField("chat_id", strconv.FormatInt(chat, 10))
	if caption != "" {
		w.WriteField("caption", caption)
	}
	fw, err := w.CreateFormFile(field, filepath.Base(path))
	if err != nil {
		return err
	}
	if _, err := io.Copy(fw, f); err != nil {
		return err
	}
	w.Close()

	req, err := http.NewRequest("POST", api()+endpoint, &buf)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

func sendVoice(chat int64, oggPath string) error {
	return uploadFile(chat, "/sendVoice", "voice", oggPath, "")
}
func sendPhoto(chat int64, path, caption string) error {
	return uploadFile(chat, "/sendPhoto", "photo", path, caption)
}
func sendDocument(chat int64, path, caption string) error {
	return uploadFile(chat, "/sendDocument", "document", path, caption)
}

// ---------------------------------------------------------------------------
// screenshots + outbox delivery
// ---------------------------------------------------------------------------

// findChrome locates a Chrome/Edge binary for headless screenshots.
func findChrome() string {
	for _, p := range []string{
		`C:\Program Files\Google\Chrome\Application\chrome.exe`,
		`C:\Program Files (x86)\Google\Chrome\Application\chrome.exe`,
		`C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`,
		`C:\Program Files\Microsoft\Edge\Application\msedge.exe`,
	} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if p, err := exec.LookPath("chrome"); err == nil {
		return p
	}
	return ""
}

func isImage(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".png", ".jpg", ".jpeg", ".webp", ".gif":
		return true
	}
	return false
}

// flushOutbox sends every file currently in the outbox to the chat, then removes
// it. Images go as photos, everything else as documents.
func flushOutbox(chat int64) {
	entries, err := os.ReadDir(outboxDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		path := filepath.Join(outboxDir, e.Name())
		var sendErr error
		if isImage(e.Name()) {
			sendErr = sendPhoto(chat, path, e.Name())
		} else {
			sendErr = sendDocument(chat, path, e.Name())
		}
		if sendErr != nil {
			fmt.Println("outbox send error:", sendErr)
			continue
		}
		os.Remove(path) // delivered
	}
}

// captureScreenshot renders url to pngOut with headless Chrome. size is "WxH".
// A throwaway --user-data-dir forces a fresh instance; otherwise an already-open
// Chrome swallows the invocation and no screenshot is written.
func captureScreenshot(url, size, pngOut string) error {
	if chromePath == "" {
		return fmt.Errorf("no chrome/edge found (set chrome_path in config)")
	}
	udd := chromeUserDir // persistent profile (keeps logins) if configured
	if udd == "" {
		tmp, err := os.MkdirTemp("", "tgchrome")
		if err != nil {
			return err
		}
		defer os.RemoveAll(tmp)
		udd = tmp
	}

	win := "--window-size=" + strings.ReplaceAll(size, "x", ",")
	cmd := exec.Command(chromePath,
		"--headless=new", "--disable-gpu", "--hide-scrollbars",
		"--no-first-run", "--no-default-browser-check",
		"--user-data-dir="+udd,
		win, "--screenshot="+pngOut, "--virtual-time-budget=8000", url)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
	}
	if _, err := os.Stat(pngOut); err != nil {
		return fmt.Errorf("chrome wrote no image: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// captureScreen grabs the full virtual desktop (all monitors) to pngOut via .NET.
// SetProcessDPIAware first so VirtualScreen reports physical (not DPI-scaled)
// pixels — otherwise scaled displays capture only a fraction of the screen.
func captureScreen(pngOut string) error {
	ps := fmt.Sprintf(`Add-Type -Name D -Namespace W -MemberDefinition '[DllImport("user32.dll")] public static extern bool SetProcessDPIAware();';`+
		`[W.D]::SetProcessDPIAware() | Out-Null;`+
		`Add-Type -AssemblyName System.Windows.Forms,System.Drawing;`+
		`$b=[System.Windows.Forms.SystemInformation]::VirtualScreen;`+
		`$bmp=New-Object System.Drawing.Bitmap $b.Width,$b.Height;`+
		`$g=[System.Drawing.Graphics]::FromImage($bmp);`+
		`$g.CopyFromScreen($b.Location,[System.Drawing.Point]::Empty,$b.Size);`+
		`$bmp.Save('%s',[System.Drawing.Imaging.ImageFormat]::Png);`+
		`$g.Dispose();$bmp.Dispose()`, pngOut)
	out, err := exec.Command("powershell", "-NoProfile", "-Command", ps).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// handleShot implements /shot [<url> [WxH]]: with a URL, screenshot the page via
// headless Chrome; with no URL, grab the PC's monitor. Then send the image.
func handleShot(chat int64, arg string) {
	parts := strings.Fields(arg)
	tmp, err := os.MkdirTemp("", "tgshot")
	if err != nil {
		return
	}
	defer os.RemoveAll(tmp)
	out := filepath.Join(tmp, "shot.png")

	if len(parts) == 0 {
		// no URL → capture the live desktop
		if err := captureScreen(out); err != nil {
			send(chat, "screen capture failed: "+err.Error())
			return
		}
		if err := sendPhoto(chat, out, "screen"); err != nil {
			send(chat, "send failed: "+err.Error())
		}
		return
	}

	url := parts[0]
	size := "1440x900"
	if len(parts) > 1 {
		size = parts[1]
	}
	if err := captureScreenshot(url, size, out); err != nil {
		send(chat, "screenshot failed: "+err.Error())
		return
	}
	if err := sendPhoto(chat, out, url); err != nil {
		send(chat, "send failed: "+err.Error())
	}
}

// ---------------------------------------------------------------------------
// remote power control
// ---------------------------------------------------------------------------

// confirmSys asks for confirmation before a destructive power action.
func confirmSys(chat int64, action, label string) {
	sendKeyboard(chat, "Confirm "+label+"?", [][]map[string]string{{
		{"text": "✅ Yes", "callback_data": "sys:" + action},
		{"text": "✖ Cancel", "callback_data": "sys:cancel"},
	}})
}

// systemAction performs a Windows power action. The message is sent first because
// the action may terminate this process immediately.
func systemAction(chat int64, action string) {
	msg := map[string]string{
		"hibernate": "😴 hibernating…",
		"shutdown":  "⏻ shutting down…",
		"restart":   "🔄 restarting…",
		"lock":      "🔒 locked",
	}[action]
	if msg == "" {
		return
	}
	send(chat, msg)

	switch action {
	case "hibernate":
		exec.Command("shutdown", "/h").Start()
	case "shutdown":
		exec.Command("shutdown", "/s", "/t", "0").Start()
	case "restart":
		exec.Command("shutdown", "/r", "/t", "0").Start()
	case "lock":
		exec.Command("rundll32.exe", "user32.dll,LockWorkStation").Start()
	}
}

// ---------------------------------------------------------------------------
// incoming voice playback (phone → PC speakers)
// ---------------------------------------------------------------------------

func resolveFFmpeg() string {
	if ffmpegPath != "" {
		return ffmpegPath
	}
	if p, err := exec.LookPath("ffmpeg"); err == nil {
		return p
	}
	return ""
}

// tgFilePath resolves a Telegram file_id to its downloadable path.
func tgFilePath(fileID string) (string, error) {
	resp, err := httpClient.Get(api() + "/getFile?file_id=" + url.QueryEscape(fileID))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var out struct {
		OK     bool `json:"ok"`
		Result struct {
			FilePath string `json:"file_path"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if !out.OK {
		return "", fmt.Errorf("getFile failed")
	}
	return out.Result.FilePath, nil
}

func downloadFile(u, dest string) error {
	resp, err := httpClient.Get(u)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, resp.Body)
	return err
}

// playIncomingVoice downloads a voice note (OGG/Opus) and plays it on the PC
// speakers (converted to WAV via ffmpeg, since SoundPlayer only handles WAV).
func playIncomingVoice(chat int64, fileID string) {
	fp, err := tgFilePath(fileID)
	if err != nil {
		send(chat, "voice fetch failed: "+err.Error())
		return
	}
	tmp, err := os.MkdirTemp("", "tgin")
	if err != nil {
		return
	}
	defer os.RemoveAll(tmp)

	src := filepath.Join(tmp, "in.oga")
	if err := downloadFile("https://api.telegram.org/file/bot"+botToken+"/"+fp, src); err != nil {
		send(chat, "voice download failed: "+err.Error())
		return
	}
	ff := resolveFFmpeg()
	if ff == "" {
		send(chat, "ffmpeg needed to play voice (set ffmpeg_path)")
		return
	}
	wav := filepath.Join(tmp, "in.wav")
	if out, err := exec.Command(ff, "-y", "-i", src, wav).CombinedOutput(); err != nil {
		fmt.Println("voice convert error:", err, string(out))
		send(chat, "voice convert failed")
		return
	}
	if err := playLocal(wav); err != nil {
		send(chat, "playback failed: "+err.Error())
	}
}

// ---------------------------------------------------------------------------
// running-task registry, typing indicator & conveniences
// ---------------------------------------------------------------------------

// runProc is the active claude process for a chat, plus (in stream mode) its
// open stdin so we can send a graceful interrupt over the control protocol.
type runProc struct {
	cmd   *exec.Cmd
	stdin io.Writer // nil in one-shot mode (runClaude) — only a hard kill is possible there
}

// running tracks the active claude process per chat so /stop can cancel it and
// a guard can prevent overlapping runs on the same session.
var running = struct {
	sync.Mutex
	m map[int64]*runProc
}{m: map[int64]*runProc{}}

func setRunning(chat int64, cmd *exec.Cmd, stdin io.Writer) {
	running.Lock()
	running.m[chat] = &runProc{cmd: cmd, stdin: stdin}
	running.Unlock()
}
func clearRunning(chat int64) { running.Lock(); delete(running.m, chat); running.Unlock() }
func isRunning(chat int64) bool {
	running.Lock()
	_, ok := running.m[chat]
	running.Unlock()
	return ok
}

// stopRunning interrupts the active task. In stream mode it sends a graceful
// interrupt over stdin (like pressing Esc), so the session stays clean and
// resumable; if the process ignores it, a hard kill follows after a short grace.
// In one-shot mode (no live stdin) it hard-kills immediately.
func stopRunning(chat int64) bool {
	running.Lock()
	rp := running.m[chat]
	running.Unlock()
	if rp == nil || rp.cmd == nil || rp.cmd.Process == nil {
		return false
	}
	kill := func() {
		exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(rp.cmd.Process.Pid)).Run()
	}
	if rp.stdin == nil {
		kill()
		return true
	}
	// graceful interrupt over the stream-json control channel
	req := map[string]any{
		"type":       "control_request",
		"request_id": fmt.Sprintf("int-%d", time.Now().UnixNano()),
		"request":    map[string]any{"subtype": "interrupt"},
	}
	if b, err := json.Marshal(req); err == nil {
		rp.stdin.Write(append(b, '\n'))
	}
	// fallback: if the same run is still active after the grace period, hard-kill
	go func() {
		time.Sleep(5 * time.Second)
		running.Lock()
		cur := running.m[chat]
		running.Unlock()
		if cur == rp && cur.cmd.Process != nil {
			kill()
		}
	}()
	return true
}

func sendAction(chat int64, action string) {
	httpClient.PostForm(api()+"/sendChatAction", url.Values{
		"chat_id": {strconv.FormatInt(chat, 10)},
		"action":  {action},
	})
}

// typingLoop keeps the "typing…" indicator alive until done is closed.
func typingLoop(chat int64, done chan struct{}) {
	for {
		sendAction(chat, "typing")
		select {
		case <-done:
			return
		case <-time.After(4 * time.Second):
		}
	}
}

// sessionDir returns the active session's working dir (or the default).
func sessionDir(chat int64) string {
	if s := activeSession(chat); s != nil && s.WorkDir != "" {
		return s.WorkDir
	}
	return workDir
}

// handleGet sends a file from the PC to the phone.
func handleGet(chat int64, path string) {
	if path == "" {
		send(chat, "usage: /get <path>")
		return
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(sessionDir(chat), path)
	}
	fi, err := os.Stat(path)
	if err != nil || fi.IsDir() {
		send(chat, "no such file: "+path)
		return
	}
	var e error
	if isImage(path) {
		e = sendPhoto(chat, path, filepath.Base(path))
	} else {
		e = sendDocument(chat, path, filepath.Base(path))
	}
	if e != nil {
		send(chat, "send failed: "+e.Error())
	}
}

// handleDiff shows git status + diff of the active session's repo.
func handleDiff(chat int64) {
	dir := sessionDir(chat)
	status, _ := exec.Command("git", "-C", dir, "status", "-s").CombinedOutput()
	if strings.TrimSpace(string(status)) == "" {
		send(chat, "📁 "+dir+"\nclean — no changes")
		return
	}
	stat, _ := exec.Command("git", "-C", dir, "diff", "--stat").CombinedOutput()
	send(chat, "📁 "+dir+"\n\n"+string(status)+"\n"+string(stat))
	if full, _ := exec.Command("git", "-C", dir, "diff").CombinedOutput(); strings.TrimSpace(string(full)) != "" {
		tmp, err := os.MkdirTemp("", "tgdiff")
		if err == nil {
			defer os.RemoveAll(tmp)
			p := filepath.Join(tmp, "diff.txt")
			if os.WriteFile(p, full, 0600) == nil {
				sendDocument(chat, p, "diff.txt")
			}
		}
	}
}

// sendHealth reports basic machine stats.
func sendHealth(chat int64) {
	ps := `$os=Get-CimInstance Win32_OperatingSystem;` +
		`$cpu=(Get-CimInstance Win32_Processor | Measure-Object -Property LoadPercentage -Average).Average;` +
		`$memTot=[math]::Round($os.TotalVisibleMemorySize/1MB,1);` +
		`$memFree=[math]::Round($os.FreePhysicalMemory/1MB,1);` +
		`$c=Get-CimInstance Win32_LogicalDisk -Filter "DeviceID='C:'";` +
		`$diskFree=[math]::Round($c.FreeSpace/1GB,1);$diskTot=[math]::Round($c.Size/1GB,1);` +
		`$bat=(Get-CimInstance Win32_Battery).EstimatedChargeRemaining;` +
		`$up=(Get-Date)-$os.LastBootUpTime;` +
		`"CPU: $cpu%";"RAM used: $($memTot-$memFree)/$memTot GB";"Disk C used: $($diskTot-$diskFree)/$diskTot GB";"Battery: $bat%";"Uptime: $([math]::Floor($up.TotalHours))h $($up.Minutes)m"`
	out, err := exec.Command("powershell", "-NoProfile", "-Command", ps).CombinedOutput()
	if err != nil {
		send(chat, "health failed: "+strings.TrimSpace(string(out)))
		return
	}
	send(chat, "🖥 Machine health\n"+strings.TrimSpace(string(out)))
}

// commafy formats an integer with thousands separators (e.g. 1234567 -> 1,234,567).
func commafy(n int64) string {
	s := strconv.FormatInt(n, 10)
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	var out []byte
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, c)
	}
	if neg {
		return "-" + string(out)
	}
	return string(out)
}

// sendUsage reports Claude token/cost usage as tracked by the bridge from each
// run's result event (dependency-free; counts only runs made through tgbridge).
func sendUsage(chat int64) {
	mu.Lock()
	totCost, totIn, totOut := state.TotalCostUSD, state.TotalInTok, state.TotalOutTok
	var b strings.Builder
	if s := activeSession(chat); s != nil {
		fmt.Fprintf(&b, "Active: %s\n  cost:   $%.4f\n  tokens: in %s / out %s\n  turns:  %d\n\n",
			s.Label, s.CostUSD, commafy(s.InTok), commafy(s.OutTok), s.Turns)
	}
	mu.Unlock()
	fmt.Fprintf(&b, "Lifetime (all runs):\n  cost:   $%.4f\n  tokens: in %s / out %s",
		totCost, commafy(totIn), commafy(totOut))
	send(chat, "📊 Claude usage (tracked by tgbridge)\n\n"+b.String()+
		"\n\nnote: counted from runs made through this bridge only.")
}

// maybeAskButtons scans the assistant's final text for a line of the form
//   ASK: <question> || option one || option two
// and, if found, presents the options as tappable buttons. Tapping one feeds
// the chosen text back into the active session (see the "qa:" callback). This is
// how the model asks the user a multiple-choice question from headless mode.
func maybeAskButtons(chat int64, text string) bool {
	for _, ln := range strings.Split(text, "\n") {
		ln = strings.TrimSpace(ln)
		rest, ok := strings.CutPrefix(ln, "ASK:")
		if !ok {
			continue
		}
		parts := strings.Split(rest, "||")
		question := strings.TrimSpace(parts[0])
		if question == "" {
			question = "Please choose:"
		}
		var rows [][]map[string]string
		for i, opt := range parts[1:] {
			opt = strings.TrimSpace(opt)
			if opt == "" {
				continue
			}
			tok := fmt.Sprintf("q%d_%d", time.Now().UnixNano(), i) // unique, well under Telegram's 64-byte cap
			asks.Lock()
			asks.m[tok] = opt
			asks.Unlock()
			rows = append(rows, []map[string]string{{"text": opt, "callback_data": "qa:" + tok}})
		}
		if len(rows) > 0 {
			sendKeyboard(chat, "❓ "+question, rows)
			return true
		}
	}
	return false
}

// answerAsk resolves a tapped ASK button back to its option text and runs it as
// the next prompt in the active session.
func answerAsk(chat int64, tok string) {
	asks.Lock()
	opt := asks.m[tok]
	delete(asks.m, tok)
	asks.Unlock()
	if opt == "" {
		send(chat, "that question has expired")
		return
	}
	send(chat, "➡️ "+opt)
	runPrompt(chat, opt)
}

// runPrompt dispatches user text to the active session (shared by typed messages
// and tapped ASK answers), guarding against overlapping runs.
func runPrompt(chat int64, text string) {
	if activeSession(chat) == nil {
		send(chat, "No active session. Use /new <name> or /sessions")
		return
	}
	if isRunning(chat) {
		send(chat, "⏳ a task is already running — tap ⏹ Interrupt or /stop")
		return
	}
	sendKeyboard(chat, "…working ["+state.Active[chat]+"]",
		[][]map[string]string{{{"text": "⏹ Interrupt", "callback_data": "stop"}}})
	if streamMode {
		go runClaudeStream(chat, text)
	} else {
		go runClaude(chat, text)
	}
}

// listCameras enumerates DirectShow video capture devices by their friendly name
// (ffmpeg prints the list to stderr; the -i dummy probe fails, which is expected).
func listCameras(ff string) []string {
	out, _ := exec.Command(ff, "-hide_banner", "-list_devices", "true", "-f", "dshow", "-i", "dummy").CombinedOutput()
	var devs []string
	for _, ln := range strings.Split(string(out), "\n") {
		if !strings.Contains(ln, "(video)") {
			continue
		}
		a := strings.Index(ln, "\"")
		if a < 0 {
			continue
		}
		b := strings.Index(ln[a+1:], "\"")
		if b < 0 {
			continue
		}
		devs = append(devs, ln[a+1:a+1+b])
	}
	return devs
}

// handleCamshot grabs a single still frame from a local webcam and sends it to
// the phone. arg may be empty (first/default camera), a device index, or a
// DirectShow device name.
func handleCamshot(chat int64, arg string) {
	ff := resolveFFmpeg()
	if ff == "" {
		send(chat, "ffmpeg not found (set ffmpeg_path in config)")
		return
	}
	dev := strings.TrimSpace(arg)
	if dev == "" && cameraDevice != "" {
		dev = cameraDevice // configured default name
	}
	if _, err := strconv.Atoi(dev); dev == "" || err == nil { // empty or an index → enumerate
		cams := listCameras(ff)
		if len(cams) == 0 {
			send(chat, "no webcam found (no DirectShow video device)")
			return
		}
		idx := 0
		if n, err := strconv.Atoi(dev); err == nil && n >= 0 && n < len(cams) {
			idx = n
		}
		dev = cams[idx]
	}
	send(chat, "📷 capturing from "+dev+" …")
	out := filepath.Join(os.TempDir(), fmt.Sprintf("camshot-%d.jpg", time.Now().Unix()))
	// -rtbufsize avoids dropped-frame warnings; grab a couple of frames so the
	// sensor auto-exposes, keeping the last one (the first frame is often dark).
	cmd := exec.Command(ff, "-hide_banner", "-f", "dshow", "-rtbufsize", "100M",
		"-i", "video="+dev, "-frames:v", "1", "-update", "1", "-q:v", "3", "-y", out)
	o, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(o))
		if len(msg) > 500 {
			msg = msg[len(msg)-500:]
		}
		send(chat, "camshot failed ["+dev+"]:\n"+msg)
		return
	}
	if err := sendPhoto(chat, out, "📷 "+dev); err != nil {
		send(chat, "send failed: "+err.Error())
	}
	os.Remove(out)
}

// transcribeAPI sends the audio to an OpenAI-compatible /audio/transcriptions
// endpoint (OpenAI Whisper or a local whisper server) and returns the text.
func transcribeAPI(wav string) (string, error) {
	f, err := os.Open(wav)
	if err != nil {
		return "", err
	}
	defer f.Close()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	w.WriteField("model", sttModel)
	fw, err := w.CreateFormFile("file", filepath.Base(wav))
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(fw, f); err != nil {
		return "", err
	}
	w.Close()

	req, err := http.NewRequest("POST", sttURL, &buf)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	if sttKey != "" {
		req.Header.Set("Authorization", "Bearer "+sttKey)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("stt %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	var out struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return "", fmt.Errorf("bad stt response: %s", strings.TrimSpace(string(b)))
	}
	return strings.TrimSpace(out.Text), nil
}

// transcribe converts a wav to text. Preferred engine first (external command,
// then STT API), each falling back to the next on error, and finally to the
// built-in Windows recognizer. So if nothing is configured — or a configured
// engine fails — it still works via the native recognizer.
func transcribe(wav string) (string, error) {
	if transcribeCmd != "" {
		f := strings.Fields(transcribeCmd)
		out, err := exec.Command(f[0], append(f[1:], wav)...).CombinedOutput()
		if err == nil {
			return strings.TrimSpace(string(out)), nil
		}
		fmt.Println("transcribe_cmd failed, falling back:", err, strings.TrimSpace(string(out)))
	}
	if sttURL != "" {
		if t, err := transcribeAPI(wav); err == nil {
			return t, nil
		} else {
			fmt.Println("stt api failed, falling back to native:", err)
		}
	}
	return transcribeWindows(wav)
}

// transcribeWindows uses the built-in Windows offline speech recognizer.
func transcribeWindows(wav string) (string, error) {
	ps := `Add-Type -AssemblyName System.Speech;` +
		`$r=New-Object System.Speech.Recognition.SpeechRecognitionEngine;` +
		`$r.LoadGrammar((New-Object System.Speech.Recognition.DictationGrammar));` +
		`$r.SetInputToWaveFile('` + wav + `');` +
		`$sb=New-Object System.Text.StringBuilder;` +
		`while($true){$res=$r.Recognize(); if($res -eq $null){break}; [void]$sb.Append($res.Text+' ')};` +
		`$sb.ToString()`
	out, err := exec.Command("powershell", "-NoProfile", "-Command", ps).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

// transcribeAndRun turns a voice note into a task for the active session.
func transcribeAndRun(chat int64, fileID string) {
	fp, err := tgFilePath(fileID)
	if err != nil {
		send(chat, "voice fetch failed: "+err.Error())
		return
	}
	tmp, err := os.MkdirTemp("", "tgtask")
	if err != nil {
		return
	}
	defer os.RemoveAll(tmp)
	src := filepath.Join(tmp, "in.oga")
	if err := downloadFile("https://api.telegram.org/file/bot"+botToken+"/"+fp, src); err != nil {
		send(chat, "voice download failed: "+err.Error())
		return
	}
	ff := resolveFFmpeg()
	if ff == "" {
		send(chat, "ffmpeg needed for transcription")
		return
	}
	wav := filepath.Join(tmp, "in.wav")
	// 16 kHz mono PCM is what recognizers expect; clean up the audio (band-limit
	// to speech range + loudness-normalize) to improve accuracy.
	if out, err := exec.Command(ff, "-y", "-i", src,
		"-ar", "16000", "-ac", "1",
		"-af", "highpass=f=100,lowpass=f=8000,dynaudnorm", wav).CombinedOutput(); err != nil {
		fmt.Println("transcribe convert error:", err, string(out))
		send(chat, "voice convert failed")
		return
	}
	text, err := transcribe(wav)
	if err != nil {
		send(chat, "transcription failed: "+err.Error())
		return
	}
	if text == "" {
		send(chat, "🎙 (couldn't make out any words)")
		return
	}
	send(chat, "🎙 heard: "+text)
	handleText(chat, text)
}

// handleAutostart registers/removes a logon task so the bridge auto-starts.
func handleAutostart(chat int64, arg string) {
	switch arg {
	case "on":
		exe, err := os.Executable()
		if err != nil {
			send(chat, "cannot resolve executable: "+err.Error())
			return
		}
		out, err := exec.Command("schtasks", "/create", "/tn", "tgbridge",
			"/tr", exe, "/sc", "onlogon", "/rl", "highest", "/f").CombinedOutput()
		if err != nil {
			send(chat, "autostart failed: "+strings.TrimSpace(string(out)))
			return
		}
		send(chat, "✅ autostart enabled (runs at logon, elevated)")
		low := strings.ToLower(exe)
		if strings.Contains(low, "temp") || strings.Contains(low, "go-build") {
			send(chat, "⚠️ you're running via `go run` (temporary exe). Build a stable one:\n  go build -o tgbridge.exe ./tools/tgbridge\nrun tgbridge.exe, then /autostart on")
		}
	case "off":
		exec.Command("schtasks", "/delete", "/tn", "tgbridge", "/f").Run()
		send(chat, "autostart disabled")
	default:
		send(chat, "usage: /autostart on|off")
	}
}

// ---------------------------------------------------------------------------
// dispatch
// ---------------------------------------------------------------------------

func mine(id int64) bool { return strconv.FormatInt(id, 10) == allowedID }

func handleText(chat int64, text string) {
	switch {
	case text == "/help" || text == "/start":
		send(chat, help)

	case text == "/sessions":
		showSessions(chat)

	case text == "/shot" || strings.HasPrefix(text, "/shot "):
		go handleShot(chat, strings.TrimSpace(strings.TrimPrefix(text, "/shot")))

	case text == "/hibernate":
		confirmSys(chat, "hibernate", "hibernate")
	case text == "/shutdown":
		confirmSys(chat, "shutdown", "shutdown")
	case text == "/restart":
		confirmSys(chat, "restart", "restart")
	case text == "/lock":
		systemAction(chat, "lock")

	case text == "/playvoice":
		playIncoming = !playIncoming
		if playIncoming {
			send(chat, "🔊 incoming voice playback: ON — send me a voice note")
		} else {
			send(chat, "🔇 incoming voice playback: OFF")
		}

	case text == "/voicemode":
		if voiceMode == "task" {
			voiceMode = "play"
			send(chat, "🔊 voice notes will PLAY on the PC")
		} else {
			voiceMode = "task"
			send(chat, "🎙 voice notes will be TRANSCRIBED into tasks for the active session")
		}

	case text == "/stop":
		if stopRunning(chat) {
			send(chat, "⏹ interrupting…")
		} else {
			send(chat, "nothing running")
		}

	case text == "/usage":
		go sendUsage(chat)

	case text == "/camshot" || strings.HasPrefix(text, "/camshot "):
		go handleCamshot(chat, strings.TrimSpace(strings.TrimPrefix(text, "/camshot")))

	case text == "/health":
		go sendHealth(chat)

	case text == "/diff":
		go handleDiff(chat)

	case strings.HasPrefix(text, "/get "):
		go handleGet(chat, strings.TrimSpace(text[len("/get "):]))

	case strings.HasPrefix(text, "/cd "):
		dir := strings.TrimSpace(text[len("/cd "):])
		s := activeSession(chat)
		if s == nil {
			send(chat, "no active session")
			return
		}
		if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
			send(chat, "no such directory: "+dir)
			return
		}
		mu.Lock()
		s.WorkDir = dir
		mu.Unlock()
		save()
		send(chat, "📁 workdir → "+dir)

	case text == "/autostart" || strings.HasPrefix(text, "/autostart "):
		handleAutostart(chat, strings.TrimSpace(strings.TrimPrefix(text, "/autostart")))

	case text == "/testask":
		sendKeyboard(chat, "Demo question — which do you prefer?", [][]map[string]string{
			{{"text": "🅰 Option A", "callback_data": "ask:Option A"}},
			{{"text": "🅱 Option B", "callback_data": "ask:Option B"}},
			{{"text": "🅲 Option C", "callback_data": "ask:Option C"}},
		})

	case text == "/voice":
		voiceNotify = !voiceNotify
		if voiceNotify {
			send(chat, "🔊 completion voice: ON")
		} else {
			send(chat, "🔇 completion voice: OFF")
		}

	case text == "/status":
		s := activeSession(chat)
		if s == nil {
			send(chat, "No active session. Use /new <name> or /sessions")
			return
		}
		send(chat, fmt.Sprintf("Session: %s\nmodel:  %s\nmode:   %s\neffort: %s",
			s.Label, orDefault(s.Model), def(s.PermMode, "acceptEdits"), orDefault(s.Effort)))

	case text == "/model":
		if s := activeSession(chat); s != nil {
			choiceKeyboard(chat, "Model (now: "+orDefault(s.Model)+")", "mdl:", s.Model, modelOpts)
		} else {
			send(chat, "No active session. Use /new <name> or /sessions")
		}

	case text == "/mode":
		if s := activeSession(chat); s != nil {
			choiceKeyboard(chat, "Permission mode (now: "+def(s.PermMode, "acceptEdits")+")", "mode:", def(s.PermMode, "acceptEdits"), modeOpts)
		} else {
			send(chat, "No active session. Use /new <name> or /sessions")
		}

	case text == "/effort":
		if s := activeSession(chat); s != nil {
			choiceKeyboard(chat, "Effort (now: "+orDefault(s.Effort)+")", "eff:", s.Effort, effortOpts)
		} else {
			send(chat, "No active session. Use /new <name> or /sessions")
		}

	case strings.HasPrefix(text, "/new "):
		label := strings.TrimSpace(text[len("/new "):])
		if label == "" {
			send(chat, "usage: /new <name>")
			return
		}
		if find(label) != nil {
			send(chat, "already exists")
			return
		}
		mu.Lock()
		state.Sessions = append(state.Sessions, &Session{Label: label, WorkDir: workDir})
		state.Active[chat] = label
		mu.Unlock()
		save()
		send(chat, "🆕 created + switched → "+label)

	case strings.HasPrefix(text, "/switch "):
		label := strings.TrimSpace(text[len("/switch "):])
		if find(label) == nil {
			send(chat, "no such session")
			return
		}
		mu.Lock()
		state.Active[chat] = label
		mu.Unlock()
		save()
		send(chat, "➡️ "+label)

	case strings.HasPrefix(text, "/rename "):
		label := strings.TrimSpace(text[len("/rename "):])
		s := activeSession(chat)
		if s == nil {
			send(chat, "no active session")
			return
		}
		if label == "" || find(label) != nil {
			send(chat, "bad or duplicate name")
			return
		}
		mu.Lock()
		old := s.Label
		s.Label = label
		state.Active[chat] = label
		mu.Unlock()
		save()
		send(chat, "✏️ "+old+" → "+label)

	case strings.HasPrefix(text, "/delete "):
		label := strings.TrimSpace(text[len("/delete "):])
		mu.Lock()
		for i, s := range state.Sessions {
			if s.Label == label {
				state.Sessions = append(state.Sessions[:i], state.Sessions[i+1:]...)
				break
			}
		}
		if state.Active[chat] == label {
			delete(state.Active, chat)
		}
		mu.Unlock()
		save()
		send(chat, "🗑 "+label)

	case strings.HasPrefix(text, "/"):
		send(chat, "unknown command\n\n"+help)

	default:
		runPrompt(chat, text)
	}
}

func handleCallback(u *tgUpdate) {
	cq := u.CallbackQuery
	answerCallback(cq.ID)
	chat := cq.Message.Chat.ID
	if cq.Data == "stop" {
		if stopRunning(chat) {
			send(chat, "⏹ interrupting…")
		} else {
			send(chat, "nothing running")
		}
		return
	}
	if tok, ok := strings.CutPrefix(cq.Data, "qa:"); ok {
		answerAsk(chat, tok)
		return
	}
	if idxStr, ok := strings.CutPrefix(cq.Data, "sw:"); ok {
		if idx, err := strconv.Atoi(idxStr); err == nil && idx < len(state.Sessions) {
			mu.Lock()
			label := state.Sessions[idx].Label
			state.Active[chat] = label
			mu.Unlock()
			save()
			send(chat, "➡️ "+label)
		}
		return
	}
	if id, ok := strings.CutPrefix(cq.Data, "res:"); ok {
		adoptSession(chat, id)
		return
	}
	if v, ok := strings.CutPrefix(cq.Data, "mdl:"); ok {
		setSetting(chat, "model", v)
		return
	}
	if v, ok := strings.CutPrefix(cq.Data, "mode:"); ok {
		setSetting(chat, "mode", v)
		return
	}
	if v, ok := strings.CutPrefix(cq.Data, "eff:"); ok {
		setSetting(chat, "effort", v)
		return
	}
	if data, ok := strings.CutPrefix(cq.Data, "perm:"); ok {
		var allow bool
		var reqID string
		if r, ok := strings.CutPrefix(data, "allow:"); ok {
			allow, reqID = true, r
		} else if r, ok := strings.CutPrefix(data, "deny:"); ok {
			allow, reqID = false, r
		}
		pending.Lock()
		ch := pending.m[reqID]
		delete(pending.m, reqID)
		pending.Unlock()
		if ch != nil {
			ch <- allow
		}
		if allow {
			send(chat, "✅ allowed")
		} else {
			send(chat, "⛔ denied")
		}
		return
	}
	if v, ok := strings.CutPrefix(cq.Data, "ask:"); ok {
		send(chat, "You chose: "+v)
		return
	}
	if a, ok := strings.CutPrefix(cq.Data, "sys:"); ok {
		if a == "cancel" {
			send(chat, "cancelled")
			return
		}
		systemAction(chat, a)
	}
}

// adoptSession pulls an existing on-disk Claude session into the registry (if
// not already there) and makes it active, so it resumes on the next message.
func adoptSession(chat int64, id string) {
	for _, s := range state.Sessions {
		if s.ID == id {
			mu.Lock()
			state.Active[chat] = s.Label
			mu.Unlock()
			save()
			send(chat, "↩ resumed → "+s.Label)
			return
		}
	}
	label := sessionLabel(filepath.Join(claudeProjectsDir(), id+".jsonl"))
	if label == "" {
		label = id[:8]
	}
	base := label
	for i := 1; find(label) != nil; i++ { // avoid duplicate labels (active is keyed by label)
		label = base + " #" + strconv.Itoa(i)
	}
	mu.Lock()
	state.Sessions = append(state.Sessions, &Session{Label: label, ID: id, WorkDir: workDir})
	state.Active[chat] = label
	mu.Unlock()
	save()
	send(chat, "↩ resumed → "+label)
}

// srcDir returns the directory this source file lives in, so config.json can be
// found next to main.go regardless of the current working directory.
func srcDir() string {
	if _, file, _, ok := runtime.Caller(0); ok {
		return filepath.Dir(file)
	}
	return "."
}

// resolveConfig picks the first existing candidate path. If -config was given
// explicitly it is used as-is; otherwise we look next to main.go, then in a
// couple of common working directories.
func resolveConfig(flagVal string, explicit bool) string {
	if explicit {
		return flagVal
	}
	for _, p := range []string{
		filepath.Join(srcDir(), "config.json"), // next to main.go (most reliable)
		"config.json",                          // cwd = the tgbridge folder
		"tools/tgbridge/config.json",           // cwd = repo root
	} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return flagVal
}

func main() {
	cfgFlag := flag.String("config", "", "path to config json (default: config.json next to main.go)")
	flag.Parse()

	cfgPath := resolveConfig(*cfgFlag, *cfgFlag != "")
	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		cwd, _ := os.Getwd()
		fmt.Printf("cannot read config %q (cwd=%s): %v\n(copy config.json.example to config.json and fill it in)\n", cfgPath, cwd, err)
		os.Exit(1)
	}
	var cfg Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		fmt.Printf("invalid config %q: %v\n", cfgPath, err)
		os.Exit(1)
	}
	botToken, allowedID, workDir = cfg.BotToken, cfg.AllowedUserID, cfg.WorkDir
	projectsDir = cfg.ProjectsDir
	if cfg.VoiceNotify != nil {
		voiceNotify = *cfg.VoiceNotify
	}
	if cfg.VoiceTarget != "" {
		voiceTarget = cfg.VoiceTarget
	}
	if cfg.StreamMode != nil {
		streamMode = *cfg.StreamMode
	}
	if cfg.PlayIncoming != nil {
		playIncoming = *cfg.PlayIncoming
	}
	if cfg.VoiceInput != "" {
		voiceMode = cfg.VoiceInput
	}
	transcribeCmd = cfg.TranscribeCmd
	sttURL = cfg.STTApiURL
	sttKey = cfg.STTApiKey
	if cfg.STTModel != "" {
		sttModel = cfg.STTModel
	}
	// convenience: a key with no URL implies OpenAI's endpoint
	if sttKey != "" && sttURL == "" {
		sttURL = "https://api.openai.com/v1/audio/transcriptions"
	}
	chromeUserDir = cfg.ChromeUserDir
	cameraDevice = cfg.CameraDevice
	ffmpegPath = cfg.FFmpegPath
	if len(cfg.AllowedTools) > 0 {
		allowedTools = cfg.AllowedTools
	}
	if botToken == "" || allowedID == "" || workDir == "" {
		fmt.Println("config must set bot_token, allowed_user_id, and workdir")
		os.Exit(1)
	}
	// Anchor the state file next to config.json so sessions/settings persist
	// across restarts regardless of the current working directory.
	if abs, err := filepath.Abs(cfgPath); err == nil {
		file = filepath.Join(filepath.Dir(abs), "tgbridge_state.json")
	}

	// screenshot + outbox setup
	chromePath = cfg.ChromePath
	if chromePath == "" {
		chromePath = findChrome()
	}
	outboxDir = cfg.OutboxDir
	if outboxDir == "" {
		outboxDir = filepath.Join(filepath.Dir(file), "outbox")
	}
	os.MkdirAll(outboxDir, 0755)
	sysPrompt = fmt.Sprintf("You are being operated remotely through a Telegram bridge; the user is on their phone. "+
		"To send the user a file or image (e.g. a screenshot), save it into this directory: %s — "+
		"anything placed there is delivered to their phone and then removed. "+
		"When asked to screenshot the running app: start the app, wait until it is ready, capture the UI to a PNG in that directory "+
		"using headless Chrome, then STOP the app again afterwards (do not leave it running). Chrome is at: %s. "+
		"When you need the user to make a choice or answer a short question, end your reply with a single line formatted exactly as: "+
		"ASK: <your question> || <option 1> || <option 2> [|| <option 3> ...] — the bridge turns those options into tappable buttons on the "+
		"phone, and the user's tap is sent back to you as their next message. Use this instead of a long open-ended question whenever the "+
		"answer is one of a few choices. Keep each option short (a few words).",
		outboxDir, chromePath)

	load()
	fmt.Println("tgbridge running — state:", file)
	fmt.Println("outbox:", outboxDir, "| chrome:", chromePath)

	var offset int64
	for {
		updates, err := getUpdates(offset)
		if err != nil {
			fmt.Println("getUpdates error:", err)
			time.Sleep(3 * time.Second)
			continue
		}
		for i := range updates {
			u := &updates[i]
			offset = u.UpdateID + 1

			if u.CallbackQuery != nil {
				if mine(u.CallbackQuery.From.ID) { // 🔒 only you
					handleCallback(u)
				}
				continue
			}
			if u.Message == nil {
				continue
			}
			if !mine(u.Message.From.ID) { // 🔒 only you
				continue
			}
			if u.Message.Voice != nil { // a voice note you sent
				if voiceMode == "task" {
					go transcribeAndRun(u.Message.Chat.ID, u.Message.Voice.FileID)
				} else if playIncoming {
					go playIncomingVoice(u.Message.Chat.ID, u.Message.Voice.FileID)
				}
				continue
			}
			if u.Message.Text == "" {
				continue
			}
			// debug: show who sent what
			fmt.Printf("msg from id=%d: %q\n", u.Message.From.ID, u.Message.Text)
			handleText(u.Message.Chat.ID, u.Message.Text)
		}
	}
}
