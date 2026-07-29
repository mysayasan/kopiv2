# myidsan

`myidsan` is the identity and SSO authority for `kopiv2`.

It owns user, group, app registry, endpoint catalog, and shared RBAC administration APIs and acts as the single sign-on provider for apps such as `myseliasan`.

## Current Scope

- Local username/password login and registration through myidsan-local login APIs.
- Authenticated `POST /api/login/default/change-password` — verifies current password and sets a new one, clearing the forced first-login flag.
- **LDAP / Active Directory login** (`POST /api/login/ldap`, an "Account type" select on both login surfaces): a service-bind search + per-user bind against an existing directory (AD, Samba AD, FreeIPA, OpenLDAP, 389-ds), always over TLS. Configured from **Federation → Directory** (`GET/PUT /api/directory-config`, `POST /api/directory-config/test` — validates the submitted, unsaved settings and never binds as a sample user), with directory group → myidsan role mappings (`/api/federated-group-mapping`) resolved on every login. See `docs/HOWTO.md`'s "MyIDSan LDAP / Active Directory Login" for setup steps and `docs/MYIDSAN_ENTERPRISE_SSO_PLAN.md` for the design. **Live-tested** against a real directory server (OpenLDAP 2.4 over both LDAPS/636 and StartTLS/389): user bind, wrong/empty-password refusal, unknown-user refusal, `memberOf` group retrieval, bind-free `LdapLookup` resolving to the same subject as an authenticated bind, the admin test probe, and CA-pinning fail-closed. The bench is repeatable — `infra/login/ldap_integration_test.go` carries the container recipe and runs under `RUN_LDAP_IT=1`. Not yet exercised against Active Directory or Samba AD specifically, where `objectGUID` (rather than `entryUUID`) is the subject attribute.
- **Kerberos SPNEGO SSO** (`GET /api/login/kerberos`, a "Windows (SSO)" button — label configurable — on both login surfaces): a domain-joined browser's Kerberos ticket is verified against an exported keytab (no password prompt); the verified principal resolves through the directory (above), when configured, to the same account an LDAP/password login for that person reaches, or a principal-derived standalone identity otherwise. Configured via the `kerberos` config block (`enabled`, `keytabPath`, `servicePrincipal`, `onlyRealms`, `displayLabel`); a bad/missing keytab degrades to "not offered" with a startup warning rather than failing boot. See `docs/HOWTO.md`'s "Kerberos SPNEGO SSO" subsection for SPN/keytab setup and `docs/MYIDSAN_ENTERPRISE_SSO_PLAN.md` (Phase 2). Not yet live-tested against a real KDC/realm.
- **Generic OIDC login** (`GET /api/login/<key>`/`GET /api/callback/<key>`, one button per configured entry on both login surfaces): federates against any spec-compliant OpenID Connect IdP (Keycloak, Authentik, ADFS, Entra, ...) via `login.oidc[]` config entries — issuer discovery at startup, PKCE + nonce on the login leg, `id_token` verified against the discovered JWKS. Accounts bind to `oidc:<key>` (changing `key` orphans existing accounts). A configured `groups_claim` seeds a role from a `FederatedGroupMapping` scoped to that provider, always for a still-pending account only (never re-applied on every login the way an authoritative LDAP directory can be — see `services/directory.go.md`'s `AdmitExternalIdentity`). The client secret can also come from `OIDC_<KEY>_CLIENT_SECRET`; a bad/unreachable issuer or missing credentials disables just that one provider with a startup warning. See `docs/HOWTO.md`'s "Generic OIDC login" subsection (Keycloak example) and `docs/MYIDSAN_ENTERPRISE_SSO_PLAN.md` (Phase 3). Not yet live-tested against a real IdP.
- **Per-IP failed-login lockout** covers every password-based credential surface (local login, LDAP login, the server-rendered federated login page) via the shared `loginSecurity` config — previously myidsan had none. Kerberos SSO does not use this lockout (a ticket is cryptographically verified, not guessed).
- **Two-factor authentication (TOTP)**: self-service on the **Profile** page (`GET/POST/DELETE /api/mfa/*` — enroll, confirm with a scanned QR or manually-entered base32 secret, view status, regenerate recovery codes, disable), gated on both password login surfaces once a factor is confirmed — `POST /api/login/default` and `POST /api/login/ldap` return `{mfaRequired, mfaToken}` with **no session cookie** until the code is verified at `POST /api/login/mfa`; the server-rendered `/api/auth/login` page has the equivalent challenge at `POST /api/auth/mfa`. Kerberos SPNEGO and OIDC/Google/GitHub logins are not gated — the upstream IdP owns factor policy for those. 10 single-use bcrypt-hashed recovery codes are issued once at enrollment as the lost-device fallback. A confirmed factor's secret is sealed at rest the same way the LDAP bind password is. Superadmin can reset another user's factor at `DELETE /api/mfa-admin/{id}`; a `RESET_MFA` marker (below) recovers the stock superadmin's own lockout. See `docs/MYIDSAN_MFA_PLAN.md` for the design.
- **Account recovery ("Forgot your password?")**: a "Forgot your password?" link on both login surfaces posts to `POST /api/login/forgot` (SPA) / `POST /api/auth/forgot` (server-rendered) — always a generic response, never revealing whether the identifier matched an account. Two channels: an always-on **operator queue** (superadmin-only **Reset requests** page, `GET /api/password-reset`, `POST /api/password-reset/{id}/resolve` issues a one-time temporary password to hand over out-of-band, `POST /api/password-reset/{id}/dismiss`) that works with zero configuration on an air-gapped install, plus an **optional** self-service email link when the new `smtp` config block is enabled (`smtp.enabled`, off by default) — the link (`GET/POST /api/auth/reset?token=...`) is single-use, 30 minutes, and deliberately does **not** issue a session, so any enrolled MFA factor is still presented at the next normal login. Only **local** accounts are eligible; LDAP/Kerberos/OIDC accounts are directed to their upstream IdP instead. See `docs/HOWTO.md`'s account-recovery section for the operator workflow and SMTP setup.
- **Self-service Profile page**: change-password, MFA management, and a profile-picture editor are consolidated into one **Profile** page (`GET/POST/DELETE /api/profile/avatar`), reachable only from the account chip in the side rail — it is not a nav item or dashboard tile. This is the first in-app self-service change-password form (`POST /api/login/default/change-password`, previously only reachable via the forced first-login gate). A superadmin can also set (or clear) any user's picture inline from the **Users** page via `GET/POST/DELETE /api/profile/avatar/{userId}`. The client resizes the picked image to a 256px JPEG in-browser before upload (canvas), so no server-side image codec is needed — just a MIME allow-list (JPEG/PNG/WebP/GIF) and a size cap.
- User and group management through protected APIs (`/api/user-credential`, `/api/user-group`). Both surfaces are **superadmin-only** (`RequireSuperadmin` middleware), preventing any non-superadmin role from reading or modifying user accounts or groups regardless of matrix grants. A superadmin may not change their own role via PUT `/api/user-credential`.
- App registry, app-auth-config, and app-redirect-uri management for SSO clients, with built-in app protection (myidsan/mymatasan/myseliasan codes are locked from rename/delete in the UI).
- SSO certificate authority: `GET /api/sso-ca` (CA public cert) and `POST /api/sso-ca/issue/{id}` (issue a client cert for a registered app's `client_id`). Backed by `ISsoCaService` / `SsoCa` entity using `infra/fleetca`. mTLS enforcement on the token endpoint is not yet wired.
- Superadmin handoff status: `GET /api/identity-status` — tells the SPA whether the stock superadmin is still active and whether handoff is safe; drives the persistent handoff banner.
- Shared accessrbac role + permission-matrix management via `/api/access-rbac` (roles CRUD + per-role endpoint permission matrix). The same surface is used identically by all apps that include the shared accessrbac module.
- SSO JWT issuer/audience settings through the `sso` config block.
- Cache-backed session entries under `sso:session:<sid>`.
- Internal fallback API: `POST /api/sso/introspect` (token validity check; RBAC decisions are now local to each app's accessrbac middleware).
- Redis or in-memory cache selection through the standard cache config.
- Bootstrap of the default `system` group; stock superadmin is seeded at startup from `localAuth.username`/`localAuth.password` with a forced first-login password change (no hardcoded SQL seed). The password is resolved with precedence `LOCAL_ADMIN_PASSWORD` env → `config.localAuth.password` → a generated 16-character per-install password (`crypto/rand`, unambiguous charset) — an **empty config password no longer falls back to a hard-coded default**. When a password is generated, `app/firstrun.go` prints a one-time console banner (URL/username/password) and writes it to `INITIAL_ADMIN_LOGIN.txt` in the data dir; a generated password is never silently rotated on a later restart (only a config/env-supplied one still refreshes on each boot, and only while the account is untouched). If you're locked out, drop a `RESET_ADMIN` marker file in the data dir — the next start force-resets the password, re-enables the account, and re-announces it the same way. If instead you've lost the stock superadmin's authenticator device **and** its recovery codes (MFA lockout, not a password lockout), drop a `RESET_MFA` marker file in the data dir — the next start clears only that account's second factor, leaving the password untouched; drop both markers together if you need to recover from both at once. Shared accessrbac built-in roles (`superadmin`, `viewer`) and endpoint tier metadata are also seeded. Endpoint catalog is app-local: legacy cross-app rows are deleted on startup.
- **First-run setup wizard**: a signed-in superadmin who has cleared the forced password change and has not yet completed setup (`GET /api/setup/state`) sees a 5-step wizard (Welcome → register the first relying app under Apps → optional LDAP quick-enable → create a personal superadmin account, retiring the stock one → done) before the normal app shell. Every step is skippable and each step is just a thin form over APIs that already exist (`/api/app-registry`, `/api/app-auth-config`, `/api/app-redirect-uri`, `/api/directory-config`, `/api/user-credential`); `POST /api/setup/complete` (or Skip setup) records completion as a single server-side flag shared across browsers, so the wizard never reappears once finished.
- **App registration is operator-controlled, not auto-seeded.** Following the standard OAuth / Google-console model, `myidsan` no longer automatically registers `mymatasan`, `myseliasan`, or itself in `app_registry`/`app_auth_config`/`app_redirect_uri` on startup. An operator must register each relying app via **Apps** in the UI (or via the API) before its SSO client can exchange authorization codes. This closes the security hole where a database drop would silently re-provision an unregistered app's credentials.
- React/Webpack identity administration UI under `views/react-webpack`, built into `static` for the Go app host.
- **Backup & restore** (`GET /api/backup/sections`, `POST /api/backup/{export,preview,restore}`, superadmin-only and deliberately **not** delegatable through the permission matrix): a single passphrase-encrypted `.idbackup` archive covering accounts and groups, roles and permissions, MFA enrolments and recovery codes, registered apps with their SSO clients and redirect URIs, the directory config and its group→role mappings, and the SSO CA. Restore remaps every foreign key onto the destination's fresh ids and **skips** rows whose parent is absent rather than leaving a dangling grant, then drops every live session (a pre-restore session would still carry pre-restore authority) and marks first-run setup complete. The two at-rest-sealed values (TOTP secrets, the LDAP bind password) are **unsealed on export and re-sealed with the destination host's key on restore** — carrying the sealed bytes would restore cleanly and then fail every second-factor check, so the archive holds them in plaintext inside the passphrase-encrypted envelope and the at-rest key never travels. Host-local state (`config.json`, TLS certs, `secret/atrest.key`, logs, pending reset requests, sessions) is excluded by design. Reachable from **Backup & restore** in the nav and from the first-run wizard's "restore from a backup instead" path. See `deploy/README-myidsan.md` for the DR runbook.
- Runtime OpenAPI documentation at `/swagger`.

## Run

From repository root:

```bash
go run . -app myidsan
```

`config.json` and `config.dev.json` both start HTTPS on port `3001`.
Both configs include app-relative TLS paths:

```text
apps/myidsan/certs/cert.pem
apps/myidsan/certs/key.pem
```

If `server.tlsPorts` is non-empty, those files must exist or startup will fail before the listener is ready.
The bundled local development certificate is for `localhost`; replace it with a trusted certificate/key pair before using another host name or a deployment environment.

Or build the app-specific command:

```bash
go build ./cmd/myidsan
```

Default dev listener:

```text
https://localhost:3001
```

Required secret:

```bash
export JWT_SECRET=replace-with-strong-secret
```

The stock superadmin account is seeded from `config.json` → `localAuth`:

```json
"localAuth": {
  "enabled": true,
  "username": "admin",
  "password": "admin123"
}
```

Change both fields before first run. The password is only refreshed from config while `mustChangePassword` is still true on the account (i.e., before the operator completes the first-login password change via the `ChangePasswordScreen`).

## Frontend

The MyIDSan frontend follows the lightweight React/Webpack pattern used by `mymatasan`.

From `apps/myidsan/views/react-webpack`:

```bash
npm install
npm run build
```

The production build writes assets into `apps/myidsan/static`, which the Go app host serves as the SPA catch-all.

The UI builds its sidebar from `GET /api/access-rbac/me`. A page appears only when the current user's role has `canGet` access to the backing API endpoint path prefix, and that endpoint's `metadata` in the `api_endpoint` catalog contains an enabled `menu` or `menus[]` item. The supported menu metadata fields are `id`, `label`, `group`, `order`, `summary`, `tone`, and optional `icon`. Superadmin roles (`isSuperadmin: true` in the `/me` response) see all menus. When `/me` returns `pending: true` (authenticated but no role assigned), an "access pending — contact your administrator" screen is shown instead of the app shell.

The side-nav uses the standardized dark icon rail (icon glyph + label, grouped by section). Nav entries are regrouped: **Users**, **Groups**, **Roles**, and **RBAC** all appear under the **Administration** group; **Users** and **Groups** are superadmin-only sections in the SPA. **Apps** is under **Federation**; **Endpoints** is under **Access Control**. The Roles page manages accessrbac roles via `/api/access-rbac/roles`; the RBAC page is the permission-matrix view only.

`DataTable`, `Toast`/`ToastStack`, and the `icons` set are now sourced from the shared in-repo module at `frontend/shared/` (via `@shared` webpack alias). Per-app copies (`lib/data_table.js`, `lib/icons.js`) have been deleted. The `webpack.config.js` has been updated accordingly.

**Theming**: three themes are available (Light / Dark / **High contrast**). The high-contrast theme uses black surfaces, white text, and strong borders for accessibility. The side-nav responds to the active theme via `--nav-*` CSS tokens.

**Multi-language UI (i18n)**: the frontend is fully localized into English, Malay (Bahasa Melayu), Chinese Simplified, and Arabic (العربية). Arabic is RTL; selecting it sets `<html dir="rtl">` via `LangProvider` so the entire layout mirrors automatically. The active language is persisted to `localStorage`. A language switcher (`LanguageDropdown` from `@shared`) appears in the top bar as an inline row of buttons (`English | Melayu | 中文 | العربية`). App-specific strings live one-per-locale under `views/react-webpack/src/views/i18n/` (`en.js`/`ms.js`/`zh.js`/`ar.js`); `views/react-webpack/src/views/i18n.js` ships only English eagerly and dynamically imports the other three as separate chunks (`i18n-ms`/`i18n-zh`/`i18n-ar`) on demand, so a single-language user only ever downloads the translations it uses. A returning non-English user briefly gates first paint while their locale chunk loads (English users never wait), and switching language loads the target chunk before applying it so the UI never flashes English. Translations layer over the shared base dictionary in `frontend/shared/src/i18n/index.js` via `LangProvider`/`useT()`. Adding a new key to the English dict and any locale dicts is sufficient; missing-locale keys fall back to English, then to the key itself, so no render path can crash.

**Shared footer**: a `AppFooter` component (`@shared`) renders at the bottom of the app shell, showing the app name, version, shared-core version, short commit hash, build date (fetched from `/api/version`), and the r450k product tagline. All fields are optional and degrade gracefully if the endpoint is unreachable.

A light/dark theme toggle (sun/moon) sits at the foot of the side-nav; the selected theme is persisted to `localStorage`. The brand mark is `@shared/BrandLogo` (violet `--brand-mark` tint), the same shared component mymatasan and myseliasan use — geometry and saturation are identical across apps, only the hue differs, so the SPA reads as part of the same product family. The server-rendered federated login page (`GET /api/auth/login`, `renderLoginPage` in `apis/federated_auth.go`) now wears the same `.login-screen`/`.login-panel` card as the SPA and the other apps' React login screens — myidsan previously had no build-time asset pipeline at all, so this page's styling was hand-inlined CSS with a different, older mark; it now has one (webpack `CopyPlugin` self-hosts the Quicksand font and copies a favicon), so the page can reference `/assets/fonts.css` and `/assets/favicon.svg` like the SPA does. This page is the one server-rendered screen in the suite, and it is where myseliasan's SSO hop lands during federated login, so its styling matters beyond myidsan itself. Google/GitHub buttons appear on that page — and gate the SPA's own social-login buttons via `GET /api/login/providers` — only for a provider whose `client_id` **and** `client_secret` are both non-empty; the stock `config.json` ships blank `google`/`github` objects, and a present-but-blank provider previously still rendered a button that sent the browser to `accounts.google.com` with no `client_id`. A failed local-login `POST` now re-renders the same branded card with the error inline and the username preserved, instead of dumping unstyled text. When LDAP/Active Directory login is enabled, both this page and the SPA's own auth screen show an "Account type" select (Local account / the configured directory label) that posts to `/api/login/ldap` instead of `/api/login/default`; the select is absent entirely when directory login is disabled.

A **forced first-login gate** (`ChangePasswordScreen`) blocks access to the app shell until the seeded stock superadmin changes its password. This mirrors myseliasan's flow and uses `POST /api/login/default/change-password`.

A **superadmin handoff banner** appears (non-dismissible) when `GET /api/identity-status` reports `superadminHandoffPending: true`. It prompts the operator to disable the stock account from the Users list once a real superadmin is confirmed.

The **Users page** is rebuilt as an inline admin table (role dropdown, Make superadmin, Enable/Disable) using `ClientDataTable`. It highlights the stock superadmin account and guards against disabling it when no real superadmin is active.

The **Apps page** is rebuilt as a master-detail view: left panel lists registered apps; the right panel shows the selected app's registry details, SSO client config, and redirect URIs inline (no modal). It includes a random-secret generator and a **Issue cert** button that calls `POST /api/sso-ca/issue/{id}` and offers the resulting PEM bundle for download.

Registering a new app is a guided walkthrough rather than a bare CRUD form, since one wrong value here fails at runtime on the *relying app's* side with a terse error far from this screen — but the guidance stays out of the way until asked for, since a wall of always-visible prose goes unread. A data-derived **step rail** (register → SSO client → redirect URIs → connect) tracks progress against what actually exists (an SSO client, one or more redirect URIs), not a wizard cursor, so it reads correctly for an app that is only half-configured. A collapsible **"How app registration works"** primer explains the authorize → login → one-time code → token-exchange flow; it auto-opens only on a genuinely first registration (no apps registered yet) and otherwise stays a one-click header. Every registry and SSO-client field carries an **(i) info tip** next to its label — click to reveal what the value is used for, click again (or Escape, or click elsewhere) to close it — plus client-side validation that stays permanently visible: the app code is held to a slug shape (leading letter, then lowercase/digits/dashes), code and audience are checked for a clash against the already-loaded app list (they are `ukey1`/`ukey2` on `app_registry`, so a collision would otherwise surface as a raw database error), and the base URL is parsed and flagged with a warning (not blocked) when it is plain `http://` on a non-localhost host — any error-level issue disables Save. A worked example (e.g. `Fleet Console`) is no longer a separate line under the field; it is the input's own placeholder. A live **"Configure the app with these"** panel renders the provider base URL, audience, redirect URI, authorize URL, token URL, and a ready-to-paste `sso` config.json block, each with its own Copy button and its explanatory note behind the same (i). A **"Connect the app"** section lists the handoff steps (each detail behind an (i)) and a collapsed-by-default troubleshooting callout mapping the literal server errors (`client is not registered`, `redirect_uri is not registered`, `audience not registered for client`, `client secret not valid`) to their likely cause; the redirect-URI matching rules above it are the same collapsed-by-default callout pattern. The SSO client form's **Require PKCE** and **Allow refresh tokens** checkboxes are still recorded on the client row but are not yet enforced or acted on: the token endpoint (`apis/federated_auth.go`) implements only the `authorization_code` grant and nothing reads either flag — the (i) on each checkbox says so explicitly.

The "Configure the app with these" panel also has an **Export** button that downloads `<app-code>-sso.json` — an envelope (`kind: "myidsan.sso.client"`, `version: 1`) carrying the issuer, audience, provider base URL, client ID, redirect base URL/path, and session TTL, so an operator setting up the relying app's own SSO config never has to retype values that must match byte-for-byte. The client secret is included only when one was just generated in that browser tab (myidsan stores only a hash and the API never returns it); a permanent note above the button states which case applies before download, and the panel's toast repeats it afterward. `myseliasan`'s Settings page can import this file directly (see its README's "Settings" section).

Example endpoint metadata stored in `api_endpoint.metadata`:

```json
{
  "menu": {
    "enabled": true,
    "id": "users",
    "label": "Users",
    "group": "Administration",
    "order": 10,
    "summary": "Maintain user access.",
    "tone": "blue",
    "icon": "user"
  }
}
```

Use `menus[]` when one API endpoint backs multiple UI pages, such as `roles` and `rbac` both backed by `/api/access-rbac`.

CRUD administration tables use the same accessrbac permission matrix for page and action access. The toolbar enables create, edit, and delete only when the current role has the matching `canPost`, `canPut`, or `canDelete` grant for the page endpoint path prefix. Table controls are standardized with floating column filter popovers, datatype-aware operators, neutral boolean filters, ordered multi-column sorting, loading feedback, popup editing, and pagination with first, previous, next, last, and goto-page controls. Filter, sort, and page position are remembered in browser cookies per table resource and reset by the table clear control.

For local frontend iteration:

```bash
npm run start
```

The webpack dev server runs on `https://localhost:4001` when the app cert files exist and proxies `/api`, `/swagger`, health, readiness, and metrics requests to the configured MyIDSan backend, which defaults to `https://localhost:3001` in dev.

## SSO Flow

`myidsan` is the JWT issuer and cross-app SSO authority:

1. A user signs in at `myidsan`.
2. `myidsan` creates a cache-backed session and issues an HMAC JWT with `iss`, `aud`, `exp`, `sid`, `appCode`, and `policyVersion`.
3. Relying apps (e.g. `myseliasan`) exchange an authorization code for the token at `POST /api/auth/token`.
4. When Redis is enabled, apps share short-lived session cache entries.
5. When only in-memory cache is available, relying apps can call `POST /api/sso/introspect` to validate a token (requires `X-Myidsan-Internal-Token` or `Authorization: Bearer <token>` matching `sso.internalToken` / `SSO_INTERNAL_TOKEN`).

Authorization decisions (who can call what API) are enforced locally by each app's shared accessrbac middleware — `myidsan` no longer issues RBAC policy decisions.

Browser relying apps such as `myseliasan` use the authorization-code routes under `/api/auth`. MyIDSan validates the registered client, exact callback URL, and requested audience before issuing a one-time code. During callback, the relying app exchanges that code at `POST /api/auth/token`; when this happens over local HTTPS, the relying app must trust the MyIDSan certificate through the OS trust store or its own `sso.caCertPath` setting.
