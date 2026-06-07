# Module: domain/enums/filestorage/security.go

## Purpose

Defines file-storage security levels used by upload metadata and download authorization.

## Values

- `SystemOnly = 0`: only internal service actors can retrieve the file.
- `Group = 1`: authenticated actors with a role in the file owner's group can retrieve the file.
- `Role = 2`: authenticated actors with the file owner's role or an ancestor role can retrieve the file.
- `Public = 3`: any caller can retrieve the file.

## Notes

- Upload endpoints default omitted `securityLvl` to `SystemOnly`.
- The file-storage service validates enum values before reading stored content.
