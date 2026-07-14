# Module: apps/mymatasan/services/local_user.go

## Purpose

No longer the implementation. The appliance user/login model now lives in
`domain/shared/services` (`local_user.go` + `local_user_types.go`) so mymatasan and myiotsan
run the SAME security-critical code — bcrypt handling, session comparison, the bcrypt
verification cache, the last-admin guard — rather than two copies that drift. This file is
now **re-export aliases only**, keeping mymatasan's existing call sites (handlers, other
services, tests) unchanged.

## Contents

```go
type (
    AuthenticatedUser              = sharedservices.AuthenticatedUser
    ILocalUserService               = sharedservices.ILocalUserService
    AdminSeedResult                 = sharedservices.AdminSeedResult
    CreateLocalUserRequest          = sharedservices.CreateLocalUserRequest
    UpdateLocalUserRequest          = sharedservices.UpdateLocalUserRequest
    ChangeLocalUserPasswordRequest  = sharedservices.ChangeLocalUserPasswordRequest
    ResetLocalUserPasswordRequest   = sharedservices.ResetLocalUserPasswordRequest
)

var NewLocalUserService = sharedservices.NewLocalUserService

var (
    ErrLocalUserInvalidCredential = sharedservices.ErrLocalUserInvalidCredential
    ErrLocalUserInactive          = sharedservices.ErrLocalUserInactive
)
```

## Notes

- See `domain/shared/services/local_user.go.md` and
  `domain/shared/services/local_user_types.go.md` for the real behavior documentation
  (`EnsureDefaultAdmin`/`ResetAdmin` bootstrap/recovery, the auth cache, the last-admin guard,
  `BackfillRoles`, etc.) — everything below this file's line count now lives there.
- This move is behavior-preserving for mymatasan: verified by booting on a fresh DB — the
  forced-change gate still returns `password_change_required`, the login probe still issues a
  cookie still named `mymatasan_local_auth`, cookie-only auth still works, a bad credential is
  still `401`, and the role ladder still holds.
- This service is intentionally separate from MyIDSan identity and RBAC services.
