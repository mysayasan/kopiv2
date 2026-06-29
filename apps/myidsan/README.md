# myidsan

`myidsan` is the identity and SSO authority for `kopiv2`.

It owns user, group, app registry, endpoint catalog, and shared RBAC administration APIs and acts as the single sign-on provider for apps such as `myseliasan`.

## Current Scope

- Local username/password login and registration through myidsan-local login APIs.
- Authenticated `POST /api/login/default/change-password` — verifies current password and sets a new one, clearing the forced first-login flag.
- User and group management through protected APIs (`/api/user-credential`, `/api/user-group`).
- App registry, app-auth-config, and app-redirect-uri management for SSO clients, with built-in app protection (myidsan/mymatasan/myseliasan codes are locked from rename/delete in the UI).
- SSO certificate authority: `GET /api/sso-ca` (CA public cert) and `POST /api/sso-ca/issue/{id}` (issue a client cert for a registered app's `client_id`). Backed by `ISsoCaService` / `SsoCa` entity using `infra/fleetca`. mTLS enforcement on the token endpoint is not yet wired.
- Superadmin handoff status: `GET /api/identity-status` — tells the SPA whether the stock superadmin is still active and whether handoff is safe; drives the persistent handoff banner.
- Shared accessrbac role + permission-matrix management via `/api/access-rbac` (roles CRUD + per-role endpoint permission matrix). The same surface is used identically by all apps that include the shared accessrbac module.
- SSO JWT issuer/audience settings through the `sso` config block.
- Cache-backed session entries under `sso:session:<sid>`.
- Internal fallback API: `POST /api/sso/introspect` (token validity check; RBAC decisions are now local to each app's accessrbac middleware).
- Redis or in-memory cache selection through the standard cache config.
- Bootstrap of the default `system` group; stock superadmin is seeded at startup from `localAuth.username`/`localAuth.password` with a forced first-login password change (no hardcoded SQL seed). Shared accessrbac built-in roles (`superadmin`, `viewer`), registered apps, and endpoint tier metadata are also seeded. Endpoint catalog is app-local: legacy cross-app rows are deleted on startup.
- React/Webpack identity administration UI under `views/react-webpack`, built into `static` for the Go app host.
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

The UI builds its sidebar from `GET /api/access-rbac/me`. A page appears only when the current user's role has `canGet` access to the backing API endpoint path prefix, and that endpoint's `metadata` in the `api_endpoint` catalog contains an enabled `menu` or `menus[]` item. The supported menu metadata fields are `id`, `label`, `group`, `order`, `summary`, `tone`, and optional `icon`. Superadmin roles (`isSuperadmin: true` in the `/me` response) see all menus.

The side-nav uses the standardized dark icon rail (icon glyph + label, grouped by section). Nav entries are now regrouped: **Users**, **Groups**, **Roles**, and **RBAC** all appear under the **Administration** group. **Apps** is under **Federation**; **Endpoints** is under **Access Control**. The Roles page now manages accessrbac roles via `/api/access-rbac/roles`; the broken `/api/user-credential/group/{id}` listing has been removed from the Groups page. The RBAC page is the permission-matrix view only.

A light/dark theme toggle (sun/moon) sits at the foot of the side-nav; the selected theme is persisted to `localStorage`. A shield+keyhole SVG brand mark replaces the old "ID" text tile.

A **forced first-login gate** (`ChangePasswordScreen`) blocks access to the app shell until the seeded stock superadmin changes its password. This mirrors myseliasan's flow and uses `POST /api/login/default/change-password`.

A **superadmin handoff banner** appears (non-dismissible) when `GET /api/identity-status` reports `superadminHandoffPending: true`. It prompts the operator to disable the stock account from the Users list once a real superadmin is confirmed.

The **Users page** is rebuilt as an inline admin table (role dropdown, Make superadmin, Enable/Disable) using `ClientDataTable`. It highlights the stock superadmin account and guards against disabling it when no real superadmin is active.

The **Apps page** is rebuilt as a master-detail view: left panel lists registered apps; the right panel shows the selected app's registry details, SSO client config, and redirect URIs inline (no modal). It includes a random-secret generator and a **Issue cert** button that calls `POST /api/sso-ca/issue/{id}` and offers the resulting PEM bundle for download.

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
