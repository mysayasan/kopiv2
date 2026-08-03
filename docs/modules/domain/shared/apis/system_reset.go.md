# Module: domain/shared/apis/system_reset.go

## Purpose

Shared HTTP handler pair + gating middleware for the factory-reset feature, built on top
of `sharedservices.SystemResetService` (`domain/shared/services/system_reset.go.md`). Used
by `myseliasan`, `myidsan`, and `myiotsan`; `mymatasan` has its own older, richer surface
(`apps/mymatasan/apis/system.go.md`).

## Behavior

- `SystemResetHandlers{reset}` / `NewSystemResetHandlers(reset)` — constructs the handler
  trio. Route registration is deliberately left to each app (same pattern as
  `domain/shared/apis/setup.go.md`), since the three apps gate the routes differently —
  see each app's own `apis/system.go.md`.
- `State(w, r)` — `GET` handler. Returns `{ allowed, confirmPhrase }` — `allowed` reflects
  `bootstrap.allowReset`, `confirmPhrase` is the app's own name. Serving the phrase from
  the server (rather than hardcoding the app name in the frontend) means the instruction
  text and the server-side check can never drift apart.
- `Start(w, r)` — `POST` handler. Decodes `{"confirm": "<phrase>"}` (a missing/unparseable
  body is treated as an empty confirmation, not a separate error, so the mismatch message
  is the one an operator sees either way) and calls `reset.Start`. A confirmation mismatch
  returns `400` via `ErrConfirmMismatch`; on success returns the initial `ResetProgress`.
  The wipe and restart proceed in the background — this call returns immediately.
- `Progress(w, r)` — `GET` handler. Returns the current in-memory `ResetProgress` verbatim.

## `NewResetGate`

`NewResetGate(reset) func(http.Handler) http.Handler` — middleware with two jobs while
`reset.InProgress()` is true:

1. **503s every other protected request** with `{"status":"failed","message":"factory
   reset in progress","code":"reset_in_progress"}` and `Retry-After: 5`, instead of letting
   DB-backed handlers fail with a raw `500` against the pool the reset has already closed —
   without this, the SPA's overlay would look like a crash rather than a reset in progress.
2. **Serves `/system/reset/progress` itself**, in front of whatever auth/permission
   middleware the app normally puts in front of that route. This exists because a live run
   on myiotsan showed the progress endpoint answering "access denied" from the moment the
   DB pool closed — it normally sits behind auth/permission middleware that need the very
   database the reset just dropped — leaving the operator blind at 60% through the most
   destructive part of the wipe. The rest of `/system/reset/*` (i.e. the `POST` that starts
   a *second* reset) still falls through to the normal handler, which the service itself
   refuses ("a reset is already in progress").

**Security note**, documented in the source because it is a deliberate, narrow exception to
the normal auth posture: while a reset is running, anyone who can reach the port can read
the stage and percentage with no authentication. That payload carries no secrets — a stage
name, a percentage, a status message — and the reset is an operator-initiated action whose
restart is externally visible anyway. Outside a reset the request falls straight through to
the normal authenticated handler, so this does not open the endpoint up in general.

`reset` is read **per request**, not captured at wiring time, because the service is
constructed after this middleware is wired in each app's `RegisterAppRoutes`. `reset ==
nil` is tolerated (the gate is a no-op, matching `NewSystemApi`'s tolerance of a nil
reset service).

## Notes

- Payload shapes (`ResetProgress`, the `{allowed, confirmPhrase}` state body, the
  `{confirm}` start body) are shared verbatim with `frontend/shared/src/FactoryReset.js`,
  so one SPA component (`FactoryResetSection`/`FactoryResetDialog`/`FactoryResetOverlay`)
  works identically across all three apps.
- Each app registers the three routes behind its own admin/superadmin gate on top of this
  package — see `apps/myseliasan/apis/system.go.md`, `apps/myidsan/apis/system.go.md`,
  `apps/myiotsan/apis/system.go.md`.
