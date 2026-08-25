# Module: apps/mymatasan/services/case_notification.go

## Purpose

Put one entry from the unified feed into a case file (W3-3c) — **resolved to what it actually
refers to**, not stored as whatever the screen happened to call it.

## Why the server resolves it, and not the client

Most of what an operator notices in the feed is a notification *about* something else: an AI
alert firing on a camera. Storing that as a "notification" item would put the same evidence in
a case under two different kinds depending on which screen it was added from, with two
different spans — and the export bundle would carry it twice.

So `AddNotification` decides:

| The feed entry | Becomes | Carrying |
|---|---|---|
| points at an alert (`refType: alert_event`) | `CaseItemAlert` | the **alert's** camera, time, snapshot and id |
| names a camera | `CaseItemNotification` | footage padded ±`notificationPadSeconds` around the moment, which the case then holds |
| names nothing | `CaseItemNotification` | the moment only. Holds nothing, exports as text |

The operator's own note travels with it either way — that is the half that turns a pile of rows
into an argument.

## The alert's time, not the feed row's

They are usually within a second of each other and occasionally are not: a relayed or replayed
notification carries the time it was ingested *here*. The evidence is the alert, so the span is
centred on the alert.

## A purged alert does not break the button

Alerts are deleted on their own retention schedule while the feed row survives. When the alert
is gone, the add **falls back to the feed entry itself** rather than refusing: the operator can
still record what they saw and when, which is the part a case needs, and the alternative is a
button that silently stops working on old rows.

## Not claimed

- **No de-duplication across kinds.** Adding the same alert from the alert screen and from the
  feed lands one item, because the resolution makes both the same alert item and `AddItem`
  refuses the duplicate. Adding a *different* feed entry about the same incident is two items,
  and that is correct — they are two things that were noticed.
- **The resolution is not proved live for the alert path.** It needs a real detection, and the
  bench fleet films a test pattern; `case_notification_test.go` covers it, mutation-checked,
  including the purged-alert fallback. See `docs/FLAGSHIP_BENCH_CHECKLIST.md` § W3-3c.
