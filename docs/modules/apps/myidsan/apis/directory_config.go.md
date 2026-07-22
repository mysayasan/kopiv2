# Module: apps/myidsan/apis/directory_config.go

## Purpose

Protected MyIDSan management API for the LDAP/Active Directory connection (singleton)
and the federated group→role mappings — backs the Federation → Directory admin page.

## Routes

- `GET /api/directory-config`: read the current directory settings (`DirectoryConfigView`; bind password never returned, only `hasBindPassword`).
- `PUT /api/directory-config`: save the directory settings. A blank `bindPassword` in the payload preserves the stored one.
- `POST /api/directory-config/test`: validates the **submitted** settings against the live directory (service bind + optional sample-user search). Never persists anything; never binds as the sample user. A test outcome — success or failure — is a `200`; only a malformed request is an error response.
- `GET /api/federated-group-mapping`: list group→role mappings (standard filter/sort/paging query options).
- `POST /api/federated-group-mapping`: create a mapping. Requires non-empty `provider`, non-empty `groupName`, and a positive `roleId`.
- `PUT /api/federated-group-mapping`: update a mapping (same validation as create).
- `DELETE /api/federated-group-mapping/{id}`: delete a mapping.

## Security

- Both route groups use MyIDSan auth and `AccessSessionMidware`. The surface is **RBAC-matrix governed** (not granted by default; a superadmin must explicitly grant the role permission in the matrix to delegate access) — the same pattern as `app_auth_config.go.md`. This is safe to delegate because the bind password is never exposed by any read response.
- `actorId(r)` reads the acting admin's user id off the request's JWT claims for `Save`'s audit columns; falls back to `0` if the claims are somehow absent.
