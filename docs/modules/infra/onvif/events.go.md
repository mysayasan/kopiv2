# Module: infra/onvif/events.go

## Purpose

ONVIF **events**: what the camera noticed, rather than what we noticed about the camera
(W3-5b). Response parsing is in `events_parse.go`; relay outputs are in `relay.go`.

`client.go` had no event surface at all. Adding PullPoint unlocks the camera's own analytics
and tamper alarms, its **digital inputs** (door contacts, PIRs, beams, panic buttons) and its
**relay outputs**.

## The transport is a subscription with a lease

`CreatePullPointSubscription` asks the device to open a subscription and returns the
**address the device issued** — every later call (pull, renew, unsubscribe) goes to that URL,
not to the event service. `PullMessages` long-polls it; `RenewSubscription` extends it;
`Unsubscribe` releases it.

A subscription that is not renewed is dropped by the camera **without a word**. That is why
the service layer treats a lapsed one as a fault rather than a retry — see
`apps/mymatasan/services/camera_events.go`.

## Decisions the tests pin

| Rule | Why |
|------|-----|
| The subscription-manager calls carry **WS-Addressing** (`wsa:Action`, `wsa:To`) | Strict devices reject a request to a subscription URL without them. Lenient ones accept it, which is worse: the feature works on the camera it was developed against and fails on somebody's site. Folded into the existing `<s:Header>` rather than emitting a second one, which is not valid SOAP. |
| `PullMessages` uses a **per-call HTTP deadline** of the device timeout + 10s | It is a long poll. The client's ordinary 5s timeout would abort every one of them, tearing the subscription down and rebuilding it forever while reporting a network error each time. |
| ...on a **copy** of the HTTP client | Mutating the shared one would silently give every other ONVIF call the long-poll timeout, and a camera that has gone away would then hang a settings page for a minute instead of failing in five seconds. |
| A subscription with **no address** is an error | It then exists on the camera and we can neither pull from it nor cancel it — a leak on the device, invisible from here. |
| `Lease()` is measured on the **device's** clock | A camera whose clock is wrong is common enough that this app has a date/time screen. The difference between the device's own two timestamps is correct whatever its clock says. An expired lease reports 0, never a negative duration a caller would happily wait for. |
| A message with **no data** is dropped | Devices emit these on some topics as keep-alives; passing them on produces alert rows that say a device did something without saying what. |
| `Initialized` messages **survive parsing** | They are not events, but the service layer needs them as a baseline. Discarding them here would remove the distinction; see the service doc. |
| `SourceToken()` tries several vendor spellings | `InputToken`, `RelayToken`, `VideoSourceConfigurationToken`… A caller that hard-codes one gets nothing from the next vendor. |
| `State()` reports whether the state was **known** | The difference between "the door is closed" and "the camera said something we could not read" is the whole value of the feature. |
| `EventKind` matches on **contains**, not equality | Real topics carry namespace prefixes and sometimes trailing predicates. |
| `UNKNOWN` move/relay states are treated as the safe reading | Consistent with the PTZ layer; see `relay.go` for the bistable default. |

## The camera's own words

Every call goes through `eventError`, which prefers `ParseSOAPFault` over the HTTP status —
the same treatment the PTZ calls get, and for the same reason: a device saying "the
subscription does not exist" is an ordinary answer, and it arrives as HTTP 500.

Live-benched against `tools/fleetbench/onvifsim.py`, which holds a real long poll open,
issues real leases, and can **lapse a subscription on demand** — because that is the case
worth testing.
