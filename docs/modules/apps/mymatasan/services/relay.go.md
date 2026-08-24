# Module: apps/mymatasan/services/relay.go

## Purpose

`relayService` / `IRelayService`: switching a camera's **relay outputs** (W3-5b) — sirens,
strobes, gates, door strikes, lights.

Everything else this product does is observation: it watches, records, and tells somebody.
This is the only code path that acts on the world, and the only one that can do something a
person then has to physically undo. It is the difference between recording an intrusion and
responding to one.

## It is a chokepoint on purpose

Every route to a relay — an operator's button, a detection rule, anything added later —
comes through `Fire()`. Not because the layering demands it, but because the things that
have to be true of an actuation (it is audited, it is rate-limited, it can always be undone)
are exactly the things that get implemented once and forgotten in the second caller. Same
shape as mypintusan's door actuation.

## The rule that matters most

**Turning something OFF is never refused.** The rate limit, the mode lookup and every other
check apply only to switching a relay *on*. The off branch sits above all of them in
`Fire()`, before anything that can fail — including the `RelayOutputs` read, so an output can
be switched off on a camera that has stopped answering that call.

A limiter that can block an OFF is a siren nobody can silence, and it would refuse exactly
when the siren is sounding — which is when the limiter is most likely to have been tripped.
Anything placed above that branch becomes a way for the appliance to refuse to stop a siren.

The screen honours the same rule: the off control is never `disabled` and never dimmed, not
even during the request that started the siren. Both halves are pinned by checks
(`TestSwitchingAnOutputOffIsNeverRefused`, `TestOffWorksEvenWhenTheCameraCannotBeListed`,
and the `uicheck_relay.js` assertions on `disabled` and computed `opacity`).

## The responsibility for switching off goes to the device

A **monostable** output returns to idle after its own `DelayTime`; a **bistable** one stays
where it was put, forever. So a pulse:

1. uses the device's own timer when the output already has one, and
2. otherwise asks the camera to *become* a timed output (`SetRelayOutputSettings`), and
3. only if the camera refuses does this appliance hold it with a timer in memory —
   and then `RelayView.HeldByUs` says so on screen.

Because that third case is the only state where a crash, a restart or a power cut leaves
something in the building energised. `ReleaseAll` runs first in the shutdown path, before
anything that can block, so a stop does not leave a siren sounding. An output the device
releases needs nothing from any of this, which is the whole reason a pulse prefers it.

**An unknown mode counts as bistable** (`infra/onvif/relay.go`). A device that does not say
whether its relay self-releases must be driven as though it does not: that is the reading
where *we* are responsible for the off, and the other way round leaves a siren running with
nobody holding the responsibility.

## Rules the tests pin

| Rule | Why |
|------|-----|
| OFF before every guard | See above. |
| Only **automatic** actuations are rate-limited | An operator pressing a button twice is an operator who meant it. |
| A throttled automatic actuation is **not an error** | A rule firing repeatedly is doing its job; turning that into a failing alert path would make the rule look broken. |
| Pulse 1–300s, **refused** not clamped | The cap is what stops a mistyped rule holding a gate open for a day. |
| An unknown output is refused | Switching a token the camera does not have is a fault worth naming. |
| "On" is refused on a self-releasing output | It cannot be honoured, and silently producing a pulse of the device's own length instead is worse than saying so. |
| **A rule can only ever pulse** | A rule that could latch an output would leave a siren sounding until somebody found the screen it is switched off from — and the rule that does it is, by construction, the one firing at 4am with nobody watching. `ApplyRuleRelay` hard-codes `RelayActionPulse`. |
| Every actuation is audited, **including failures** | "Who set the siren off at 04:12, and did the camera actually do it" has to be answerable. Unlike a PTZ preset recall, which an operator generates by the dozen and which moves nothing but a camera. |
| A failed release is a **notification**, not a log line | Something is energised, nothing is going to switch it off, and only a person can now. |

The audit recorder is a function (`RelayAuditRecorder`), not the request-scoped `Auditor` the
handlers use, because most actuations have no HTTP request behind them — a rule sounding a
siren at 4am has no actor, no client IP and no user agent, and it still has to be in the
trail. Threading the API's auditor down here would have meant either inventing a fake
request or leaving automatic actuations unaudited, and the second is what happens when the
seam is awkward.

## The grant

`/api/cameras/*/relays` is its own path, not part of `/api/cameras/*/ptz`. A control-room
operator who may point a camera is not automatically somebody who may open a gate. The
default operator preset gets **read** (a screen that cannot list the outputs cannot offer a
button for one) and an administrator grants the switching deliberately, as the Live Views
page's third rung. See `services/pages.go` and `services/rbac.go`.
