# Module: apps/myseliasan/services/notification_channels.go

## Purpose

`NotificationChannelConfig(cfg)` gives the control plane an **outbound** leg.

Until W2-7, `apps/myseliasan/app/app.go` built a `domain/notification.Service` and never
called `Configure` on it. The fleet feed was therefore persist + log + SSE only: every
node-offline, every relayed alert, every monitoring gap reached a browser and nowhere else.
An operator watching fifty sites had to be looking at the screen to learn that one of them
had gone dark — which is precisely the moment nobody is looking at a screen.

It also made two config blocks untrue. `notification.webhook` and `notification.telegram`
have been in `infra/config`'s `NotificationConfigModel` all along and were never consumed by
this app; an operator who filled them in got silence. Wiring `Configure` makes them work and
adds `notification.email` alongside.

## Responsibilities

- Maps the shared top-level `smtp` block into `notification.SmtpConfig` (the relay every
  email destination delivers through), defaulting a zero/invalid port to **587** —
  submission, not 25, because the target is an internal submission endpoint and 25 is
  blocked outbound on most networks.
- Builds at most one destination per configured block: `config-webhook`, `config-telegram`,
  `config-email`. Each is skipped unless its block is **enabled** *and* carries the target
  it needs (a URL / a token+chat / at least one recipient), so a half-filled config never
  produces a channel that cannot deliver.
- `splitList` turns the comma-separated `to` and `categories` values into trimmed,
  de-duplicated lists. Comma-strings rather than JSON arrays is the shape the settings
  editor already uses for lists (`allowOrigins`), so the field round-trips through the
  editor without special handling.
- `looksLikeEmailAddress` is the deliberately loose shape check the settings editor
  validates recipients with: one `@`, something either side, a dotted domain with no leading
  or trailing dot, and no whitespace or CR/LF. It is **not** an RFC 5322 validator —
  rejecting an unusual but legitimate internal address would be a worse failure than
  accepting one the relay later refuses, which the delivery path already reports per
  recipient. The CR/LF part is the one that must not be relaxed: it is what keeps a
  recipient field out of the mail headers.

## Upgrade safety

Everything is off unless explicitly enabled, and a `nil` or zero config yields zero
destinations. An install that upgrades onto this build starts delivering nothing new.
`TestNotificationChannelConfigDefaultsOff` pins this with a **fully populated** relay and
recipient list behind a disabled flag, because an empty config has no recipients either and
would pass for the wrong reason.

## The snapshot is deliberately never attached

`Email.IncludeSnapshot` is hard-coded `false` here. A node's notification crosses the
control channel as JSON and `Notification.Attachment` is `json:"-"`, so no image survives
the hop — claiming to attach one would produce mail that promises evidence it does not
carry. `TestControlPlaneNotificationDropsAttachment` asserts the wire format actually drops
it, so if that ever changes the test fails and the decision gets revisited rather than the
comment quietly becoming untrue.

## Related

- `apps/myseliasan/services/settings.go.md` — the `notification` section that edits this,
  and `TestMail`, the send-a-real-message-before-you-rely-on-it button.
- `infra/notification/mail_channel.go.md` — the channel this config builds.
- `domain/notification/service.go.md` — `Configure` and `buildDestinationChannel`.
