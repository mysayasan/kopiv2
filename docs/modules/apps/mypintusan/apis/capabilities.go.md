# Module: apps/mypintusan/apis/capabilities.go

## Purpose

Answers **"what may the signed-in role actually do?"** — by asking the same permission matrix that
decides every request.

## Why it exists

This app decided authorization **twice**. The server used the deny-by-default catalog in
`services/rbac.go.md`; the SPA hid Access rules and Settings on a client-side `user.isAdmin` and
offered everything else to everybody. Two mechanisms with one intent, and nothing kept them in
step — the root cause this codebase had already recorded once as *"nav uses isAdmin, server uses
the matrix — two sources of truth"*.

Drift is not cosmetic, and it went wrong in **both directions at once**:

- **The screen offered what the server refuses.** A viewer was shown an Unlock button on every door
  card and an "Add person" button on the People screen. Pressing one produced a bare error, from
  which nobody can tell "I am not allowed" from "this is broken" — while standing at a door.
- **The screen hid what the server allows.** An operator is granted read on groups, grants and
  schedules precisely so they can see the rules they must work within; the rail hid the whole
  section because it was not an admin's.

The fix is not a longer list of `isAdmin` checks in the frontend — that is a second copy of the
policy, which is the defect. It is this: **the client asks the matrix.**

## Route

```
GET /api/auth/capabilities   -> { "unlockDoor": true, "editRules": false, ... }
```

Readable by every signed-in role (`services/rbac.go`'s catalog grants viewer and operator `read`).
A role that cannot ask what it may do gets a UI that guesses.

## The capability table

Each flag names one thing a screen can offer **and the request it would really send**:

| capability | probe |
|---|---|
| `viewDoors` / `viewReaders` / `viewActivity` / `viewPeople` | `GET` on `/api/doors`, `/api/readers`, `/api/events`, `/api/holders` |
| `unlockDoor` | `POST /api/doors/0/unlock` |
| `managePeople` / `issueBadges` | `POST /api/holders`, `POST /api/holders/0/credentials` |
| `viewRules` / `editRules` | `GET` / `POST` on `/api/grants` |
| `lockdown` | `POST /api/lockdown` |
| `viewSettings` / `editSettings` | `GET` / `PUT` on `/api/settings/access` |
| `manageUsers` | `POST /api/settings/users` |
| `createDoors` | `POST /api/doors` |
| `viewAudit` | `GET /api/audit` |

The probe path carries a **placeholder id**. The matrix matches segment-wise, so any id decides the
same way — what matters is that the SHAPE is the shape the browser sends. A probe written
`/api/doors/unlock` would have agreed with the catalog rule that had exactly that mistake in it and
reported a capability nobody had.

`viewRules` and `editRules` are deliberately separate: an operator may read the grants and schedules
they work within and may not touch one, and a rail that collapses the two hides something somebody
was deliberately allowed to see.

## `allows`

Mirrors `NewRequireRolePermission` exactly — a superadmin bypasses the matrix, a user with no role
has nothing, everything else is the matrix's answer. **A lookup that fails answers NO**: a
capability that cannot be established is not a capability, and the middleware would fail closed on
the same request anyway, so saying yes would only produce a button that 403s.

## Registration

Registered on the protected router **before** `sharedapis.NewLocalAuthApi`, which mounts a
subrouter on the `/auth` prefix serving only the two routes it declares. Ordering is the simple way
to keep this route from depending on how a prefix subrouter behaves when none of its children
match. See `app/app.go.md`.

## Consumers

`views/App.js` fetches it alongside the session and renders the navigation rail from it — including
the **Admin trail** section, which is offered only where `viewAudit` says the server would serve it,
so an admin-only screen is hidden by the same matrix that refuses the request; `Doors.js`
(`unlockDoor`, `lockdown`), `People.js` (`managePeople`, `issueBadges`), `Access.js` (`editRules`),
`Readers.js` (`viewSettings`) and `Settings.js` (`manageUsers`) each gate their controls on it.
Change the catalog and the screens follow.
