# Module: apps/myseliasan/apis/push_api.go

## Purpose

The control plane's HTTP surface for mobile push (W3-9). Design lives in
`services/push.go.md`.

## Routes (`/api/push`)

| Method | Path | Access | Purpose |
|---|---|---|---|
| GET | `/status` | matrix | What this appliance can actually do, from real attempts, plus the public key a browser needs to subscribe |
| GET | `/devices` | matrix + owner | The devices this session may see |
| POST | `/devices` | matrix + owner | Register (or re-register) a browser — and PROVE it before answering |
| DELETE | `/devices/{id}` | matrix + owner | Stop waking a device |
| POST | `/devices/{id}/test` | matrix + owner | Send a real notification to one device, on demand |

## Authorization: two axes, and they answer different questions

**The matrix.** The whole surface sits behind an accessrbac grant on `/api/push`, like every
other module. That is deliberate and it is not bureaucracy: enabling push makes this appliance
open outbound HTTPS connections to a browser vendor, and on the intranet installs this product
is usually sold into, the deployment's entire security posture is that it does not do that.
Whether the control plane talks to Google or Apple **at all** is an administrator's decision,
not one each signed-in user makes for the estate.

**Ownership, which the matrix cannot express.** A push subscription is a phone in somebody's
pocket, not a fleet object. Within the grant a user sees and acts on THEIR OWN devices only. A
superadmin sees all of them, because somebody has to be able to revoke the device of a person
who has left, and a subscription nobody can remove keeps receiving fleet alerts forever.

The API layer only decides which flag to pass; `services/push.go` enforces the same rule again
on every call. Two places, because a check that exists only in the HTTP layer is one refactor
away from not existing at all.

## `POST /devices` is not a create

It registers **and proves**: the service performs a real delivery before returning, so the view
in the response already carries `lastOutcome`. That is the contract of the whole feature — a
device is registered and proved, or registered and known not to be reachable. The screen shows
whichever it was, and never says "registered" on its own.

The same endpoint is an **upsert by endpoint**, so the SPA re-posting on every page load is how
a browser-rotated subscription heals rather than accumulating a duplicate.

## Not returned

`p256dh` and `auth` never leave the server (`json:"-"` on the entity). With the endpoint and
the auth secret, anyone could decrypt what this appliance sends. The endpoint itself is not
returned either — only its host, as `vendor`, which is what a firewall rule needs and all
anybody but the sender requires.
