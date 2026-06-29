# Module: apps/myidsan/apis/user_role.go

## Status

**Retired.** This file was deleted in the accessrbac migration. The legacy `/api/user-credential` (user role management) endpoint group has been removed from myidsan. The "Roles" nav item in the myidsan SPA now points to the shared accessrbac management surface (`/api/access-rbac/roles`), which provides role CRUD and the per-role permission matrix editor.

The `/api/user-credential` path that still appears in the endpoint catalog seeds is the **Users** admin surface (user login management), served by `apps/myidsan/apis/user_login.go`.
