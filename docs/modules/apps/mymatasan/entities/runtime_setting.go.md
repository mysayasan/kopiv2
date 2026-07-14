# Module: apps/mymatasan/entities/runtime_setting.go

## Purpose

`RuntimeSetting` is now a type alias (`= sharedentities.RuntimeSetting`) of the shared
`domain/entities.RuntimeSetting` — the persisted key-value row moved there as part of the fleet
extraction (see `docs/MYIOTSAN_PLAN.md` §6/P6) so `myiotsan` can use the identical bootstrap
entity for its own runtime settings and pairing state. `mymatasan`'s own call sites (repos,
services) are unchanged, since a Go type alias is the same type under a different name.

## Responsibilities

- Alias `RuntimeSetting` to `domain/entities.RuntimeSetting` so `dbsql.IGenericRepo[entities.RuntimeSetting]` still compiles everywhere it already did in `mymatasan`.

## Notes

- **Load-bearing constraint, same as `LocalUser`**: the struct this aliases MUST stay named `RuntimeSetting`, because the code-first bootstrap derives the table name by reflecting the struct's name. Renaming the underlying type (or aliasing to a differently-named one) would rename `runtime_setting` out from under every deployed appliance — taking the fleet key, the pairing enrollment, and every persisted setting with it.
- See `docs/modules/domain/entities/runtime_setting.go.md` for the field-level definition (unchanged from what previously lived directly in this file: `Id`, `Key` (unique), `Value`, audit columns).
