# Module: domain/shared/services/ifaces.go

## Purpose

Defines shared service interfaces consumed by API handlers and runtime wiring.

## Responsibilities

- Defines user, role, endpoint, API log, cache, runtime log, and file storage service contracts.
- Defines `FileStorageUpload` for staged file metadata plus temp/final path handoff.
- Defines `FileStorageDownload` for metadata plus bytes ready to stream.
- Defines `FileStorageDownloadActor` for optional caller identity during file access checks.
- Exposes `StoreUploads` so file uploads can use the coordinated transaction workflow behind the service boundary.
- Exposes `DownloadById` and `DownloadByIds` so APIs can download by metadata IDs while GUIDs remain internal.
- Exposes `EnqueueUploads`, `GetUploadJob`, `ProcessUploadJobs`, and `RecoverStaleUploadJobs` for the durable async upload boundary.
- Exposes `SweepExpiredFiles` so the runtime scheduler can remove expired physical files and metadata.

## Notes

- API layers depend on these interfaces rather than concrete service implementations.
- Apphost depends on the same interface for the backend upload worker, keeping scheduler wiring outside the concrete service type.
- Download actors are nullable so public downloads can flow without authentication while protected file levels still fail closed in the service.
