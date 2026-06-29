# Module: domain/utils/middlewares/rbac.go

## Status

**Retired.** This file was deleted in the accessrbac migration. The legacy `RbacMidware` (app-code-scoped, cache-backed endpoint allow-list) has been replaced by `AccessSessionMidware` in `domain/utils/middlewares/access_rbac.go`.

The host-matching and path-matching helpers that lived in `rbac.go` have been extracted to `domain/utils/middlewares/match.go` and are still used by the rate-limit middleware.

See `docs/modules/domain/utils/middlewares/access_rbac.go.md` for the replacement.
