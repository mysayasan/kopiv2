# Module: domain/entities/api_log.go

## Purpose

Defines the database-backed API activity log entity.

## Fields

- `statsCode`: HTTP response status.
- `durationMs`: elapsed request handling time in milliseconds.
- `logMsg`: request activity or audit detail.
- `clientIpAddrV4` / `clientIpAddrV6`: captured client address.
- `requestUrl`: request URI including query string.
- audit columns: `createdBy`, `createdAt`, `updatedBy`, `updatedAt`.

## Notes

- The shared bootstrap engine creates and additively migrates the `api_log` table from this entity.
