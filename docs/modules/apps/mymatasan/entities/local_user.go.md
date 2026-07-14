# Module: apps/mymatasan/entities/local_user.go

## Purpose

`LocalUser` is now a **type alias** of the shared appliance user
(`domain/entities.LocalUser`) so mymatasan's existing call sites (`ILocalUserService`,
repositories, handlers) keep compiling unchanged after the local-auth stack was extracted to
`domain/shared` (myiotsan needed the same DB-backed users, and forking ~1,300 lines of
security-critical code into a second app was rejected). The real field-by-field definition
and its documentation now live at `domain/entities/local_user.go.md`.

## Notes

- `type LocalUser = sharedentities.LocalUser` — this **must** stay an alias of a type still
  named `LocalUser`: the code-first bootstrap derives the table name by reflecting the struct
  name (`strcase.ToSnake(typeOf.Name())`), so aliasing a type named anything else would rename
  the `local_user` table out from under every deployed appliance.
- No behavior changed for mymatasan by this move — verified by booting on a fresh DB: the
  table is still `local_user`, existing rows read back unchanged.
