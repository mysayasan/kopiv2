# Module: apps/mypintusan/apis/users.go

## Purpose

User and role management for the appliance: who may sign in to this controller, and as what.

## Why it exists

**This closed a gap that made the app's entire role model unreachable.** `services/rbac.go` seeds
three roles on every boot and spends thirty lines reasoning about the line between them — who may
change the rules about who gets in, credentials operator-level, grants and lockdown admin-only. And
nothing served a route that could put a person in one of those roles: there was no user API and no
Users screen, so a mypintusan appliance had exactly **one account**, the bootstrap admin, and no
way to make a second. Every access decision the catalog draws was theoretical.

`myiotsan` had the identical gap and closed it the same way (`apps/myiotsan/apis/settings.go`).

## Routes

```
GET    /api/settings/roles              the roles an admin may assign
GET    /api/settings/users              list
POST   /api/settings/users              create
PUT    /api/settings/users/{id}         rename / re-role / activate
DELETE /api/settings/users/{id}         remove
POST   /api/settings/users/{id}/password  reset somebody else's password
```

All admin-only. `/api/settings/users` and `/api/settings/roles` each have their **own** catalog row
— deeper than `/api/settings` and therefore more specific — so minting an account stays denied even
if the settings grant is widened later.

## `requireAdmin`

A self-gate on top of the matrix. The matrix already denies these routes to viewer and operator;
this is defence in depth on the one surface that can mint an account with any power on the
appliance, **including the power to open every door**.

## Notes

- Runs on the shared appliance user service (`domain/shared/services/local_user.go`), so bcrypt,
  session hashing and the **last-administrator guard** are one implementation rather than five. On a
  door controller that guard matters more than usual: an appliance nobody can administer is one
  where nobody can lift a lockdown.
- `Update` and `Delete` surface the shared service's refusal verbatim — its messages are written for
  this reader.
- A created user is **not** flagged must-change unless the request says so; the create form on the
  Settings screen sets a password directly.
- The UI is `views/Settings.js`'s `UsersSection`, rendered only when
  `GET /api/auth/capabilities` reports `manageUsers` — see `apis/capabilities.go.md`.
- The role picker defaults to the **last** role the appliance lists rather than to an admin: a form
  that defaults to the most powerful role is how an operator account becomes an administrator by
  nobody's decision.

## The administrative trail

`user.create`, `user.update`, `user.delete` and `user.password_reset` are written to the append-only
trail (`apis/audit.go.md`).

Auditing matters more on this surface than anywhere else in the app: it can mint an account holding
every other power on the appliance, including the power to open every door and to change who else
may. An account created at 02:00 and deleted at 02:20 leaves nothing behind but the doors it opened
in between.

- **The role is the payload.** "A user was created" is administrative noise; *"a user was created as
  an administrator"* is somebody handing out the keys to the building.
- **An update reads the before state**, so a role change reads as `moved from role 3 to role 1`
  rather than as an unexplained edit. It pages the user list to do it (`lookup`) because the shared
  appliance user service exposes no by-id read, and adding one to a service four apps depend on is
  not this feature's business; a door appliance has a handful of accounts, not a directory, and a
  miss still writes the entry, just with less in it.
- **A delete reads the account before removing it**, because afterwards there is nothing left to
  name and `user 4 deleted` is the entry that makes an investigation give up.
- **A password reset by an administrator is a takeover of that account**, however legitimate the
  reason. The password itself is never recorded.
