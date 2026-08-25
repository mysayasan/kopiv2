# Module: apps/myseliasan/entities/push_subscription.go

## Purpose

`PushSubscription` is one browser, on one device, that has agreed to be woken by this control
plane (W3-9). Table `push_subscription`, created by the auto-migrator on first boot.

## Fields

| Field | Notes |
|---|---|
| `Id` | Auto-increment primary key. |
| `UserId` | Who this device belongs to (`idx:"user"`). A subscription is personal: it wakes a phone in somebody's pocket, so only its owner — or a superadmin cleaning up after somebody who has left — may remove it. |
| `Endpoint` | The vendor URL this device is reached at, **unique** (`ukey:"push_endpoint"`). Also, unavoidably, a third-party identifier for a personal device: it is never returned by the API and never written to the audit trail. |
| `P256dh`, `Auth` | The browser's key material for the payload encryption (RFC 8291). `Auth` is a secret — with it and the endpoint, anyone could decrypt what we send. Both carry `json:"-"`. |
| `Label` | What the operator calls this device, so a list is something a person can act on rather than a column of opaque URLs. |
| `MinSeverity` | The floor for THIS device (see below). |
| `Enabled` | Parks a device without forgetting it — a phone left at home for a week. |
| `LastOutcome`, `LastDetail`, `LastAttemptAt` | What the last real delivery attempt did. |
| `LastDeliveredAt` | The last time a push service ACCEPTED a message. Kept apart from `LastAttemptAt` because "we tried a second ago" and "it worked a second ago" are different facts and only one of them is reassuring. |
| `CreatedAt`, `UpdatedAt` | Unix seconds. |

## Why the endpoint carries the unique key

It is per BROWSER, not per user: the same operator with a phone and a laptop has two rows, and
revoking one must not silence the other. Browsers also renew their subscriptions, and a renewal
that added a row would add one more buzz per notification to the same phone — nothing about
which looks broken from the server. Keying on the endpoint makes re-subscribing an update.

## Why the floor is per device

The phone in somebody's pocket at 3am and the laptop on their desk want different thresholds. A
single install-wide setting forces the stricter one on everybody, or the looser one on the
person who then mutes the app — and a muted app is the same as no push at all.

## Why the outcome columns are here at all

They are not diagnostics; they ARE the feature's honesty. An install that cannot reach a push
service has to say so, and the only way to know is to have tried. `services/push.go` derives
the install-wide verdict entirely from these columns.

## Notes

- The auto-migrator creates new TABLES but does not ALTER existing ones. Adding a column here
  needs a migration; this table shipped complete in W3-9.
- Rows are deleted, not marked, when a push service reports the subscription `gone` (404/410) —
  see `services/push.go.md`.
