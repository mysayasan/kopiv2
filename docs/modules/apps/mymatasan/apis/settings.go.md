# Module: apps/mymatasan/apis/settings.go

## Purpose

Registers runtime settings routes for standalone `mymatasan`.

## Routes

- `GET /api/settings/runtime`: return current decoder and live stream settings.
- `PUT /api/settings/runtime`: save decoder and live stream settings without restart.
- `POST /api/settings/runtime/reset`: restore startup config defaults into the runtime settings row.
- `GET /api/settings/users`: list standalone local users.
- `POST /api/settings/users`: create a standalone local user.
- `PUT /api/settings/users/{id}`: update user profile, admin flag, and active flag.
- `POST /api/settings/users/{id}/password`: reset a local user's password.
- `DELETE /api/settings/users/{id}`: delete a local user.

## Notes

- Routes are mounted behind the app-level local Basic Auth middleware.
- Runtime settings are persisted in SQLite through `RuntimeSetting`.
- User management routes require an authenticated admin local user.
