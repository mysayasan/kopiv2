# Module: apps/myidsan/apis/app_redirect_uri.go

## Purpose

Protected MyIDSan management API for relying-app callback URLs.

## Routes

- `GET /api/app-redirect-uri`: list registered callback URLs.
- `POST /api/app-redirect-uri`: create a callback URL.
- `PUT /api/app-redirect-uri`: update a callback URL.
- `DELETE /api/app-redirect-uri/{id}`: delete a callback URL.

## Security

- Routes use MyIDSan auth and RBAC middleware.
- MyIDSan authorization only accepts active callback URLs from this table.
