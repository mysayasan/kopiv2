# Module: domain/shared/apis/file_storage.go

## Purpose

Registers file storage upload, download, and upload-job endpoints.

## Endpoints

- `POST /api/file-storage/upload`
- `POST /api/file-storage/upload-async`
- `GET /api/file-storage/download`
- `GET /api/file-storage/job`

## Upload Behavior

- Accepts multipart files from the `documents` field.
- Accepts batch-level `securityLvl` (`0=SystemOnly`, `1=Group`, `2=Role`, `3=Public`).
- Accepts optional expiry as either absolute Unix-second `expiredAt` or countdown `expiresIn` plus `expiresInUnit`.
- Converts countdown expiry into absolute `ExpiredAt` before calling the service.
- Defaults omitted `securityLvl` to `SystemOnly` and omitted expiry to no expiry.
- Allows JPEG, PNG, PDF, and plain text content types.
- Streams each accepted file once into a staging directory while computing SHA-1 checksum.
- Rejects the whole batch if any file fails validation or staging.
- Calls the file storage service to run the coordinated metadata insert and final file write.
- Returns uploaded file metadata through shared output DTOs.
- Removes staged files when the request fails before service commit.

## Async Upload Behavior

- Uses the same staging and validation path as synchronous upload.
- Creates an `OperationJob` with the optional `Idempotency-Key` request header.
- Leaves staged files in place after enqueue so the backend worker can process or retry the upload.
- Returns the durable job output DTO so callers can poll `GET /api/file-storage/job?id=<id>`.

## Job Status Behavior

- Requires an `id` query parameter.
- Returns the upload job output DTO.

## Download Behavior

- `GET /api/file-storage/download?id=<id>` streams one stored file by metadata ID.
- `GET /api/file-storage/download?ids=<id1>,<id2>` streams a ZIP archive containing the requested files.
- `GET /api/file-storage/download?id=<id>&view=true` streams one stored file with inline disposition for browser rendering.
- GUIDs remain internal physical storage identifiers and are not part of the public download contract.
- The route is mounted without auth/RBAC so `Public` files can be retrieved by anonymous callers.
- If auth cookies are present, the API passes caller user/role identity to the service for `Group` and `Role` access checks.
- Delegates metadata lookup and file reads to the file storage service.
- ZIP downloads always use attachment disposition.
