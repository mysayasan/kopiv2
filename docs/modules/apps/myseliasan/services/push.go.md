# Module: apps/myseliasan/services/push.go

## Purpose

Mobile push for the fleet control plane (W3-9): the install's push identity, the devices
registered against it, and delivery to them. The transport is `infra/webpush`; this is
everything around it.

## What it is for

W2-7 gave this appliance an outbound leg at all — before it, a node going dark at 03:00 was
persisted, logged, streamed to any open browser, and stopped there. Email closed most of that.
What email does not do is **wake somebody**. This does, which is the situation F-20 described
and the one nobody is looking at a screen for.

## The honesty problem, which is most of this file

A Web Push message is delivered by POSTing to a URL a browser vendor owns. **This control plane
is normally deployed on an intranet with no internet egress**, where every one of those POSTs
fails at the TCP connect and no setting changes it.

So the feature never claims to work. It **measures** whether it works, per device, from real
attempts:

| `PushDelivery*` | Reached when |
|---|---|
| `no-devices` | Nothing has subscribed. |
| `untested` | Devices exist and none has ever been attempted. **Not "fine".** |
| `confirmed` | At least one device was accepted by a push service. |
| `unreachable` | Every attempt failed before reaching anything — the air-gapped install. |
| `rejected` | Something was reached and refused. |

The ORDER of those cases in `Status` is the whole point, and each is covered by its own test.
`untested` exists for the same reason W3-7's standby has one: absence of evidence gets its own
answer rather than borrowing the reassuring one.

`Subscribe` **performs a real delivery before it returns**, so the view handed back already
carries the verdict. A device is never merely "registered": it is registered and proved, or
registered and known not to be reachable.

## The per-device severity floor

`MinSeverity` lives on the DEVICE, not on the install. The phone in somebody's pocket at 3am
and the laptop on their desk want different thresholds, and one install-wide filter forces the
stricter one on everybody or the looser one on the person who then mutes the app — which is the
same as having no push at all. The channel is therefore registered on the hub with **no**
filter, and `fanOut` applies each row's own.

`normalizePushSeverity` decides the UNSET case before `Normalize()` sees it. `Normalize()`
answers `info` for anything it does not recognise, which is right for the hub and wrong here: a
device with no floor would be woken by every routine event. The default is `warning`.

## The rules that keep the table honest

- **One row per BROWSER**, keyed by endpoint (`ukey:"push_endpoint"`). Browsers renew
  subscriptions; a renewal that added a row would add one more buzz per notification to the
  same phone, and nothing about that looks broken from the server.
- **Re-subscribing does not transfer ownership.** A shared browser must not silently re-point
  somebody else's device at whoever signed in last.
- **`gone` DELETES the row.** A subscription the service reports as 404/410 will never accept
  another message; keeping it means an outbound request per notification, forever, and a
  permanently red line on somebody's screen.
- **A refusal does NOT delete.** A transient fault (clock skew, a rate limit) must not
  silently unenrol the fleet.
- **The queue DROPS when full.** The hub delivers to every channel in turn; a push service that
  has gone slow must not hold up the SSE stream, the log or the database write, all of which
  still reach somebody.

## The VAPID identity

Minted on first use and **never rotated casually**. A browser binds its subscription to the
public key it subscribed with, so a second key pair silently orphans every device already
enrolled: they stay in the table, they stop being reachable, and the only symptom is
notifications that quietly stop arriving. There is deliberately no regenerate path. The private
key is sealed at rest with the same cipher the fleet CA key uses.

If a stored key cannot produce a public half, `keysFor` FAILS rather than minting a
replacement — saying so is better than orphaning every device.

## Permission and privacy

Two axes, and they are not the same question. Enabling push makes this appliance open outbound
connections to a browser vendor, which on an intranet install is a decision an ADMINISTRATOR
owns — so the whole surface sits behind an accessrbac grant (`apis/push_api.go`). Within that
grant a subscription is a phone in somebody's pocket: a user sees and acts on their own only, a
superadmin on all, because somebody has to be able to revoke the device of a person who has
left. The service enforces ownership again on every call; a check that exists only in the HTTP
layer is one refactor away from not existing.

The audit trail records the VENDOR and never the endpoint. An endpoint is a third-party
identifier for somebody's personal device, and a trail is a long-lived document read by people
who never needed to know which phone anybody carries. The API never returns `P256dh` or `Auth`.

`truncatePush` bounds a payload in BYTES (what the service counts) but cuts on a RUNE boundary
— three of this product's four languages are not ASCII, and a byte-sliced title ends in a
replacement box on a lock screen nobody can go and correct.

## Tests

`push_test.go` covers the proof-on-subscribe, the air-gapped enrolment, the renewal upsert, the
ownership rules, the four outcomes, the install verdict in all five states, the per-device
floor, the dropping channel, the payload shape, and truncation swept across every limit from 8
bytes up (a single limit passes by luck whenever it lands on a character boundary). Mutation-
checked five ways — an info default floor, keeping a gone subscription, folding no-egress into
refusals, calling untested confirmed, and slicing bytes instead of runes — each failing the
matching test with a message that names the defect.

What the tests cannot cover is the wiring to the hub; that is `tools/fleetbench/bench_w39_push.py`,
which takes a node down for real and counts what reaches a device.

## Not claimed

- **No delivery guarantee**: `delivered` means a push service accepted it. The feed remains the
  record.
- **No rotation repair from the worker.** See `sw.js` — the SPA re-posts on load and the dead
  endpoint is swept by the 410 rule, so a device is unreachable between a rotation and the next
  time somebody opens the app.
- **No offline cache**, deliberately. A control plane serving yesterday's node list would show
  a green estate that went dark an hour ago.
