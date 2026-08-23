# Module: infra/notification/mail_channel.go

## Purpose

`NewMailChannel` is the outbound **email** delivery channel — the seventh sink alongside
log, store, SSE, reloadable, webhook, telegram and MQTT. It exists because a mailbox is the
one destination every organisation already operates, audits and retains: a webhook needs an
endpoint somebody maintains and a bot needs an account somebody approves, while email needs
a decision that has usually already been made.

It delivers through `infra/mailer` (`infra/mailer/mailer.go.md`), so there is exactly one
SMTP implementation in the suite and one place the credential rules live.

## The relay is NOT per destination

`MailOptions.Relay` is a whole `mailer.Config`, but it is supplied **once per install**, not
once per recipient — `domain/notification.ChannelConfig.Smtp` holds it and every email
destination is built against the same value (`domain/notification/service.go.md`).

An SMTP relay is infrastructure, not a routing choice. Copying its password onto every
destination row would multiply the secret that has to be rotated when it changes, and it
would let a notification screen quietly open network egress on an install whose config says
mail is off. Adding a recipient stays a frequent, low-privilege decision; pointing the
install at a different mail server stays a rare, audited one.

## Responsibilities

- Returns a `noopChannel` named `"email"` when the relay is disabled/hostless or no
  recipient is configured, so the channel can always be registered and switched on later.
- Wraps the sender in the shared `asyncSender` (`infra/notification/async.go`), inheriting
  the queue, the per-channel severity floor, and the 1s/3s/6s retry schedule.
- Renders the subject as `<prefix> [SEVERITY] <title>`, truncated to 200 runes.
  `SubjectPrefix` (e.g. `[Warehouse 3]`) is what lets a recipient covering several sites
  tell them apart and build a mailbox rule on it. A blank title falls back to
  `"Notification"` rather than producing a blank-subject email.
- Renders a plain-text body: message, then `Severity`/`Category`/`Source`, then the time
  **always in UTC and always labelled** — an unlabelled local timestamp in an email read on
  another continent is worse than no timestamp — then the deep link.
- Sets `X-Kopiv2-Severity`, `X-Kopiv2-Category` and `X-Kopiv2-Source` so a recipient can
  filter into folders without parsing the body. This is the first thing an ops team asks
  for and it costs three headers.
- Attaches the notification's image when `IncludeSnapshot` is set and the notification
  carries one. In mymatasan this is driven by the destination-wide `SnapshotMode`
  (`inline` attaches, `link` does not) rather than an email-only toggle, so the two can
  never disagree.

## The error contract is the load-bearing part

The async worker retries whatever `send` returns unless it is marked `permanent`, so
classifying the failure decides how many copies of an alert a working mailbox receives.

| Outcome | Returned | Why |
|---|---|---|
| Delivered | `nil` | — |
| `*mailer.RecipientError`, `Delivered()` true | `nil` (+ warning log) | The message **was** delivered to the rest. Retrying would mail the working addresses again, on every alert, forever, because of one stale entry in a distribution list. |
| `*mailer.RecipientError`, all rejected | `permanent(err)` | Nobody got it, but the relay refused the addresses **by name** — retrying the same list cannot change that answer. |
| Anything else (dial refused, timeout, 4xx) | the error | Transient. A relay that is merely restarting must not lose the alert. |

Both non-retry cases and the retry case are asserted in `mail_channel_test.go`
(`TestMailChannelPartialRejectionIsNotRetried`, `TestMailChannelTotalRejectionIsNotRetried`,
`TestMailChannelRetriesTransientFailure`), and all three were mutation-checked.

## Testability

`MailOptions.Sender` narrows the transport to a one-method `MailSender` interface. Without
it the partial/total rejection paths — which are the entire point of the file — could not be
driven from a test without standing up an SMTP server. (Same shape as W2-6's `frameWriter`.)

## Related

- `infra/mailer/mailer.go.md`, `infra/mailer/message.go.md` — the transport and MIME.
- `domain/notification/service.go.md` — where an `email` destination becomes this channel.
- `apps/mymatasan/services/notification_destination.go.md` — the per-destination model.
- `apps/myseliasan/services/notification_channels.go.md` — the control plane's config.
