# Module: apps/myidsan/entities/directory_config.go

## Purpose

Entity for myidsan's LDAP/Active Directory connection. Stored in table
`directory_config`. A singleton row (`Name = "default"`) holds the settings the
Federation → Directory admin page edits and `services.IDirectoryService` reads on
every LDAP login.

## Fields

| Field | Notes |
|---|---|
| `Id` | Primary key. |
| `Name` | Unique key (`ukey1`); always `"default"` for the singleton config. |
| `Enabled` | Directory login is offered/accepted only while true. |
| `DisplayLabel` | Names the login option on both sign-in pages (e.g. "Domain account"); defaults to "Domain account" when blank. |
| `Host` / `Port` | Directory server address. `Port` 0 defaults to 636 (implicit TLS) / 389 (StartTLS). |
| `UseStartTls` | Plaintext-port connect + StartTLS upgrade instead of implicit TLS (`ldaps`, 636). There is no insecure mode. |
| `CaCertPem` | Optional pinned CA bundle (PEM). Empty = system roots. |
| `BindDn` / `BindPassword` | Read-only service account used to find the user entry before the per-user bind. `BindPassword` is **encrypted at rest** (`infra/atrest`, see `services/directory.go.md`'s `encodeDirectorySecret`) — it cannot be hashed like an OAuth client secret because the service bind has to replay it against the directory on every login. |
| `BaseDn` | Search base for the user lookup. |
| `UserFilter` | `%s`-templated LDAP filter; empty uses the built-in AD/inetOrgPerson default (`infra/login.defaultLdapUserFilter`). |
| `GroupAttr` | User-entry attribute holding group memberships (`memberOf` on AD; configurable for FreeIPA/OpenLDAP). |
| `SubjectAttr` | Overrides the stable-id attribute; empty = auto `objectGUID` → `entryUUID` → DN. |
| `Authoritative` | When true, the group→role mapping re-applies on **every** login (directory is the system of record); when false, a mapping only seeds the role for accounts still pending clearance and a manually assigned role sticks. |
| `CreatedBy` / `UpdatedBy` | Audit user IDs (0 = system). |
| `CreatedAt` / `UpdatedAt` | Audit timestamps (Unix). |

## Notes

- Mirrors the `AppAuthConfig` precedent (DB entity + RBAC-gated API + Federation SPA
  menu) rather than a `config.json` block — directory settings change often and
  deserve a "Test connection" button, which a static file cannot offer.
- `services.DirectoryConfigView` is the admin read model: it never returns
  `BindPassword`, only a derived `HasBindPassword` boolean, so the secret is
  write-only from the API's perspective (same pattern as `AppAuthConfig`'s
  `HasClientSecret`).
