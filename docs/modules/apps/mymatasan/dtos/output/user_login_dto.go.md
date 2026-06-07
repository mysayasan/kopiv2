# Module: apps/mymatasan/dtos/output/user_login_dto.go

## Purpose

Defines the app-local user-login response DTO.

## Notes

- Mirrors the shared `UserLogin` fields needed by the app user-login API.
- Omits `userpwd` so app-local user-login responses do not expose password hashes.
