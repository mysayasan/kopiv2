# Module: apps/myidsan/dtos/output/user_login.go

## Purpose

Defines the myidsan output DTO for user login responses.

## Fields

Mirrors `entities.UserLogin`. Includes `mustChangePassword bool` — the admin list view uses this field to surface accounts that have not yet completed their first-login password change.

Deliberately has **no `Userpwd`/password field**. `domain/utils/dtos.Project` copies only the fields present on the destination struct, so omitting `Userpwd` here is what keeps the stored bcrypt hash out of every response built from this DTO — notably the superadmin-only `GET /api/user-credential` account listing, which previously leaked every local account's password hash to the client. Write operations are unaffected: they go through `dtos/input.UserLoginDto`, which still carries `Userpwd`.
