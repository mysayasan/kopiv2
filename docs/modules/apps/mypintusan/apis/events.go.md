# Module: apps/mypintusan/apis/events.go

## Purpose

Two unrelated but small surfaces sharing a file: the read-only access log (`GET /events`), and
the first-run setup-wizard endpoints (`GET /setup/state`, `POST /setup/complete`) on the same
shared `setup.state` contract mymatasan/myidsan/myiotsan/myseliasan use.

## Responsibilities

- `NewEventApi` / `list` — **read only, and there is no delete route anywhere in this app.** The
  log is the product: "the fire door opened at 02:14 and nobody badged" is a fact somebody may
  want gone, and a system that can quietly rewrite it is not evidence of anything. Retention,
  when it lands, has to be a scheduled policy with its own audit trail rather than a button.
  `list` supports `limit` (default 200, capped at 1000), `offset`, and filters on `doorId`,
  `holderId`, `decision` (`granted`/`denied`), `reason`, and `since` — but nothing filters out
  denials by default, because a denied unknown credential at 03:00 on a perimeter door is the
  single most valuable row this table holds.
- `NewSetupApi` — wraps `sharedapis.NewSetupHandlers(setup)` for `GET /setup/state`, plus a
  locally admin-gated `POST /setup/complete` (`sharedapis.LocalUserFromContext` +
  `user.IsAdmin`) — promoting the first-run wizard's completion flag off any per-browser storage
  onto a property of the install, matching the other three apps
  (`domain/shared/services/setup_state.go.md`).

## Notes

- mypintusan has no setup-wizard **frontend** yet (see `apps/mypintusan/app/app.go.md`'s Notes)
  — these endpoints exist so a future UI, or a script, has the same contract the other apps'
  wizards already use; nothing in this app currently calls `POST /setup/complete` on its own.
