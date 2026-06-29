# Module: domain/shared/services/api_endpoint_rbac.go

## Status

**Retired.** This file was deleted in the accessrbac migration. The `IApiEndpointRbacService`, its DTO service, and the `ApiEndpointRbac` entity have all been removed. Authorization is now performed by the shared accessrbac core (`domain/shared/services/access_rbac.go`), which uses the `access_role_permission` table instead of the old `api_endpoint_rbac` table.

See `docs/modules/domain/shared/services/access_rbac.go.md` for the replacement.
