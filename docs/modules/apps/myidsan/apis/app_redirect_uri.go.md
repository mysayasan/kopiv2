# Module: apps/myidsan/apis/app_redirect_uri.go

## Purpose

Protected MyIDSan management API for relying-app callback URLs.

## Routes

- `GET /api/app-redirect-uri`: list registered callback URLs.
- `POST /api/app-redirect-uri`: create a callback URL.
- `PUT /api/app-redirect-uri`: update a callback URL.
- `DELETE /api/app-redirect-uri/{id}`: delete a callback URL.

## Security

- Routes use MyIDSan auth and accessrbac middleware (`AccessSessionMidware`). The surface is **RBAC-matrix governed** (delegatable; not granted by default) — a superadmin can grant a role access to manage callback URLs via the permission matrix.
- MyIDSan authorization only accepts active callback URLs from this table.
