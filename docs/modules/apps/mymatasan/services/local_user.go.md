# Module: apps/mymatasan/services/local_user.go

## Purpose

Implements standalone DB-backed user management for `mymatasan`.

## Responsibilities

- Seeds `admin` / `Admin123` on first startup when no local users exist.
- Hashes local passwords with bcrypt.
- Authenticates Basic Auth credentials and DB-backed auth cookies.
- Lists, creates, updates, resets passwords, and deletes local users.
- Prevents deleting, disabling, or demoting the last active admin user.

## Notes

- This service is intentionally separate from MyIDSan identity and RBAC services.
