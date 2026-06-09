# Module: apps/mymatasan/entities/local_user.go

## Purpose

Defines the standalone local login user persisted by `mymatasan`.

## Notes

- `Username` is unique and used for HTTP Basic Auth login.
- `PasswordHash` is omitted from JSON responses.
- `IsAdmin` gates Settings user-management actions.
- `IsActive` disables login without deleting the row.
- `LastLoginAt`, `CreatedAt`, and `UpdatedAt` support simple local audit metadata.
