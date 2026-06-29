# myseliasan

`myseliasan` is a relying control-plane app for `mymatasan`.

It has no public landing page. Opening `/` requires a valid local `myseliasan` session; unauthenticated users are redirected to MyIDSan through the authorization-code flow.

It communicates with `mymatasan` nodes directly over the LAN using the pairing protocol and the reverse command tunnel — no traffic is relayed through `myidsan`. A shared fleet key (PSK) authenticates UDP multicast discovery and HTTPS adoption; after adoption, ongoing management runs over mutual TLS using certificates issued by a self-contained on-prem fleet CA hosted here. `myidsan` is used for operator SSO login only; authorization and user management are self-contained in `myseliasan`.

## Self-RBAC and user management

`myseliasan` owns its own user store (`control_user` table). Two kinds of users coexist:

- **Local (stock superadmin)**: seeded from `localAuth.username` / `localAuth.password` in `config.json` (defaults `admin` / `admin`); must change password on first login; intended to be retired after a real operator account is elevated.
- **Federated**: a `myidsan`-authenticated user auto-provisioned on first login and assigned the `viewer` role.

Roles and the per-endpoint permission matrix are managed via the shared accessrbac surface at `/api/access-rbac` (superadmin-only). User management and the bootstrap handoff are at `/api/rbac/users/*` (also superadmin-only). The **bootstrap handoff** (`POST /api/rbac/users/{id}/elevate`) promotes a chosen real federated user to superadmin. The stock account is intentionally left active; a persistent non-dismissible banner appears in the SPA when `session/me` reports `superadminHandoffPending: true` (stock active + real active), prompting the operator to disable it from the Users list. Disabling the stock account is guarded — the API rejects the request if no real superadmin is active yet (`SuperadminStatus` check).

The SPA gates nav tabs using `GET /api/session/me`, which returns the caller's role name, `isSuperadmin`, `stockSuperadminActive`, `superadminHandoffPending`, and permission rows — the same data the API gateway uses.

## Node management

The **Mymatasan** view in the UI exposes:

- **Fleet key**: generate or set the shared PSK (`POST /api/nodes/fleet-key`). Both the control plane and every node must have the same key for discovery to work.
- **LAN scan**: discover unpaired nodes on the local subnet (`POST /api/nodes/scan`); each result shows whether the node is already adopted.
- **Adopt**: provide the node IP, HTTPS port, and the claim code generated on the node UI to bind it (`POST /api/nodes/adopt`). After adoption, the node automatically contacts `POST /api/nodes/enroll` with a CSR; the control plane signs it and returns a short-lived certificate (default 7 days).
- **Adopted nodes**: table showing all adopted nodes with their status (`online`, `lost`, `self-dropped`), cert expiry, and a **Release** button. Release revokes the node's certificate and removes the registry row.
- **Heartbeat**: the control plane probes every adopted node over mTLS (`GET :<mtlsPort>/heartbeat`) on a configurable interval (default 60 s) and marks each node `online` or `lost`.
- **Command tunnel**: after pairing, each node dials a persistent WebSocket-over-fleet-mTLS connection to `myseliasan`'s control channel port (`pairing.controlPort`, default 49533). The `/api/nodes/{id}/proxy/<node-path>` endpoint tunnels any HTTP command to the node's own API router; the node's authorization stack enforces viewer/admin based on the per-node access grant. Node-pushed event frames (AI alerts, health, going-offline) are ingested into the control plane's notification feed at `/api/notifications`.
- **Per-node access grants**: the adopting role owns the node (full access without a grant); other roles need an explicit grant via `GET/POST/DELETE /api/nodes/access`.

Both app and node must be on the same LAN segment for UDP multicast discovery to reach the node. Manual adoption by IP+port works across subnets if the node is reachable by HTTPS. The mTLS management port (default 49532) must also be reachable from the control plane.

## Fleet CA

