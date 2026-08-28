# Module: apps/mypintusan/services/alarm.go

## Purpose

Two unrelated pieces of production plumbing that `services/controller.go.md` needs but stays
free of: the PIN hashing/verification pair, and the shipped `Alarmer` (`NotificationAlarmer`)
that routes door alarms into the shared notification feed rather than a log nobody opens.

## Key Function: BcryptPIN / HashPIN

- `BcryptPIN(hash, presented) bool` — the production `PINVerify` `Controller` is configured
  with. A PIN is a secret, checked the way myidsan checks passwords; bcrypt's deliberate slowness
  matters *more* here than for a password, because a 4-digit PIN space is only 10,000 wide — the
  hash cost is essentially the only thing standing between an attacker holding the database and
  every PIN on the site. Injected rather than called from the decision path so `Decide`
  (`services/decision.go.md`) stays a pure function with no crypto dependency.
- `HashPIN(pin) (string, error)` — produces a storable hash; used only at enrolment time
  (`apis/holders.go.md`'s `issueCredential`).

## Key Type: NotificationAlarmer

```go
type NotificationAlarmer struct {
    notify *notification.Service
    log    applog.Logger
}
```

The shipped `services.Alarmer` implementation, built via `NewNotificationAlarmer`.

## Responsibilities

- `severity(kind)` — `AlarmDuress`/`AlarmDoorForced` → `Critical` (a person may be in trouble or
  a boundary has been breached; neither can wait for someone to notice a dashboard row); every
  other alarm kind — including the new `AlarmDegraded` — → `Warning`. A reader going offline is
  deliberately *not* critical: every door bound to it is out of service, which is serious but not
  an emergency, and paging on it the same way as duress would train people to ignore alarms
  altogether on a site with one flaky segment — the failure mode worth avoiding is an alarm nobody
  believes. A whole site running from cache is the same shape of "serious but not an emergency": the
  cached grants are still correct until the TTL runs out.
- `Raise(ctx, kind, ev, detail)` — publishes a `notification.Notification` (category
  `"access."+kind`, `RefType: "access_event"`, `RefId: ev.Id`) carrying door/reader/holder
  context. **Deliberately does not touch the reader** — a duress alarm must be invisible at the
  door (no LED, no buzzer), because a coercer who can tell an alarm was raised is a coercer who
  takes it out on the holder.
- `alarmTitle(kind)` — the headline an operator sees, kept plain: read at 3am, on a phone, by
  somebody who was asleep. `AlarmDegraded`'s headline is "Running from cache — access rules cannot
  be updated" — the fact an operator most needs, stated in the title rather than buried in the
  detail.
- `Decision(ctx, ev)` — **new**: publishes an access DECISION (a badge accepted or refused, an
  operator unlock) into the feed at `Info` severity, category `"access.granted"` or
  `"access.denied"`. `Raise` alarms tell a human something is wrong now; `Decision` is the
  routine stream `myseliasan`'s correlator matches across nodes ("a door opened AND no badge was
  accepted") — see `apps/myseliasan/services/correlate.go.md`. This is what the badge decisions
  need to travel up the fleet control channel to a control plane once adopted
  (`apps/mypintusan/app/wire_fleet.go.md`).
  - **DURESS IS INVISIBLE IN THIS STREAM.** A duress grant's notification is byte-identical to a
    normal grant's: the `Duress` flag is ignored, `Detail`/`RawCredential` are never included
    (they can carry PIN phrasing), and `decisionReason(ev)` NORMALISES the reason — a granted
    event always reads `"ok"` even though the audit row says `"duress"`, and a bad PIN or a
    duress PIN both coarsen to `"credential-rejected"`. The separate `Critical` duress alarm
    still goes out via `Raise` — worded for the operator — and stays untouched; `Decision` is
    what anything with feed access (a dashboard, a webhook, a digest, the fleet control plane's
    unified feed) could expose to the wrong eyes.

## Notes

- Wired in `apps/mypintusan/app/app.go.md`'s `RegisterAppRoutes`, ahead of building the runtime,
  so every controller built by `app/runtime.go.md`'s `superviseBus` shares the one alarm sink.
  `alarms.Decision` is passed into `newRuntime` as the runtime's `decisions` hook and threaded
  through to every `Controller` via `ControllerConfig.Decisions`.
- A nil `*NotificationAlarmer` or nil `notify` makes `Raise` (and `Decision`) a no-op rather than
  a panic — safe for tests that construct a `Controller` without a real notification service.
- `AlarmDegraded` (`services/controller.go.md`) is raised not from `Controller` but from
  `app/runtime.go.md`'s `runtime.SetOffline` — the transition into/out of offline mode is a
  site-wide fact, not a per-bus one, and `runtime` is the one place that fact is held. It is the
  alarm `docs/MYPINTUSAN_DATA_MODEL.md` §2 has always said offline mode should raise and nothing
  did until the first live bench of offline mode (`tools/fleetbench/bench_pintusan_offline.py`)
  found it missing.
