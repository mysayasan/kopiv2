# Module: domain/shared/services/file_storage.go

## Purpose

Provides file storage metadata operations and the coordinated upload transaction workflow.

## Responsibilities

- Read metadata by GUID.
- Read stored file content by metadata ID.
- Read multiple stored files by metadata IDs for ZIP streaming.
- Enforce file download security level and expiry before reading the physical file.
- Sweep expired files by removing the physical GUID object and deleting metadata.
- Create metadata through the generic repository for simple callers.
- Store staged upload batches with FIFO transaction locking.
- Enqueue staged upload batches as durable `OperationJob` rows.
- Process queued/retrying upload jobs in FIFO order.
- Recover stale running upload jobs by deadline and retry/fail them based on attempt count.
- Open request-scoped DB transactions through `ScopedTxStarter`.
- Insert metadata and copy staged files into final GUID paths through an atomic final-path swap.
- Roll back DB changes and delete staged/final files when a coordinated upload fails.

## Download Authorization (accessrbac migration)

The `Group` and `Role` security levels now collapse to an owner-or-superadmin check: a file is accessible if the requesting actor's `UserId` matches the file's `CreatedBy`, or if the actor belongs to an accessrbac superadmin role (checked via the injected `IAccessRoleService`). The old group-hierarchy and role-ancestor comparisons have been removed. The `WithFileStorageAccessRoles` option injects the role service; when omitted, only the owner check applies.

Security levels summary after migration:

- `SystemOnly`: requires `actor.IsSystem = true`.
- `Group` / `Role`: owner (`actor.UserId == CreatedBy`) or superadmin role.
- `Public`: any caller (no actor required).

## Notes

- The Redis or memory coordinator serializes file-storage critical sections.
- The DB transaction remains request-scoped; it is not stored on the shared DB adapter.
- The upload batch is treated as all-or-nothing after files are staged.
- Async upload jobs keep staged files while retryable so the backend worker can resume without asking the client to upload again.
- Exhausted jobs clean staged and final paths before moving to `failed`.
- Existing metadata by GUID is reused during retry so a recovered job does not insert duplicate file rows.
- Download APIs use metadata IDs externally; GUIDs are only used by the service to resolve physical file paths.
- Expired files are denied on download even before the scheduled cleanup removes them.
