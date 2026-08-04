# MyPintuSan — Hardware Compatibility & Reference Kit

Status: **DRAFT — design only, nothing built.** This document covers *only* the hardware
seam: how a reader model is described (`ReaderProfile`), how much a given profile is
trusted, and the reference bill of materials the app is tested against. None of that —
`ReaderProfile`, the trust-rule enforcement, the reference kit — is implemented, and there is
still no `apps/mypintusan`. The app-level plan (credential store, door state machine, schedules,
anti-passback, lockdown, visitor management) is not yet written. What *is* now built, one layer
down, is the OSDP wire protocol this hardware plan assumes: `infra/access/osdp` (CP driver) and
`tools/osdp-sim` (PD simulator), covering build order steps 1–4 of 6 in
[`MYPINTUSAN_OSDP_PLAN.md`](MYPINTUSAN_OSDP_PLAN.md) §5. Steps 5–6 (serial transport, a real
reader on the bench) still need hardware, so nothing in this document has been validated against
an actual reader.

`mypintusan` is the proposed fifth app in the suite: **physical access control**. It
decides who goes through a door, where `myidsan` decides who signs into software. It is
the first app in the suite whose failure mode is a *person stuck behind a door*, which is
why the trust model and the life-safety wiring are specified before the driver.

---

## 1. Principle: recommend, don't require

OSDP is an open SIA standard, so the honest product claim is **"any OSDP v2.x reader"**,
exactly as `mymatasan` claims "any ONVIF camera" rather than a model list. But ONVIF
conformance is uneven in practice and OSDP will be no different, so the claim needs a
second half: **"and here is what we have actually put on a bench."**

This is the same problem `myiotsan` already solved. From
[`device_profile.go`](../apps/myiotsan/entities/device_profile.go):

> *"the abstraction mymatasan does not have, and it is what makes onboarding scale past a
> demo — without it, every one of a hundred identical door sensors would be configured by
> hand."*

`ReaderProfile` copies that shape deliberately: a seeded builtin catalog
(cf. [`profile_catalog.go`](../apps/myiotsan/services/profile_catalog.go)) plus
import/export (cf. [`profile_transfer.go`](../apps/myiotsan/services/profile_transfer.go)),
so an operator with a reader we have never seen can author a profile, run it, and send it
back to us. Good ones get promoted to builtin. **The compatibility list grows without our
labour.**

Keep the *verified* list short. Every model on it is a model we own, re-bench on firmware
changes, and support indefinitely. "We tested these three" is a stronger claim from a small
vendor than "supports 200+ devices", which every commodity vendor asserts and no buyer
believes.

### 1.1 Verification levels

Four levels, ordered. This is the field that does the real work — it is *not* the same as
`Builtin`, and conflating them is the mistake to avoid.

| Level | Meaning |
| --- | --- |
| `unverified` | Community-authored. Never benched by us, never attested by anyone. |
| `community` | Another operator reports it working in production. Self-attested, unaudited. |
| `verified` | We put it on a bench and recorded the firmware version it passed on. |
| `reference` | It is in the reference kit (§4). Re-tested every release. |

`Builtin` stays orthogonal and means only "shipped in the binary, cannot be deleted" — a
builtin profile may still be `unverified` if we seeded it from a datasheet without owning
the hardware. Say so rather than implying a bench test we never ran.

---

## 2. The `ReaderProfile` model

Mirrors `DeviceProfile`'s conventions (struct tags, `Slug` as the stable `ukey`, `Builtin`
guard). Proposed home: `apps/mypintusan/entities/reader_profile.go`.

