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

## Notes

- The Redis or memory coordinator serializes file-storage critical sections.
- The DB transaction remains request-scoped; it is not stored on the shared DB adapter.
- The upload batch is treated as all-or-nothing after files are staged.
- Async upload jobs keep staged files while retryable so the backend worker can resume without asking the client to upload again.
- Exhausted jobs clean staged and final paths before moving to `failed`.
- Existing metadata by GUID is reused during retry so a recovered job does not insert duplicate file rows.
- Download APIs use metadata IDs externally; GUIDs are only used by the service to resolve physical file paths.
- `SystemOnly` requires a service actor, `Group` compares owner and actor role groups, `Role` accepts the owner's role or ancestor roles, and `Public` accepts any caller.
- Expired files are denied on download even before the scheduled cleanup removes them.
