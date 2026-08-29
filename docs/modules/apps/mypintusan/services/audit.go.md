# Module: apps/mypintusan/services/audit.go

## Purpose

The **administrative trail** — who changed the rules about who gets in. Thin app-level bindings over
`domain/shared/audit`: the action vocabulary, the target types, and the constructors that give the
shared service this app's database and metric names.

## Why it exists

mypintusan already keeps the best log in the suite. `entities.AccessEvent` records every badge
presented at every door — who, where, when, granted or denied, and why. What it did not record is
**who decided they could**.

A grant created at 23:40. A holiday deleted the morning of a shutdown. A perimeter door's offline
policy flipped from `deny` to `cached`. A badge quietly issued to a holder nobody added to the
roster. Every one of those changes what the access log will say tomorrow, and none of them left a
mark that survives.

The gap is sharper here than the identical gap was in mymatasan, **because the access log hides
it**. A grant edit does not look like an incident; it looks like an ordinary badge event three weeks
later, on a door the person was never supposed to reach, with `decision: granted, reason: ok` next
to it. The log answers *"did this happen"* perfectly and *"was this supposed to happen"* not at all.

## What was already there, and why it is not this

`apis/access_rules.go` publishes an `access.rule-change` **notification** on grant, schedule,
holiday and membership edits, naming the administrator. That is a good alert and a bad record:

- the notification feed is a **bounded, evicted stream** meant to be read within minutes (the suite
  has already been bitten once by a diagnostic flood evicting the rows that mattered);
- it covers **only** the access-rules API — not doors, badges, settings, lockdown or accounts;
- it has no filter, no export and no retention policy of its own.

An investigation six months later needs a table. **Both are kept**: the notification is how a change
is noticed now, the trail is how it is proven later.

## The action vocabulary

Constants, not inline strings, so the set stays greppable and a UI filter can offer a closed list.
Convention: `<subject>.<verb>`, `lower_snake` for multi-word verbs.

| group | actions |
|---|---|
| who may enter | `grant.create` `grant.delete` `group.create` `group.delete` `group.member_add` `group.member_remove` `schedule.create` `schedule.delete` `holiday.create` `holiday.delete` |
| people and badges | `holder.create` `credential.issue` `credential.revoke` |
| the estate | `door.create` `door.unlock_remote` |
| safety posture | `lockdown.set` |
| the appliance | `settings.change` `settings.reset` `user.create` `user.update` `user.delete` `user.password_reset` |
| the guarantee | `api.write` |
| retention | `audit.retention_purge` (written by `domain/shared/audit/retention.go` itself) |

**A declared action that nothing emits is a lie this suite has told before.** The same grep found
five dead audit constants on myidsan and three alarms on this app that could never fire (#220). The
failure it produces is specific: somebody filters on `credential.revoke` because the constant
exists, gets nothing back, and concludes no badge was ever revoked — *an absent record and a record
of absence look identical in a query*. `apis/audit_actions_test.go` reads the source and fails the
build if any constant loses its call site, and if any handler emits a name that is not declared.

There is deliberately **no** `auth.password_change` constant: somebody changing their own password
is served by shared code this app does not own, so it is recorded as `api.write` on
`/api/auth/change-password` — thinner, and actually true.

## Notable choices

- **`door.unlock_remote` duplicates the access log on purpose.** A reviewer reading the
  administrative trail is asking "what did the people with power do", and a remote open is squarely
  that; sending them to a second table for the answer is how half a story gets told.
  `entities.AccessEvent` remains the authority on door *decisions*.
- **`door.create` records the security fields**, not just the name. There is no `PUT /api/doors`:
  the offline policy, the cache TTL and whether the reader must speak Secure Channel are decided
  once, at install, and are exactly what decides how the door behaves on the day the network is gone
  and nobody is watching.

## Constructors

```go
NewAuditService(db, logf) IAuditService          // over mypintusan's database
WithAuditMetrics(svc, metrics) IAuditService     // under this app's series names
```

`Record` swallows its own write errors by design — auditing must never fail the action being
audited, and here that action may be somebody standing at a door — which means a broken trail has no
symptom at all. `WithAuditMetrics` is what gives it one: see `metrics.go.md`.

## Related

`apis/audit.go.md` (the read surface, the recording helper and the fallback middleware),
`app/audit_retention.go.md`, `services/rbac.go.md` (the trail is admin-only),
`domain/shared/audit`.