```go
package entities

// ReaderProfile is a template for a reader TYPE: how to talk to it, what it can do, and
// how far we trust it. It is to mypintusan what DeviceProfile is to myiotsan — the thing
// that stops a hundred identical doors being configured by hand — with one addition that
// myiotsan does not need: a trust level, because a wrong reader binding does not produce a
// bad reading, it produces a door that opens for the wrong person.
type ReaderProfile struct {
	Id   int64  `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	Slug string `json:"slug" form:"slug" query:"slug" ukey:"slug" validate:"required"`
	Name string `json:"name" form:"name" query:"name" validate:"required"`

	// Vendor/Model/Description are for the human choosing a profile. Model is separate from
	// Name because firmware behaviour tracks the model, not our label for it.
	Vendor      string `json:"vendor" form:"vendor" query:"vendor"`
	Model       string `json:"model" form:"model" query:"model"`
	Description string `json:"description" form:"description" query:"description"`

	// Transport selects the driver: "osdp" (the standard, and the only one at launch),
	// later "wiegand", "zkteco", "hikvision-isapi". Same "declare once, don't add an
	// entity per driver" pattern as DeviceProfile.Transport.
	Transport string `json:"transport" form:"transport" query:"transport" validate:"required"`

	// --- OSDP transport ---------------------------------------------------------------

	// OsdpBaud is the line rate. Readers overwhelmingly ship at 9600; 115200 is common once
	// configured. 0 defaults to 9600.
	OsdpBaud int `json:"osdpBaud" form:"osdpBaud" query:"osdpBaud"`
	// OsdpDefaultAddress is the PD address the reader ships with (almost always 0). It is a
	// PROFILE hint for onboarding, not the live address — the live address lives on the
	// reader instance, because a multi-drop bus cannot have two PDs at 0.
	OsdpDefaultAddress int `json:"osdpDefaultAddress" form:"osdpDefaultAddress" query:"osdpDefaultAddress"`
	// SupportsSecureChannel declares the reader can do OSDP Secure Channel (AES-128). This
	// is a CAPABILITY CLAIM by the profile; whether SC is REQUIRED is a per-door policy
	// (§3.2), never a property of the reader.
	SupportsSecureChannel bool `json:"supportsSecureChannel" form:"supportsSecureChannel" query:"supportsSecureChannel"`
	// ShipsWithDefaultSCBK marks readers that come with the well-known install-mode base
	// key. Those readers are NOT secure until rekeyed, so the UI must nag until it happens.
	ShipsWithDefaultSCBK bool `json:"shipsWithDefaultScbk" form:"shipsWithDefaultScbk" query:"shipsWithDefaultScbk"`

	// --- Capabilities -----------------------------------------------------------------
	// These drive the UI: do not offer "card + PIN" on a reader with no keypad.

	HasKeypad    bool `json:"hasKeypad" form:"hasKeypad" query:"hasKeypad"`
	HasBiometric bool `json:"hasBiometric" form:"hasBiometric" query:"hasBiometric"`
	HasTamper    bool `json:"hasTamper" form:"hasTamper" query:"hasTamper"`
	// HasLedControl / HasBuzzerControl gate the feedback commands. A reader that ignores
	// them silently is worse than one that declares it cannot: the operator sees no red
	// flash on deny and assumes the system is broken.
	HasLedControl    bool `json:"hasLedControl" form:"hasLedControl" query:"hasLedControl"`
	HasBuzzerControl bool `json:"hasBuzzerControl" form:"hasBuzzerControl" query:"hasBuzzerControl"`

	// CardFormats are the credential encodings this reader emits, most-preferred first —
	// e.g. "wiegand26", "wiegand34", "raw-uid", "desfire-ev2", "seos". Stored as a JSON
	// array. A reader emitting raw UIDs only is a SECURITY NOTE, not just a format note:
	// UID-only credentials are cloneable, so §3.1 caps where they may be used.
	CardFormats string `json:"cardFormats" form:"cardFormats" query:"cardFormats"`

	// --- Trust ------------------------------------------------------------------------

	// Verification is one of: unverified | community | verified | reference (§1.1).
	Verification string `json:"verification" form:"verification" query:"verification" validate:"required"`
	// VerifiedFirmware and VerifiedAt record WHAT we benched. A verification with no
	// firmware string is worthless — vendors change reader behaviour in point releases.
	VerifiedFirmware string `json:"verifiedFirmware" form:"verifiedFirmware" query:"verifiedFirmware"`
	VerifiedAt       int64  `json:"verifiedAt" form:"verifiedAt" query:"verifiedAt"`
	// SourceUrl points at the datasheet or the contributor's notes, so a future maintainer
	// can tell a tested profile from a transcribed one.
	SourceUrl string `json:"sourceUrl" form:"sourceUrl" query:"sourceUrl"`

	// Builtin marks a shipped profile: usable and copyable, never deletable, so a site
	// cannot break its own onboarding by tidying up. Orthogonal to Verification.
	Builtin   bool  `json:"builtin" form:"builtin" query:"builtin"`
	CreatedBy int64 `json:"createdBy" form:"createdBy" query:"createdBy"`
	CreatedAt int64 `json:"createdAt" form:"createdAt" query:"createdAt"`
	UpdatedBy int64 `json:"updatedBy" form:"updatedBy" query:"updatedBy"`
	UpdatedAt int64 `json:"updatedAt" form:"updatedAt" query:"updatedAt"`
}
```

---

## 3. Trust rules

### 3.1 Door classes vs verification

Every door carries a `DoorClass`. The pairing of class and profile verification is enforced
server-side at bind time — not merely warned about in the UI.

| | `interior` | `perimeter` | `critical` |
| --- | --- | --- | --- |
| `unverified` | ⚠️ allowed, persistent banner | ❌ blocked | ❌ blocked |
| `community` | ✅ | ⚠️ allowed, banner | ❌ blocked |
| `verified` | ✅ | ✅ | ✅ (Secure Channel required) |
| `reference` | ✅ | ✅ | ✅ (Secure Channel required) |

Two extra caps, independent of the table:

- A profile whose only `CardFormats` entry is `raw-uid` is capped at `interior` regardless
  of verification. UID-only credentials clone with a £20 phone app; verification of the
  *driver* says nothing about the weakness of the *credential*.
- `ShipsWithDefaultSCBK` + not yet rekeyed is capped at `interior` until the rekey lands.

### 3.2 Secure Channel is a door policy, not a reader property

The profile *declares* `SupportsSecureChannel`. The **door** decides `RequireSecureChannel`
(default: on for `perimeter` and `critical`, off for `interior`).

**The security-critical rule:** if a door requires Secure Channel and the session fails to
establish — or drops mid-session — the reader is taken **out of service and alarmed**. It
does not silently fall back to cleartext. A downgrade-to-plaintext fallback is exactly the
attack an RS-485 tap wants, and "the door kept working" is how it goes unnoticed for a year.

This mirrors the fail-closed posture already taken in the suite for the
`GetById` superadmin guard: when the trust input is missing, refuse rather than assume.

### 3.3 Actuation goes through one chokepoint

Every unlock — badge, operator override, schedule, flow, API — routes through a single
audited `Issue`-style call, the way `myiotsan` funnels all actuation through
`CommandService.Issue`. That pattern is already proven in this codebase and it is the only
thing that makes "who opened door 3 at 02:14" answerable. Do not add a second path.

---

## 4. Reference kit (BOM)

Indicative SEA sourcing (AliExpress / Shopee), 2026. **Prices need verifying before
publication** — they are here to size the kit, not to quote it.

| # | Item | Notes | ≈ USD |
| --- | --- | --- | --- |
| 1 | USB→RS-485 adapter ×**2** | See §5.1 — you need *two buses*. Isolated preferred. | 20–30 |
| 2 | OSDP 13.56 MHz reader | Must support OSDP Secure Channel. This is the device under test. | 40–60 |
| 3 | Modbus RTU relay board, 4-ch | RS-485. Drives the strike. Already supported by `myiotsan`'s Modbus write path. | 10–15 |
| 4 | **Fail-secure** electric strike, 12 V DC | See §6 — fail-secure, *not* a maglock, for the kit. | 15–40 |
| 5 | Access-control PSU, 12 V 3–5 A, battery backup | Separate lock/reader rails (§5.3). | 25–45 |
| 6 | Reed door-position contact | Feeds `myiotsan` → "door forced" / "door held open". | 3 |
| 7 | REX button (or PIR) | Request-to-exit. Hardware, in the safety chain. | 5–15 |
| 8 | Emergency break-glass release | Hardware. Non-negotiable on the shipped diagram. | 10 |
| 9 | 1N4004 diode + 120 Ω resistors ×2 | Flyback + bus termination. Pennies, and §5.2/§5.4 explain why they are not optional. | 2 |
| 10 | Shielded twisted pair, 2×2 core | Reader bus + relay bus. | 10 |

**≈ $140–230** for a complete, code-plausible door. Cheap enough to keep one permanently on
the bench as a release gate, and to ship to a prospect so the demo cannot fail on their
hardware. Worth listing on the site as a **Compatibility** page: BOM, wiring diagram,
verified list, and the "any OSDP v2.x" statement.

---

## 5. Wiring

### 5.1 Two RS-485 buses — do not share one

**This is the gotcha that will otherwise cost a day.** OSDP and Modbus RTU are both
polled master/slave protocols with incompatible framing. Putting an OSDP reader and a
Modbus relay board on the same pair means two masters transmitting into each other: frames
collide, CRCs fail intermittently, and it looks like a cabling fault. Use two adapters.

```
                 ┌──────────────────────────────┐
                 │        mypintusan host       │
                 │   (Pi / mini-PC / NUC)       │
                 └───┬──────────────────────┬───┘
                     │ USB                  │ USB
              ┌──────┴──────┐        ┌──────┴──────┐
              │ RS-485 #1   │        │ RS-485 #2   │
              │  (OSDP CP)  │        │ (Modbus RTU)│
              └──┬───────┬──┘        └──────┬──────┘
             A/B │       │ 120Ω             │ A/B
        ┌────────┴──┐    ┴             ┌────┴─────────┐
        │ OSDP      │                  │ 4-ch relay   │
        │ reader    │ 120Ω at the      │ board        │
        │ (PD addr) │ far end too      └────┬─────────┘
        └───────────┘                       │ dry contact
                                            ▼  (see §6)
