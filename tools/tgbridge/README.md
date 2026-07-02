# tgbridge

A standalone, dependency-free (stdlib only) Go dev/ops utility that relays a
Telegram chat to the headless Claude Code CLI, so you can drive coding
sessions from your phone the way you would in VS Code. It also delivers
files/screenshots (via an outbox) and relays ad-hoc PC commands (health,
lock/hibernate/shutdown/restart) during development.

This is developer tooling only — it is not part of any shipped app, has no
runtime dependency on kopiv2's apps/domain/infra, and is not covered by
`docs/modules/` or the version changelog.

## Setup

1. Copy `config.json.example` to `config.json` and fill in:
   - `bot_token` — from [@BotFather](https://t.me/BotFather)
   - `allowed_user_id` — your numeric Telegram user id (from
     [@userinfobot](https://t.me/userinfobot)); the bridge only responds to
     this id
   - `workdir` — default project directory Claude runs in (e.g. this repo)
   - optional: `ffmpeg_path`, `chrome_path` (screenshots), voice/STT settings
     — see the `Config` struct doc comments in `main.go` for the full list
2. `config.json` and `tgbridge_state.json` (session/runtime state) are
   git-ignored — your bot token and session ids never hit git.

## Run

From the repo root:

```
go run ./tools/tgbridge
```

Or build a stable exe (needed for `/autostart`):

```
make            # builds tools/tgbridge/tgbridge.exe
make run        # go run, no exe
make clean      # remove the exe
```

## Usage

Message the bot `/help` for the full command list (sessions, model/mode/
effort, screenshots, voice notes, machine power control, `/autostart`).
Any non-command text is sent as a prompt to the active Claude session.

## Optional: local Whisper for voice-note transcription

See `whisper/README.md` for running a local, OpenAI-compatible speech-to-text
server in Docker, so voice-note-to-task transcription is accurate and never
leaves your PC. Without it, `/voicemode task` still works via the built-in
Windows speech recognizer.
