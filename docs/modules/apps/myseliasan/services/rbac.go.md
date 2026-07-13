# Module: apps/myseliasan/services/rbac.go

## Purpose

Implements `IControlUserService`, the myseliasan-specific identity layer over the shared accessrbac role core. Handles both local (stock bootstrap) and federated (myidsan-authenticated) control-plane users.

## Responsibilities

- `EnsureStockSuperadmin(ctx, username, password) (StockSeedResult, error)` — seeds a local bootstrap admin (must-change-password, IsStock=true) on startup. The password is resolved by `resolveBootstrapPassword`: `LOCAL_ADMIN_PASSWORD` env override, then the passed-in config value, else a 16-char password generated from `crypto/rand` (`generateBootstrapPassword`, unambiguous charset, no O/0/I/l/1). Empty `password` no longer silently becomes the literal string `"admin"` — that fallback shipped a guessable admin/admin superadmin on any install with a blank config password. The returned `StockSeedResult{Username, Password, Generated, Seeded}` tells the caller what was established: `Seeded` is true only when the account was actually created (so the caller knows to announce a credential), `Generated` is true when the password was invented rather than supplied. Refreshes the password from config only while the account is still untouched (MustChangePassword=true, not Disabled) **and** the password was not generated — a generated password is minted fresh every boot, so refreshing it on restart would invalidate the credential the operator just read out of `INITIAL_ADMIN_LOGIN.txt`. Once a real admin has changed the password or the account is retired, config no longer overrides it.
- `ResetStockSuperadmin(ctx, username, password) (StockSeedResult, error)` — the lock-out recovery path: force-resets the stock superadmin's password (same `resolveBootstrapPassword` precedence), re-enables the account, re-flags it must-change, and re-assigns the `superadmin` role, regardless of whether the account was already taken over or disabled at handoff. Re-creates the account via `EnsureStockSuperadmin` if it does not exist. Only reachable by consuming a `RESET_ADMIN` marker file in the data dir (see `apps/myseliasan/app/firstrun.go.md`) — not exposed over the network.
- `UpsertFederated(ctx, ssoUserId, email, name)` — provisions or refreshes a myidsan user on first login. New accounts are provisioned with **no role** (`RoleId = 0`, pending clearance). They can authenticate but have zero access until a superadmin assigns a role on the RBAC page; the SPA shows them an "access pending" screen (previously they were auto-granted the `viewer` role, whose legacy GET /api wildcard exposed every admin page). Returns `ErrUserDisabled` for a disabled account. Rejects `ssoUserId <= 0` with an error (a stable SSO subject id is mandatory; without it the service cannot safely distinguish identities). Lookup is keyed strictly on `ssoUserId` — email is deliberately not used as a fallback match key (see Security note below).
- `AuthenticateLocal(ctx, username, password)` — bcrypt verification; returns `ErrInvalidCredentials` or `ErrUserDisabled`.
- `ChangePassword(ctx, userId, current, next)` — local accounts only; minimum 8 characters; clears `MustChangePassword`.
- `SetRole(ctx, userId, roleId)` / `SetDisabled(ctx, userId, disabled)` — superadmin-level mutations.
- `RetireStock(ctx)` — disables every IsStock account. Returns count retired (retained in the interface for programmatic use; the elevate endpoint no longer calls it automatically).
- `SuperadminStatus(ctx)` — returns `(stockActive bool, realActive bool, err error)`. `stockActive` is true if any enabled stock superadmin exists; `realActive` is true if any enabled non-stock superadmin exists. Used by the disable-lockout guard in the API handler and by the `/api/session/me` response to drive the handoff banner.
- `ResolveAccessUser(ctx, userId)` — implements `sharedservices.AccessUserResolver`; maps `ControlUser → AccessPrincipal`.
- `GetById(ctx, id)` — uses `repo.GetById` (primary key). The prior `GetByUnique(ctx,"","id",id)` matched no field and always returned the first user — a critical auth bug causing every myseliasan session to resolve as the stock superadmin's role.
- `List(ctx)` — returns all users (up to 1000), sorted by `Id ASC` for a stable default order.

## Notes

- Built-in roles (`superadmin`, `viewer`) are seeded by the shared accessrbac core (`EnsureBuiltins`), not by this service.
- The `ErrUserDisabled` and `ErrInvalidCredentials` sentinel errors are checked by the auth API to return appropriate HTTP responses.
- Minimum password length is 8 characters (`minPasswordLen`).
- `generateBootstrapPassword` mirrors mymatasan's bootstrap-password generator (`apps/mymatasan/services/local_user.go`), so every platform in the suite hands a fresh install a strong per-install credential instead of a shared default.
- `apps/myseliasan/app/firstrun.go` is the caller: it invokes `EnsureStockSuperadmin`/`ResetStockSuperadmin`, prints the console banner, and writes `INITIAL_ADMIN_LOGIN.txt` when `StockSeedResult.Seeded` is true.

## Security: federated identity lookup

`findFederated` looks up by `ssoUserId` only. The previous email-fallback path was removed because `myidsan` can emit a non-unique placeholder email (e.g. `"admin"`) for accounts that have no real email, and matching on it would allow a new SSO identity to inherit the role of an existing — potentially privileged — account (privilege escalation / account takeover). A changed or new SSO subject id therefore provisions a fresh `viewer` account rather than rebinding to another row. Operators who switch identity providers and want to preserve an existing account's role must re-elevate the new federated user explicitly via `POST /api/rbac/users/{id}/elevate`.
