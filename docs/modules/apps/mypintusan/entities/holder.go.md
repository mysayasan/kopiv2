# Module: apps/mypintusan/entities/holder.go

## Purpose

A person who may go through a door. Holders live in `mypintusan`, not `myidsan`, because most
people who badge never sign into any app — contractors, cleaners, delivery drivers, visitors,
and the majority of floor staff. Forcing every one of them into the identity broker to hold a
card would fill it with thousands of non-users that RBAC, MFA policy and session management
would then have to reason about, and would drag a clean intranet SSO binary into the life-safety
blast radius. See `docs/MYPINTUSAN_DATA_MODEL.md` §1 for the full argument.

## Fields

- Identity: `Id`, `Ref` (the site's OWN reference — staff number, contractor id — and the `ukey`,
  because that is what an HR export or a visitor book joins on; `Id` means nothing outside this
  database), `Name`, `Kind` (`staff`/`contractor`/`visitor`/`service` — drives defaults and
  reporting, not permissions; permissions come from group membership).
- `SsoUserId` — a nullable link to a `myidsan` user, `0` for the majority who have no login.
  **STRICT id match only, never a fallback to email.** This is a direct carry-over of the
  privilege-escalation bug already fixed in the suite's federated-user dedup (see the repo's
  `federated-user-dedup-security` history): email-fallback matching was removed there because
  `myidsan` can emit a non-unique email. Re-introducing it here would let one person's card bind
  to another person's identity — which in this app is a door opening for the wrong human.
- `Status` — `active`/`suspended`/`terminated`. Suspension is reversible and keeps the audit
  trail attached; deleting a holder to revoke access would erase who went where.
  `ValidFrom`/`ValidUntil` bound when the holder is current (`0` = no expiry).
- `OfflineAllowed` — permits this holder through a `critical` door while its controller is
  running on cached data. Default `false`: an explicit, auditable grant, not a convenience.
- `ExtendedUnlock` — gives this holder the door's longer strike time. Modelled on the holder
  rather than configured per door so the accessibility provision follows the person around the
  site instead of being re-entered at every opening.
- Audit fields: `Notes`, created/updated user and timestamps.

## Notes

- **Not yet persisted.** `apps/mypintusan/entities` has no repository, no dbsql registration, and
  no migration — this struct is a data shape consumed only by in-memory test fakes
  (`services.Store` in `controller_test.go`). Nothing here survives a restart yet.
- Consumed by the decision path (`services/decision.go.md`) as `Snapshot.Holder`, and by
  `services.Controller` (`services/controller.go.md`) via `Store.Holder`.