```

Multi-drop: further readers hang off bus #1, each with a **distinct PD address**. Readers
almost always ship at address 0, so address assignment is a real onboarding step, not an
afterthought — the wizard has to handle "two brand-new readers on one bus".

### 5.2 Termination

120 Ω across A/B at **both physical ends** of each bus, and nowhere else. Shielded twisted
pair; ground the shield at **one end only** (grounding both ends creates a ground loop that
shows up as random CRC failures on long runs).

### 5.3 Power

Separate rails for locks and readers, or at minimum separately fused runs from the PSU. An
electric strike draws a large inrush on energise; sharing a rail browns out the reader
mid-transaction and produces "the reader randomly reboots" reports that look like a firmware
bug. Battery backup on the PSU — a door that loses its brain in a power cut is an incident.

### 5.4 Flyback diode

A strike or maglock is an **inductive load**. Without a flyback diode across the coil
(1N4004, cathode to +12 V), the back-EMF on de-energise welds relay contacts over time and
injects noise onto the RS-485 pairs. Fit it at the lock, not at the relay board. AC locks
need an RC snubber or MOV instead.

---

## 6. The life-safety chain — read before wiring anything

**Free egress must be implemented in hardware. Software must never be the only thing that
can open a door.** A panic in a Go goroutine must not be able to trap a person in a
stairwell. This constrains the diagram we ship, so it is decided here rather than later.

### 6.1 Fail-secure (electric strike) — the reference kit default

Locked when unpowered; energise to unlock. Egress is **mechanical**: the inside lever or
panic bar always retracts the latch, with no electricity involved. This is why the kit uses
a strike — the life-safety burden sits on door hardware, not on our wiring.

```
  +12V ──[fuse]──┬── relay NO (mypintusan energises to unlock) ──┬── STRIKE ──┬── 0V
                 │                                               └──►|────────┘
                 │                                                 1N4004
   Inside lever/panic bar = mechanical free egress at ALL times, powered or not.
   REX button → shunts the door-forced alarm (it does not need to unlock anything).
