# Module: domain/shared/services/ifaces.go

## Purpose

Defines shared service interfaces consumed by API handlers and runtime wiring.

## Responsibilities

- Defines endpoint, API log, app registry, cache, runtime log, and file storage service contracts.
- Defines `IAccessRoleService` for the shared accessrbac role catalog (CRUD + EnsureBuiltins).
- Defines `IAccessPermissionService` for the per-role endpoint permission matrix (Authorize, ListForRole, Set, Delete, EnsureViewerDefaults).
- Defines `AccessPrincipal` — the app-agnostic view of an authenticated user (RoleId, Disabled, MustChangePassword).
- Defines `AccessUserResolver` — the interface apps implement to adapt their own user store to an `AccessPrincipal` (used by `AccessSessionMidware`).
- Defines `FileStorageUpload` for staged file metadata plus temp/final path handoff.
- Defines `FileStorageDownload` for metadata plus bytes ready to stream.
- Defines `FileStorageDownloadActor` for optional caller identity during file access checks.
- Exposes `StoreUploads` so file uploads can use the coordinated transaction workflow behind the service boundary.
- Exposes `DownloadById` and `DownloadByIds` so APIs can download by metadata IDs while GUIDs remain internal.
- Exposes `EnqueueUploads`, `GetUploadJob`, `ProcessUploadJobs`, and `RecoverStaleUploadJobs` for the durable async upload boundary.
- Exposes `SweepExpiredFiles` so the runtime scheduler can remove expired physical files and metadata.

## Removed (accessrbac migration)

- `IApiEndpointRbacService`, `IApiEndpointRbacDtoService`, and `IUserRoleService` / `IUserRoleDtoService` (myidsan) are removed; the old app-code-scoped RBAC service is superseded by `IAccessRoleService` + `IAccessPermissionService`.

## Notes

- API layers depend on these interfaces rather than concrete service implementations.
- Apphost depends on the same interface for the backend upload worker, keeping scheduler wiring outside the concrete service type.
- Download actors are nullable so public downloads can flow without authentication while protected file levels still fail closed in the service.
