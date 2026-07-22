# Module: apps/myidsan/apis/user_login.go

## Purpose

REST API endpoints for user login credential management.

## Route Group

Base path: `/api/user-credential`

- `GET /api/user-credential`
- `GET /api/user-credential/email`
- `POST /api/user-credential`
- `PUT /api/user-credential`
- `DELETE /api/user-credential/{id}`

## Middleware Contract

Protected by auth middleware + `AccessSessionMidware` + `RequireSuperadmin`. The entire `/api/user-credential` surface is **superadmin-only** — role assignment is a privilege-escalation vector and must not be reachable by any non-superadmin role regardless of matrix grants.

## Handler Behavior

- `post` is the admin-provisioning create path — a role is assigned up front in the
  request body (`userRoleId`), unlike self-registration
  (`/api/login/default/register`), which always lands the new account pending
  (`UserRoleId: 0`). Requires non-empty `email`/`userpwd`; forces `id = 0`,
  `isActive = true`, defaults `createdAt` to now, and stamps `createdBy` from the
  caller's JWT claims when present. Still gated by the surface-wide
  `RequireSuperadmin` middleware below. Used today by the SPA's setup wizard
  ("create your own superadmin" step,
  `views/react-webpack/src/views/components/setup.js`) and is available for the
  Users admin page as well.
- GET supports `limit`, `offset`, `filters`, and `sorters` query parameters.
- Filter and sorter query values use the shared SQL enum JSON contract from `query_options.go`.
- Read handlers return myidsan output DTOs through `IUserLoginDtoService`.
- PUT decodes the myidsan input DTO, then projects it to a `UserLogin` entity for service writes.
- PUT rejects unknown JSON fields.
- PUT blocks self-role-change: if `body.Id` matches `claims.Id` and `body.UserRoleId` differs from `claims.RoleId`, the request is rejected with 403. This prevents a superadmin from accidentally (or maliciously) demoting or escalating their own role.
- `/email` uses the `email` query parameter for exact unique lookup.
- DELETE parses `{id}` from route params.
