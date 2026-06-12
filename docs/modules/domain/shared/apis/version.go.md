# Module: domain/shared/apis/version.go

## Purpose

Exposes the shared runtime version endpoint.

## Route Group

Base path: `/api/version`

- `GET /api/version`

## Middleware Contract

- The route is mounted under the `/api` router so API activity logging still applies.
- The route does not use auth or RBAC so clients can check versions before login.

## Handler Behavior

- Returns the selected app name, app version, core version, commit, and update timestamp.
- Uses the standard shared `DefaultResponse` JSON shape.
