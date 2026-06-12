# Module: domain/entities/file_storage.go

## Purpose

Defines stored file metadata for the shared file-storage API.

## Responsibilities

- Store the internal file GUID, MIME type, checksum, virtual path, security level, optional expiry, ownership, and lifecycle timestamps.
- Use `Id` as the primary key for database records.
- Use `Guid` as a unique key so physical file storage and upload retry recovery are stable.

## Notes

- The file-storage service generates GUID values during staging.
- Public download contracts use metadata IDs; GUIDs remain internal.
- `securityLvl` values are defined in `domain/enums/filestorage`.
- `expiredAt=0` means no expiry.
- Countdown upload fields are endpoint conveniences only; stored metadata still uses absolute `expiredAt`.
- Async upload retries use the GUID as the idempotent metadata key to avoid duplicate rows after worker recovery.
