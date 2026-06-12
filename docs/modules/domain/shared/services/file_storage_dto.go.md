# Module: domain/shared/services/file_storage_dto.go

## Purpose

Adapts the core file storage service to return caller-selected DTO types.

## Responsibilities

- Wraps `IFileStorageService` without changing file access, upload, transaction, or cleanup behavior.
- Projects file metadata and durable upload job results into selected DTO types.
- Leaves binary download payloads and upload processing operations on the core service contract.
- Forwards create, download, worker, recovery, and expiry cleanup calls to the core service.
