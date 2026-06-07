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
- Runtime OpenAPI documentation at `/swagger`.

## Run

From repository root:

```bash
go run . -app myidsan
```

`config.json` starts HTTPS on port `3001`; `config.dev.json` starts plain HTTP on port `3001`.
Both configs include app-relative TLS paths:

```text
apps/myidsan/certs/cert.pem
apps/myidsan/certs/key.pem
```

If `server.tlsPorts` is non-empty, those files must exist or startup will fail before the listener is ready.

Or build the app-specific command:

```bash
go build ./cmd/myidsan
```

Default dev listener:

```text
http://localhost:3001
```

Required secret:

```bash
export JWT_SECRET=replace-with-strong-secret
```

## SSO Flow

`myidsan` is the issuer and policy authority for other apps:

1. A user signs in at `myidsan`.
2. `myidsan` creates a cache-backed session and issues an HMAC JWT with `iss`, `aud`, `exp`, `sid`, `appCode`, and `policyVersion`.
3. Resource apps such as `mymatasan` validate the token locally.
4. When Redis is enabled, apps share short-lived session and RBAC cache entries.
5. When only in-memory cache is enabled, resource apps call `myidsan` service APIs for introspection and authorization.

Internal fallback requests must include either `X-Myidsan-Internal-Token` or `Authorization: Bearer <token>` matching `sso.internalToken` or `SSO_INTERNAL_TOKEN`.
