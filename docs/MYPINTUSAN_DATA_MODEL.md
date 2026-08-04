# MyPintuSan — Door & Credential Data Model

Status: **P1 PARTIALLY BUILT — logic layer only, not a runnable app.** All the entities below
(`Holder`, `Credential`, `Door`, `Reader`/`ReaderProfile`, `AccessGroup`/`Grant`/`Schedule`/
`ScheduleWindow`/`Holiday`, `AccessEvent`) now exist as Go structs in `apps/mypintusan/entities`,
and the decision path (§5), the door state machine (§4), and Wiegand decode/encode are built and
tested in `apps/mypintusan/services` (87.3% coverage). What is **not** there yet: **no database
layer** — `Store` is an interface with only an in-memory test implementation, nothing persists
across a restart, and there is no repository, migration, or dbsql entity registration; **no
`apps/mypintusan/apis`, no `app/` composition root, no `config.json`, no firstrun/setup wizard,
no frontend** — the app cannot be started, it is a library. `Controller.ContactChanged` exists
as a seam but nothing calls it (no myiotsan door-contact binding wired in),
`Door.RelayDeviceKey` → myiotsan `CommandService.Issue` is not implemented (the only shipped
`Actuator` drives the reader's own output), PIN pairing is PIN-then-card only, and
`Snapshot.AntiPassbackViolation` is an input nothing computes (P3 anyway). See
`docs/modules/apps/mypintusan/entities/` and `docs/modules/apps/mypintusan/services/` for the
per-file detail. Companion to
[`MYPINTUSAN_HARDWARE_PLAN.md`](MYPINTUSAN_HARDWARE_PLAN.md) (reader profiles, trust tiers,
reference kit — still design only above the `ReaderProfile` struct itself) and
[`MYPINTUSAN_OSDP_PLAN.md`](MYPINTUSAN_OSDP_PLAN.md) (the driver, built — build order steps 1–4
of 6: `infra/access/osdp` + `tools/osdp-sim` — though steps 5–6, real hardware, are not).

This document settles the two questions §7 of the hardware plan left open — **where
credentials live** and **what happens offline** — then specifies the entities, the door
state machine, and the decision path a badge swipe takes.

---

## 1. Where credentials live: `mypintusan`, not `myidsan`

**Decision: `mypintusan` owns a local `Holder` table. `myidsan` remains the person
authority for anyone who also signs into software.**

The deciding argument is not architectural neatness, it is population. **Most people who
badge through a door never sign into any app** — contractors, cleaners, delivery drivers,
visitors, and the majority of floor staff. Forcing every one of them into `myidsan` to hold
a card would pollute the identity broker with thousands of non-users, each an account that
RBAC, MFA policy and session management now have to reason about. It would also drag
`myidsan` — currently a clean intranet SSO binary — into the life-safety blast radius.

So:

- `Holder` is local, and may exist with **no** `myidsan` link at all.
- `Holder.SsoUserId` is a **nullable, strict** link to a `myidsan` user.
- **Link by `SsoUserId` only. Never by email.** This is a direct carry-over of the
  privilege-escalation bug already fixed in the suite's federated-user dedup: email-fallback
  matching was removed because `myidsan` can emit a non-unique email. Re-introducing an
  email fallback here would let one person's card bind to another person's identity, which
  in this app is a door opening for the wrong human.
- When a holder *is* linked, `myidsan` group membership can drive access-group membership —
  read-only, one direction, never the reverse.

---

## 2. Offline behaviour

**Decision: cache, with a per-door-class policy, and never fail open.**

A door whose controller cannot reach the rest of the system must still work — a network
blip is not a reason to strand staff outside a building. But "works offline" must not mean
"stops checking".

| Door class | Offline policy | Cache TTL (default) |
| --- | --- | --- |
| `interior` | Serve from the cached credential + schedule set. | 72 h |
| `perimeter` | Serve from cache, and raise a degraded-mode alert immediately. | 24 h |
| `critical` | Serve from cache **only** for holders explicitly flagged `OfflineAllowed`. | 8 h |

Past TTL, the door denies. There is deliberately **no `allow-all` option** — a "fail open on
network loss" setting is a documented attack (cut the uplink, walk in), and offering it as a
checkbox means someone will tick it. Free egress is unaffected by any of this because egress
is hardware (see the hardware plan §6).

The cache is a full local replica of the credential/schedule set for *that controller's*
doors, refreshed on change, sealed at rest with the existing `infra/atrest` machinery.

---

## 3. Entities

Proposed home: `apps/mypintusan/entities/`. Struct tags follow the house convention
(`json`/`form`/`query`, `pkey`, `ukey`, `skipWhenInsert`, `validate`); omitted below for
readability except where a field needs comment.

### 3.1 `Holder`

```go
type Holder struct {
	Id        int64
	Ref       string // site's own person reference (staff no., contractor id) — ukey
	Name      string
	Kind      string // staff | contractor | visitor | service
	// SsoUserId links to a myidsan user, or 0 for the majority who have no login.
	// STRICT id match only — never fall back to email (see §1).
	SsoUserId int64
	Status    string // active | suspended | terminated
	ValidFrom int64
	ValidUntil int64 // 0 = no expiry. Contractors and visitors should always set it.
	// OfflineAllowed permits this holder through a `critical` door while the controller
	// is running on cache. Default false — it is an explicit, auditable grant.
	OfflineAllowed bool
}
```

### 3.2 `Credential`

One physical token. A holder may have several (card + PIN + plate).

```go
type Credential struct {
	Id       int64
	HolderId int64
	// Kind: card | pin | plate | face | mobile. `plate` and `face` are satisfied by
	// mymatasan's LPR and face-recognition rather than by a reader on the RS-485 bus —
	// same decision path, different sensor (§5.2).
	Kind string
	// Format is the credential encoding: wiegand26 | wiegand34 | raw-uid | desfire-ev2 |
	// seos | plate | face-vector. It is part of the MATCH KEY, not decoration.
	Format string
	// FacilityCode + CardNumber together identify a Wiegand credential. Matching on
	// CardNumber alone is a real-world collision: card 1234 exists in every facility code
	// ever issued, so a site with two card batches will grant the wrong person.
	FacilityCode int
	CardNumber   string
	// PinHash — a PIN is a secret and is stored hashed, the way myidsan stores passwords.
	// Never a plaintext or reversible PIN column.
	PinHash string
	// DuressPinHash, when set, grants access AND fires a silent alarm (§5.3).
	DuressPinHash string

	Status     string // active | lost | stolen | suspended | expired | revoked
	ValidFrom  int64
	ValidUntil int64
	IssuedBy   int64
	IssuedAt   int64
	RevokedBy  int64
	RevokedAt  int64
	RevokeReason string
}
```

### 3.3 `Door`

```go
type Door struct {
	Id   int64
	Name string
	// Placement reuses the myseliasan site/building/floor tree rather than inventing a
	// second geography — a door belongs on a floor plan next to the cameras watching it.
	SiteId, BuildingId, FloorId int64

	Class    string // interior | perimeter | critical  — drives §2 and the trust matrix
	LockKind string // fail-secure | fail-safe  — see hardware plan §6; changes the wiring
	                // AND the alarm semantics, so it is modelled, not just documented

	UnlockSeconds         int // strike time, typically 5
	ExtendedUnlockSeconds int // accessibility: longer unlock for flagged holders
	HeldOpenSeconds       int // door-held-open alarm threshold, typically 30

	ReaderInId  int64 // entry-side reader
	ReaderOutId int64 // exit-side reader; 0 for a REX-only door. Required for hard APB.

	// Bindings into myiotsan: the relay channel that throws the strike, and the contact
	// that reports real door position. Actuation goes through myiotsan's guarded
	// CommandService.Issue chokepoint — mypintusan does not drive a relay directly (§5.4).
	RelayDeviceKey   string
	RelayChannel     int
	ContactDeviceKey string

	RequireSecureChannel bool   // default true for perimeter/critical
	OfflinePolicy        string // cached | deny  (there is no allow-all — §2)
}
```

### 3.4 `Reader`

The *instance* on a bus, distinct from `ReaderProfile` (the model template).

```go
type Reader struct {
	Id        int64
	Name      string
	ProfileId int64
	DoorId    int64
	Direction string // in | out

	BusPort     string // "COM3" / "/dev/ttyUSB0" — the OSDP bus this PD sits on
	OsdpAddress int    // 0-126, unique per bus. Readers ship at 0; onboarding must reassign.

	// ScbkState: default | rekeyed | failed. A reader still on the well-known default
	// base key is NOT secure; the hardware plan caps such a reader at `interior` until
	// rekeyed, and the UI nags until then.
	ScbkState string

	LastSeenAt  int64
	TamperState string // ok | tamper | offline
}
```

### 3.5 Access rules

The classic triple, kept deliberately boring:

```go
type AccessGroup struct { Id int64; Name string }           // a set of holders
type AccessGroupMember struct { GroupId, HolderId int64 }

type Schedule struct { Id int64; Name string }              // named time policy
type ScheduleWindow struct {                                 // weekly pattern
	ScheduleId int64
	Weekday    int   // 0-6
	StartMin   int   // minutes from midnight
	EndMin     int
}
type Holiday struct {                                        // its own entity on purpose:
	Id   int64                                               // Malaysian public holidays vary
	Name string                                              // BY STATE, so a site needs its
	Date string                                              // own calendar, not a hardcoded one
	ScheduleBehaviour string // deny | follow-sunday | ignore
}

// Grant is the ACL row: this group, through this door, during this schedule.
type Grant struct { Id int64; GroupId, DoorId, ScheduleId int64 }
```

### 3.6 `AccessEvent` — append-only

Modelled on `myidsan`'s existing append-only audit log. Every decision is recorded,
**including denials and unknown cards** — a denied unknown credential at 03:00 on a
perimeter door is the single most valuable row this table will ever hold.

```go
type AccessEvent struct {
	Id       int64
	At       int64
	DoorId   int64
	ReaderId int64
	// Nullable: an unknown card has no CredentialId and no HolderId. Store the raw
	// presented value anyway so the operator can enrol it or investigate it.
	CredentialId int64
	HolderId     int64
	RawCredential string

	Decision string // granted | denied
	// Reason is the machine-readable why. Enumerated because "denied" alone is useless
	// at 3am: ok | unknown-credential | credential-expired | credential-revoked |
	// holder-suspended | no-grant | out-of-schedule | holiday | lockdown | antipassback |
	// offline-cache-expired | secure-channel-failed | reader-offline | duress
	Reason string

	// Correlation hooks: the snapshot mymatasan captured at this door at this instant,
	// and the myiotsan contact event that confirms the door actually opened.
	SnapshotRef string
	ContactRef  string
}
```

---

## 4. Door state machine

States, and the transitions that matter:

```
                   ┌────────────────────────────────────────────┐
                   │                                            │
              ┌────▼─────┐  grant     ┌──────────┐  timer   ┌───┴────┐
              │  LOCKED  ├───────────►│ UNLOCKED ├─────────►│ RELOCK │
              └────┬─────┘            └────┬─────┘          └────────┘
                   │                       │
   contact opens   │                       │ contact still open
   with no grant   │                       │ past HeldOpenSeconds
                   ▼                       ▼
            ┌─────────────┐         ┌──────────────┐
            │ DOOR FORCED │         │ HELD OPEN    │   both → alert + AccessEvent
            └─────────────┘         └──────────────┘

  Overlay states (independent of the above):
    FREE-ACCESS  scheduled unlock (office hours). Gated by FIRST-PERSON-IN on perimeter
                 doors: the schedule does not unlock the building until one valid holder
                 has actually arrived — otherwise a public holiday nobody noticed leaves
                 the front door open all day.
    LOCKDOWN     overrides every grant and every schedule. Never overrides egress, which
                 is hardware and cannot be overridden by software by design.
```

`DOOR FORCED` and `HELD OPEN` are only detectable because the door contact is bound
(`Door.ContactBinding`). A door with no contact can report that it *energised the strike*
but not that anyone went through — worth surfacing in the UI as a capability gap rather
than silently degrading.

---

## 5. The decision path

### 5.1 Order of checks

Evaluated in this order, first failure wins, every outcome written to `AccessEvent`:

1. Reader healthy and (if `RequireSecureChannel`) Secure Channel established — else
   **deny, out of service, alarm**. No cleartext fallback.
2. Lockdown active? → deny.
3. Credential known, `active`, within `ValidFrom`/`ValidUntil`? → else deny.
4. Holder `active`, within validity? → else deny.
5. A `Grant` exists for (holder's groups × this door)? → else deny.
6. Current time inside that grant's `Schedule`, accounting for `Holiday`? → else deny.
7. Anti-passback satisfied (§5.5)? → else deny.
8. If running on cache: within TTL, and for `critical`, holder `OfflineAllowed`? → else deny.
9. **Grant** → fire relay for `UnlockSeconds` via `CommandService.Issue`.

### 5.2 Plate and face are just credentials

A `Credential` with `Kind = plate` is matched from `mymatasan`'s LPR rather than from a
reader on the bus; `Kind = face` likewise from face recognition. **The decision path above
is identical** — only step 1 differs (there is no Secure Channel to a camera; the trust
boundary is the mTLS control channel instead).

This is the cheapest big win in the app: gate access by plate **works the day LPR is
wired in**, with no new decision logic. It is also the feature that makes the suite's
existing camera investment pay for itself twice.

### 5.3 Duress

`DuressPinHash` — conventionally the normal PIN with the last digit incremented — grants
access normally so the coercer sees nothing, and simultaneously raises a **silent** alarm
with `Reason = duress`. Cheap to build, and a genuine differentiator against commodity
controllers. The alarm must not touch the reader's LED or buzzer.

### 5.4 One actuation chokepoint

Every unlock — badge, plate, operator override, schedule, flow, API — goes through a single
audited `Issue`-style call, exactly as `myiotsan` funnels all actuation through
`CommandService.Issue`. That pattern is proven in this codebase and is the only reason
"who opened door 3 at 02:14, and through what path" is answerable. **Do not add a second
path**, however convenient a direct relay call looks during development.

### 5.5 Anti-passback

Needs both `ReaderInId` and `ReaderOutId` populated. Two modes:

- **Soft** — log the violation, grant anyway. The right default; hard APB on a door with a
  flaky contact locks out your entire staff on day one.
- **Hard** — deny. Reserve for `critical` doors, and always pair with a scheduled midnight
  reset so one missed exit swipe does not permanently strand someone.

---

## 6. Phasing

| Phase | Scope |
| --- | --- |
| **P1** | `Holder`, `Credential` (card + PIN), `Door`, `Reader`, `Grant`/`Schedule`, `AccessEvent`, the state machine, offline cache. One door, one reader, working end to end. **Entities, the decision path, Wiegand decode/encode, the door state machine, and the offline-policy logic are built and tested** (`apps/mypintusan/entities`, `apps/mypintusan/services`). **Not done:** persistence (no `Store` implementation but the in-memory test fake), `apps/mypintusan/apis`/`app`/`config.json`/firstrun (no runnable app), the myiotsan relay-actuation and door-contact bindings (`RelayDeviceKey`/`ContactChanged` are unwired seams), and card-then-PIN pairing (PIN-then-card only today). |
| **P2** | Holiday calendar, free-access + first-person-in, lockdown, duress, door-forced/held-open alerts, `myseliasan` adoption so doors appear on the fleet map. Holiday calendar, free-access/first-person-in, lockdown, and duress are already implemented in the P1 decision/state-machine code above; what remains here is wiring them to a real deployment (persistence, myiotsan bindings) plus `myseliasan` adoption. |
| **P3** | Plate and face credentials via `mymatasan`, anti-passback, two-person rule on `critical` doors, visitor management (pre-registration, QR pass, host notification, on-site roster, **evacuation list**). |

The evacuation list in P3 is worth calling out as a sales item: an accurate live roster of
who is inside the building is what sells this to a safety officer, and it falls out of
`AccessEvent` almost for free once door contacts are bound.
