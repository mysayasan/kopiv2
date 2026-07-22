# Module: apps/myidsan/entities/federated_group_mapping.go

## Purpose

Entity for a provider-group → myidsan-role mapping. Stored in table
`federated_group_mapping`, managed from the Federation → Directory admin page
alongside `DirectoryConfig`. Resolved by `services.ResolveMappedRole` on every LDAP
login (`services/directory.go.md`).

## Fields

| Field | Notes |
|---|---|
| `Id` | Primary key. |
| `Provider` | Scopes the mapping — `"ldap"` today; a later OIDC phase would use `"oidc:<key>"` per `docs/MYIDSAN_ENTERPRISE_SSO_PLAN.md`. |
| `GroupName` | The provider's group identifier. For LDAP, the full group DN exactly as delivered in the user entry's `memberOf` (or configured `GroupAttr`). Matched case-insensitively. |
| `RoleId` | The accessrbac role granted on a match. A mapping with `RoleId <= 0` is inert — never treated as "match to no role". |
| `Priority` | Higher wins when several mappings match one login's group memberships; ties break to the lowest `Id` (deterministic). |
| `CreatedBy` / `UpdatedBy` | Audit user IDs (0 = system). |
| `CreatedAt` / `UpdatedAt` | Audit timestamps (Unix). |

## Notes

- A login matching **no** mapping leaves the account's role untouched — mappings only
  ever grant, never widen implicitly; a brand-new account stays at the existing
  pending-clearance default (`UserRoleId: 0`) until a superadmin assigns one, or until
  a matching mapping does.
- Groups are re-evaluated on every login; a mapping/role change only takes effect at
  the account's next login (sessions do not revalidate role mid-session per the
  suite's existing session model, `middlewares/auth.go`).
