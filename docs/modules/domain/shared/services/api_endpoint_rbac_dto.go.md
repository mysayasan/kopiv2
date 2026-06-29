# Module: domain/shared/services/api_endpoint_rbac_dto.go

## Status

**Retired.** This file was deleted in the accessrbac migration. DTO projection for authorization decisions is no longer needed at this level; the accessrbac permission service (`domain/shared/services/access_rbac.go`) returns plain `AccessRolePermission` entities directly. The SPA fetches the caller's permission matrix via `GET /api/access-rbac/me`.
