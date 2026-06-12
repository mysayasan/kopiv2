# myidsan

`myidsan` is the identity and access-management app for `kopiv2`.

It owns the identity user, group, role, app registry, endpoint, and RBAC administration APIs and acts as the single sign-on authority for apps such as `mymatasan`.

## Current Scope

- Local username/password login and registration through myidsan-local login APIs.
- User, group, role, app registry, endpoint, and endpoint RBAC management through shared protected APIs.
- SSO JWT issuer/audience settings through the `sso` config block.
- Cache-backed session entries under `sso:session:<sid>`.
- Internal fallback APIs:
  - `POST /api/sso/introspect`
  - `POST /api/sso/authorize`
- Redis or in-memory cache selection through the standard cache config.
- Bootstrap of the default `system` group, `superadmin` role, `superadmin` account, registered apps, and protected identity-management endpoint permissions.
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

## Frontend

The MyIDSan frontend follows the lightweight React/Webpack pattern used by `mymatasan`.

From `apps/myidsan/views/react-webpack`:

```bash
npm install
npm run build
```

The production build writes assets into `apps/myidsan/static`, which the Go app host serves as the SPA catch-all.

The UI builds its sidebar from `GET /api/endpoint-rbac/ep/me`. A page appears only when the current user's role has RBAC access to the backing API endpoint and that endpoint's `metadata` contains an enabled `menu` or `menus[]` item for the page. The supported menu metadata fields are `id`, `label`, `group`, `order`, `summary`, `tone`, and optional `code`.

Example endpoint metadata:

```json
{
  "menu": {
    "enabled": true,
    "id": "users",
    "label": "Users",
    "group": "Identity",
    "order": 10,
    "summary": "Maintain user access.",
    "tone": "blue"
  }
}
```

Use `menus[]` when one API endpoint backs multiple UI pages, such as `users` and `roles` through `/api/user-credential`.

CRUD administration tables use the same RBAC source for page and action access. The toolbar enables create, edit, and delete only when the current role has the matching `POST`, `PUT`, or `DELETE` grant for the page endpoint, so row selection cannot bypass a denied action. Table controls are standardized with floating column filter popovers, datatype-aware operators, neutral boolean filters, ordered multi-column sorting, loading feedback, popup editing, and pagination with first, previous, next, last, and goto-page controls. Filter, sort, and page position are remembered in browser cookies per table resource and reset by the table clear control. The active page is also remembered; if the remembered page is no longer allowed by RBAC after refresh, the UI shows the unauthorized access page instead of silently jumping to another module.

For local frontend iteration:

```bash
npm run start
```

The webpack dev server runs on `https://localhost:4001` when the app cert files exist and proxies `/api`, `/swagger`, health, readiness, and metrics requests to the configured MyIDSan backend, which defaults to `https://localhost:3001` in dev.

## SSO Flow

`myidsan` is the issuer and policy authority for other apps:

1. A user signs in at `myidsan`.
2. `myidsan` creates a cache-backed session and issues an HMAC JWT with `iss`, `aud`, `exp`, `sid`, `appCode`, and `policyVersion`.
3. Resource apps such as `mymatasan` validate the token locally.
4. When Redis is enabled, apps share short-lived session and RBAC cache entries.
5. When only in-memory cache is enabled, resource apps call `myidsan` service APIs for introspection and authorization.

Internal fallback requests must include either `X-Myidsan-Internal-Token` or `Authorization: Bearer <token>` matching `sso.internalToken` or `SSO_INTERNAL_TOKEN`.

Browser relying apps such as `myseliasan` use the authorization-code routes under `/api/auth`. MyIDSan validates the registered client, exact callback URL, and requested audience before issuing a one-time code. During callback, the relying app exchanges that code at `POST /api/auth/token`; when this happens over local HTTPS, the relying app must trust the MyIDSan certificate through the OS trust store or its own `sso.caCertPath` setting.
