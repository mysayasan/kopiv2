# Module: apps/myidsan/services/user_role.go

## Status

**Retired.** This file was deleted in the accessrbac migration. The myidsan-specific `IUserRoleService` (which managed per-app `user_role` rows) has been removed. Role management is now handled by the shared accessrbac service (`domain/shared/services/access_rbac.go`), which the myidsan `app.go` accesses via `deps.AccessRoles`.