`myseliasan` is its own on-prem certificate authority (ECDSA P-256, 10-year root). The CA key is stored in the local `ControlSetting` table (`pairing.caCert`/`pairing.caKey`) and is never transmitted off-prem. Node private keys are generated on the node and never sent to the control plane — only a CSR crosses the wire. Revocation is "refuse to renew + short TTL" (no CRL/OCSP). The `ManagedNode.Fingerprint` field is reserved for future use; identity is currently verified through the certificate CN.

## Frontend

The UI is a React/webpack SPA under `apps/myseliasan/views/react-webpack/`, built into `apps/myseliasan/static/` (content-hashed bundles), mirroring `mymatasan`'s frontend architecture. It reuses `mymatasan`'s `app.css` and `icons.js` verbatim; myseliasan-only styling lives in `styles/controlplane.css` and the shared RBAC-standard rail in `styles/rbac-standard.css`. Build with `npm install && npm run build` in that directory.

The shell has been migrated from a horizontal topbar (`TopBar`) to the standardized dark icon side-nav (`SideNav` from `components/layout.js`), matching myidsan's design. Admin pages (Users, Roles, RBAC) are now separate nav tabs under an **Administration** group instead of a single "Users & Roles" tab; they use the shared `DataTable` component from `components/data_table.js`. The `RbacAdminTab` export has been split into `UsersPage`, `RolesPage`, and `RbacPage`.

A **superadmin handoff banner** is shown at the top of the workspace whenever `session/me` returns `superadminHandoffPending: true`, with a "Go to Users" shortcut for superadmins.

Because the control plane authenticates with the federated middleware, **state-changing API calls must send the double-submit CSRF token**: the `api()` helper in `lib/helpers.js` echoes the non-HttpOnly `__Host-kopiv2_csrf` (HTTPS) / `kopiv2_csrf` (dev) cookie in the `X-CSRF-Token` header on POST/PUT/PATCH/DELETE. Omitting it yields a 403 (which redirects to the SSO login). The fleet-key card includes a copy-to-clipboard button to avoid copy errors.

## Networking / operations

- Discovery is UDP multicast (group `239.255.90.21:49531` by default) sent and received on **all** multicast-capable interfaces, so multi-homed hosts and same-host dev work. The host firewall must allow inbound UDP on the discovery port (49531) and TCP on the mTLS management port (49532) and the control channel port (49533).
- Docker's default bridge network does not forward multicast — run with host networking, or use manual adoption by IP+port (which needs no multicast).
- Discovery is same-subnet only (UDP multicast does not route); manual adoption works across subnets as long as the node's HTTPS + mTLS ports are reachable.

## Development defaults

- App URL: `https://localhost:3002`
- MyIDSan provider URL: `https://localhost:3001`
- Client ID: `myseliasan`
- Dev client secret: `dev-myseliasan-secret`
- Callback URL registered in MyIDSan: `https://localhost:3002/api/auth/callback`
- `sso.redirectBaseUrl` controls the callback origin sent to MyIDSan; it must match a registered MyIDSan redirect URI.
- Local HTTPS requires certificates signed by a CA trusted by the machine running MySeliaSan, because MySeliaSan exchanges callback codes with MyIDSan over HTTPS from the backend.
- `sso.caCertPath` can point to a PEM CA/certificate bundle for that backend call; relative paths resolve from `apps/myseliasan`.
- The default dev value points to `../myidsan/certs/cert.pem`, which trusts the bundled localhost MyIDSan certificate. If you later replace MyIDSan with a privately signed certificate, point `sso.caCertPath` or `SSO_CA_CERT_PATH` at that CA bundle.
- `sso.caCertPath` only adds trusted roots for the backend HTTPS token exchange. It does not skip hostname, expiry, or chain validation.
- DB engine: SQLite at `apps/myseliasan/data/myseliasan.db`
- `localAuth.username` / `localAuth.password`: stock superadmin credentials (default `admin`/`admin123`; must change on first login)

Run MyIDSan first, then run:

```bash
ENVIRONMENT=dev JWT_SECRET=replace-with-strong-secret go run . -app myseliasan
```