```

### 6.2 Fail-safe (maglock) — heavier code exposure

Unlocked when unpowered; energise to **lock**. A maglock has **no** mechanical egress, so
every safety device must be able to cut its power *without software*, wired in **series** on
the lock feed. Any one opening drops the door.

```
  +12V ──[fire alarm relay NC]──[break-glass NC]──[REX NC]──[relay NC]── MAGLOCK ── 0V
             ▲                       ▲                ▲          ▲
             │                       │                │          └─ mypintusan: one more
             │                       │                │             switch in the chain,
             │                       │                │             never the only one
             └── alarm trips ────────┴── glass broken ┴── exit pressed → power drops → open
```

Do not ship a maglock diagram without a competent local sign-off. Egress and door-hardware
requirements are set by building code — Malaysia's UBBL and BOMBA requirements, NFPA 101 /
IBC elsewhere — and they vary by occupancy type and door location. **Our documentation
should state the interlock requirement and explicitly defer specification to the local
authority**, not attempt to summarise code we are not qualified to interpret.

---

## 7. Open questions

1. **Host placement.** RS-485 reaches ~1200 m at low baud, so one `mypintusan` box per
   building is plausible. Is the unit of deployment a building or a site? This decides
   whether doors are a flat list or nest under the existing `myseliasan` site/building tree.
2. **Offline behaviour.** If `myidsan` is unreachable, does a door fall back to a locally
   cached credential set? Almost certainly yes — but the cache TTL, and whether it fails
   open or closed per door class, is a policy decision with real consequences.
3. **Credential store ownership.** Do cards live in `myidsan` alongside identities (one
   person, one record) or in `mypintusan` (keeps the identity broker out of life-safety)?
   Leaning toward `mypintusan` holding card→person bindings and `myidsan` remaining the
   person authority.
4. **Verified-list maintenance.** Who re-benches on firmware releases, and how does a
   profile get demoted when a vendor breaks it?
5. **Reference-kit sourcing.** Whether to stock/drop-ship the kit, or publish the BOM and
   let buyers source it. The former fits the existing OEM/Fleet tier on the pricing page.
