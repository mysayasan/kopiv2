# Module: domain/entities/runtime_setting.go

## Purpose

The appliance runtime key-value store, shared by every appliance app (`mymatasan`,
`myiotsan`). Moved here from `apps/mymatasan/entities/runtime_setting.go` (Tier: fleet
extraction, `docs/MYIOTSAN_PLAN.md` §6/P6) so a second appliance app persists pairing state,
the fleet key, and its own runtime-editable settings through the identical row shape instead
of forking it — it joins `LocalUser` as a shared appliance entity.

## Fields

| Field | Notes |
|---|---|
| `Id` | Auto-increment primary key. |
| `Key` | Unique key (`ukey:"key"`). |
| `Value` | Arbitrary string payload — a raw value, base64-encoded ciphertext, or a JSON blob depending on the caller. |
| `CreatedBy`, `CreatedAt`, `UpdatedBy`, `UpdatedAt` | Standard audit columns. |

## Notes

- No fixed schema for `Value` — every consumer defines its own key namespace and payload shape. `domain/shared/fleetnode` uses it for `pairing.state`, `pairing.fleetKey`, `pairing.nodeId`, and `pairing.enrollment` (see `docs/modules/domain/shared/fleetnode/pairing.go.md`); `mymatasan`'s own `RuntimeSettings` service uses it for user-editable app config.
- Sensitive values (the fleet key, the mTLS enrollment bundle) are stored as `atrest`-encrypted, base64-encoded blobs when a cipher is configured — encryption is the caller's responsibility, not this entity's.

## Load-bearing constraint: the struct name

The code-first bootstrap derives the table name by reflecting the struct name
(`strcase.ToSnake(typeOf.Name())`). That means this struct **must stay named
`RuntimeSetting`** — renaming it (or aliasing it to something else) would rename the
`runtime_setting` table out from under every deployed appliance on its next boot, taking the
fleet key, the pairing enrollment, and every persisted setting with it.
`apps/mymatasan/entities/runtime_setting.go`'s `type RuntimeSetting = entities.RuntimeSetting`
alias exists to keep mymatasan's call sites compiling unchanged while this constraint holds.
