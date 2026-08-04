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
  other alarm kind → `Warning`. A reader going offline is deliberately *not* critical: every door
  bound to it is out of service, which is serious but not an emergency, and paging on it the same
  way as duress would train people to ignore alarms altogether on a site with one flaky segment —
  the failure mode worth avoiding is an alarm nobody believes.
- `Raise(ctx, kind, ev, detail)` — publishes a `notification.Notification` (category
  `"access."+kind`, `RefType: "access_event"`, `RefId: ev.Id`) carrying door/reader/holder
  context. **Deliberately does not touch the reader** — a duress alarm must be invisible at the
  door (no LED, no buzzer), because a coercer who can tell an alarm was raised is a coercer who
  takes it out on the holder.
- `alarmTitle(kind)` — the headline an operator sees, kept plain: read at 3am, on a phone, by
  somebody who was asleep.

## Notes

- Wired in `apps/mypintusan/app/app.go.md`'s `RegisterAppRoutes`, ahead of building the runtime,
  so every controller built by `app/runtime.go.md`'s `superviseBus` shares the one alarm sink.
- A nil `*NotificationAlarmer` or nil `notify` makes `Raise` a no-op rather than a panic — safe
  for tests that construct a `Controller` without a real notification service.
