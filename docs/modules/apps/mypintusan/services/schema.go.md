# Module: apps/mypintusan/services/schema.go

## Purpose

`Entities()` — the 12-struct schema this app owns, in dependency order for a human reading it
rather than for the migrator (which does not care). This is production code now: it was
**moved here from `store_sql_test.go`** so `apps/mypintusan/app/app.go.md`'s `Entities()` can
call the real function instead of the composition root re-deriving (and risking drifting from)
the list only tests used to know.

## Responsibilities

```go
func Entities() []any
```

Returns, in order: people and what they carry (`Holder`, `Credential`); the physical estate
(`Door`, `Reader`, `ReaderProfile`); the rules about who gets in (`AccessGroup`,
`AccessGroupMember`, `Schedule`, `ScheduleWindow`, `Holiday`, `Grant`); the append-only record of
what happened (`AccessEvent`).

## Notes

- Lives beside `services/store_sql.go.md` rather than in the `app` package deliberately: the
  store is what breaks if a table is missing, so keeping the list next to it means the two
  cannot drift.
- `apps/mypintusan/app/app.go.md`'s `Entities()` appends this list to the shared appliance block
  (`ApiEndpoint`, `ApiLog`, `UserSession`, `Notification`, `LocalUser`, `AccessRole`,
  `AccessRolePermission`, `RuntimeSetting`) — 8 shared + 12 here + `pkey`/index rows account for
  the 23 tables the live boot created.
- `store_sql_test.go` previously defined an identical function locally; that copy was deleted in
  the same change (see `services/store_sql.go.md`'s Notes) so this is now the only definition.
