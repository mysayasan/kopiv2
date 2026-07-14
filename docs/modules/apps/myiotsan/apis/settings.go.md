# Module: apps/myiotsan/apis/settings.go

## Purpose

Registers user and role management under `/api/settings` — closing a real gap: myiotsan's policy
catalog (`services/rbac.go.md`) has named `/api/settings/users` and `/api/settings/roles` since
P0, and the `viewer`/`operator`/`admin` roles have existed since then, but **nothing served these
routes**, so viewer and operator were unassignable and the appliance was effectively
single-admin. A catalog that names routes the app does not serve is a lie an operator would rely
on — exactly the thing the catalog exists to prevent. Found and closed in P4.

Runs on the **shared** appliance user service (`domain/shared/services`) — the same code
`mymatasan` uses — so bcrypt handling, sessions, and the last-admin guard are one implementation,
not two.

## Responsibilities

- `NewSettingsApi(router, users sharedservices.ILocalUserService, roles sharedservices.IAccessRoleService)`
  mounts, all under `/settings`:
  - `GET /roles` — the roles an admin may assign (`viewer`/`operator`/`administrator`).
  - `GET /users`, `POST /users` — list / create a local user
    (`sharedservices.CreateLocalUserRequest`).
  - `PUT /users/{id}` — edit (`sharedservices.UpdateLocalUserRequest`); the shared service
    refuses an edit that would remove the last administrator — an appliance nobody can administer
    is a bricked appliance.
  - `DELETE /users/{id}` — remove.
  - `POST /users/{id}/password` — reset a user's password.
- `requireAdmin` is a **self-gate on top of the matrix**: `services.Policy()` already denies these
  routes to viewer and operator, but this is defence in depth on the one surface in the app that
  can mint an account.

## Notes

- Mounted in `app.go`'s `RegisterAppRoutes` right after `sharedapis.NewLocalAuthApi`, on the same
  `protected` subrouter (auth + RBAC matrix already applied).
- `services.Policy()` already listed `/api/settings/users`/`/api/settings/roles` as admin-only,
  no-grants rows since P0 — this file is what finally makes that catalog entry true rather than
  aspirational.
