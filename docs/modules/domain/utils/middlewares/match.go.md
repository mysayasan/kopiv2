# Module: domain/utils/middlewares/match.go

## Purpose

Shared host/path matching helpers extracted from the retired `rbac.go`. Reused by the rate-limit middleware and any future middleware that needs the same semantics.

## Functions

- `hostMatches(allowedHost, requestHost string) bool` — `"*"` matches any host; otherwise compares normalized (port-stripped) strings.
- `pathMatches(allowedPath, requestPath string) bool` — exact match or prefix-segment match (e.g. `/api/admin` matches `/api/admin/users` but not `/api/adminx`). Empty `allowedPath` matches nothing.

## Notes

- These were previously private functions inside `rbac.go`; they were extracted here so the rate-limit middleware can use them without depending on the retired RBAC type.
