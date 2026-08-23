# Module: apps/mymatasan/services/notification_destination.go

## Purpose

`NotificationDestination` is one configured delivery target. Any number can exist, and each
carries its own severity floor, category subscription, field-inclusion toggles, snapshot
mode and static custom fields — so different recipients receive different payloads without
anyone needing a second notification pipeline.

Types: `webhook`, `telegram`, `mqtt`, **`email`** (W2-7).

## The email destination

`NotificationEmailSettings` holds `To` (the recipient list) and `SubjectPrefix` (e.g.
`[Warehouse 3]`, which lets a recipient covering several sites tell them apart and filter on
it). That is all.

**The SMTP relay is deliberately NOT here.** It is one per install, on
`NotificationSettings.Smtp` (`notification_settings.go`). Adding a recipient is a routing
decision an operator makes often; pointing the install at a different mail server is an
infrastructure decision made once and audited. Copying relay credentials onto every
destination row would also multiply the secret that has to be rotated when it changes.

**Whether the alert image is attached is NOT a field here either.** That is the
destination-wide `SnapshotMode` (`inline` attaches, `link` sends the reference only) — the
same control webhook and MQTT destinations already use. A second, email-only toggle would
let the two disagree, and an operator who set `SnapshotMode` to inline and received no image
would have no way to tell which control had won.
`notification_settings.go`'s `notificationChannelConfig` maps
`IncludeSnapshot: !EqualFold(SnapshotMode, "link")`.

## Normalisation and validation

- `normalizeEmailRecipients` splits comma-separated entries, trims, drops blanks and
  de-duplicates case-insensitively while preserving order. Operators paste these from a
  directory or a spreadsheet, where one address appearing twice is routine — and the same
  alert arriving twice reads as a bug in the alerting.
- `looksLikeEmail` is a deliberately loose shape check: one `@`, something either side, a
  dotted domain with no leading or trailing dot, and no whitespace, comma, semicolon, angle
  bracket or CR/LF. It is **not** an RFC 5322 validator — rejecting an unusual but
  legitimate internal address would be a worse failure than accepting one the relay later
  refuses, which the delivery path already reports per recipient. **The CR/LF part must not
  be relaxed**: it is what keeps a recipient field out of the mail headers.
- `validateDestinations` requires at least one recipient when an email destination is
  **enabled** (a switched-off destination may be saved half-built, as for every other type),
  and rejects a malformed address **whether or not the destination is enabled** —
  stored-then-enabled-later is the dangerous path, and a bad address must never reach the
  store.

## Related

- `apps/mymatasan/services/notification_settings.go` — `NotificationSmtpSettings`, the
  install-wide relay, seeded from the shared `config.json` `smtp` block and saved through
  `PUT /api/settings/notification/smtp` (its own endpoint, so saving the relay never
  rewrites the destinations from a stale copy the browser was holding).
- `infra/notification/mail_channel.go.md` — what an email destination becomes.
