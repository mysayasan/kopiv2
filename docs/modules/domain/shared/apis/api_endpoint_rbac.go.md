# Module: domain/shared/apis/api_endpoint_rbac.go

## Status

**Retired.** This file was deleted in the accessrbac migration. The legacy `/api/endpoint-rbac` route group (and the `/api/sso/authorize` service-to-service RBAC endpoint) have been removed.

Role and permission management is now handled by the shared accessrbac surface at `/api/access-rbac` (`domain/shared/apis/access_rbac.go`). The `POST /api/sso/authorize` endpoint (which returned app-scoped RBAC decisions) has been removed entirely; relying apps now use the accessrbac middleware directly rather than calling back to myidsan for authorization decisions.

See `docs/modules/domain/shared/apis/access_rbac.go.md` for the replacement.
