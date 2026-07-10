# myseliasan

`myseliasan` is a relying control-plane app for `mymatasan`.

It has no public landing page. Opening `/` requires a valid local `myseliasan` session; unauthenticated users are redirected to MyIDSan through the authorization-code flow.

It communicates with `mymatasan` nodes directly over the LAN using the pairing protocol and the reverse command tunnel — no traffic is relayed through `myidsan`. A shared fleet key (PSK) authenticates UDP multicast discovery and HTTPS adoption; after adoption, ongoing management runs over mutual TLS using certificates issued by a self-contained on-prem fleet CA hosted here. `myidsan` is used for operator SSO login only; authorization and user management are self-contained in `myseliasan`.

## Self-RBAC and user management

`myseliasan` owns its own user store (`control_user` table). Two kinds of users coexist:

- **Local (stock superadmin)**: seeded from `localAuth.username` / `localAuth.password` in `config.json` (defaults `admin` / `admin`); must change password on first login; intended to be retired after a real operator account is elevated.
- **Federated**: a `myidsan`-authenticated user auto-provisioned on first login with **no role** (pending clearance). They can authenticate but have zero access until a superadmin assigns a role on the RBAC page; the SPA shows an "access pending — contact your administrator" screen until a role is assigned.

Roles and the per-endpoint permission matrix are managed via the shared accessrbac surface at `/api/access-rbac` (superadmin-only). User management and the bootstrap handoff are at `/api/rbac/users/*` (also superadmin-only). The **bootstrap handoff** (`POST /api/rbac/users/{id}/elevate`) promotes a chosen real federated user to superadmin. The stock account is intentionally left active; a persistent non-dismissible banner appears in the SPA when `session/me` reports `superadminHandoffPending: true` (stock active + real active), prompting the operator to disable it from the Users list. Disabling the stock account is guarded — the API rejects the request if no real superadmin is active yet (`SuperadminStatus` check).

The SPA gates nav tabs using `GET /api/session/me`, which returns the caller's role name, `isSuperadmin`, `pending`, `stockSuperadminActive`, `superadminHandoffPending`, and permission rows — the same data the API gateway uses. When `pending: true` the SPA shows an "access pending" screen before the main nav is rendered. Role changes by a superadmin take effect on the next request without a re-login.

## Node management

The **Mymatasan** view in the UI exposes:

- **Fleet key**: generate or set the shared PSK (`POST /api/nodes/fleet-key`). Both the control plane and every node must have the same key for discovery to work.
- **LAN scan**: discover unpaired nodes on the local subnet (`POST /api/nodes/scan`); each result shows whether the node is already adopted.
- **Adopt**: provide the node IP, HTTPS port, and the claim code generated on the node UI to bind it (`POST /api/nodes/adopt`). After adoption, the node automatically contacts `POST /api/nodes/enroll` with a CSR; the control plane signs it and returns a short-lived certificate (default 7 days).
- **Adopted nodes**: table showing all adopted nodes with their status (`online`, `lost`, `self-dropped`), cert expiry, and a **Release** button. Release revokes the node's certificate and removes the registry row.
- **Heartbeat**: the control plane probes every adopted node over mTLS (`GET :<mtlsPort>/heartbeat`) on a configurable interval (default 60 s) and marks each node `online` or `lost`.
- **Command tunnel**: after pairing, each node dials a persistent WebSocket-over-fleet-mTLS connection to `myseliasan`'s control channel port (`pairing.controlPort`, default 49533). The `/api/nodes/{id}/proxy/<node-path>` endpoint tunnels any HTTP command to the node's own API router; the node's authorization stack enforces viewer/admin based on the per-node access grant. Node-pushed event frames (AI alerts, health, going-offline) are ingested into the control plane's notification feed at `/api/notifications`.
- **Node camera live view (media relay)**: the node also dials a separate media channel (`pairing.mediaPort`, default 49534) over fleet mTLS. When a browser requests live view of a node camera (`POST /api/nodes/{id}/cameras/{cam}/webrtc/offer`), `myseliasan` asks the node to stream that camera's RTP over the media channel, then re-broadcasts it to the browser over WebRTC — at full frame rate, without the browser needing any direct path to the node.
- **Embedded node camera pages**: selecting a camera in the side-nav node tree opens that camera's real `mymatasan` page inside `myseliasan` — Live View (relay video + PTZ + audio + "Add to Live Views"), Detection, Recordings, and Settings tabs, matching mymatasan's own tab bar exactly. These tabs are the actual mymatasan React components (copied into `components/nodecam/`), routed over the control/media channels instead of same-origin, so behavior and design track mymatasan automatically — see "Frontend" below. Recorded video plays through a dedicated range-capable endpoint rather than the command proxy (see the "Recording playback over the tunnel" bullet below). A cross-node **Live Views** wall (its own nav item) lets an operator pin cameras from any adopted node into one grid; a **Node Dashboard** mirrors mymatasan's own analytics dashboard over the tunnel. Older nodes whose API predates a given feature show an inline "unavailable on this node" banner instead of a silent blank panel.
- **Recording playback over the tunnel**: the control channel caps each message at 16 MiB, and an encrypted or HEVC-stored clip isn't seekable end-to-end through it, so recorded video for a node camera streams through `GET /api/nodes/{id}/recording-stream/{segId}` instead of the generic command proxy. It caps every browser `Range` request to 8 MiB, forwards it to the node's now-Range-capable `GET /api/recording/segments/{id}/download` (which materializes a seekable plaintext temp copy of encrypted/HEVC segments on first touch), and returns `206 Partial Content` — so the `<video>` element can play and seek a clip of any size without any single tunneled message exceeding the cap. See `docs/REQUEST_FLOW.md` → "Recording Playback over the Control Tunnel Flow".
- **Per-node access grants**: the adopting role and **superadmin roles** own the node (full access without a grant); other roles need an explicit grant via `GET/POST/DELETE /api/nodes/access`. A superadmin can also query a role's grants across all nodes with `GET /api/nodes/access?roleId=ID` (central RBAC node-access matrix on the RBAC page).
- **Adoption metadata**: when adopting a node, the operator can now set a custom **Name** (overrides the node's reported hostname), a **Description** (shown as a tooltip in the nav tree), and an **Icon** (glyph displayed in the side-nav node tree).

Both app and node must be on the same LAN segment for UDP multicast discovery to reach the node. Manual adoption by IP+port works across subnets if the node is reachable by HTTPS. The mTLS management port (default 49532) must also be reachable from the control plane.

## Fleet CA

`myseliasan` is its own on-prem certificate authority (ECDSA P-256, 10-year root). The CA key is stored in the local `ControlSetting` table (`pairing.caCert`/`pairing.caKey`) and is never transmitted off-prem. Node private keys are generated on the node and never sent to the control plane — only a CSR crosses the wire. Revocation is "refuse to renew + short TTL" (no CRL/OCSP). The `ManagedNode.Fingerprint` field is reserved for future use; identity is currently verified through the certificate CN.

## Frontend

The UI is a React/webpack SPA under `apps/myseliasan/views/react-webpack/`, built into `apps/myseliasan/static/` (content-hashed bundles), mirroring `mymatasan`'s frontend architecture. Myseliasan-only styling lives in `styles/app.css` and the shared RBAC-standard rail in `styles/rbac-standard.css`. Build with `npm install && npm run build` in that directory.

The shell uses the standardized dark icon side-nav (`SideNav` from `components/layout.js`), with a bespoke **Nodes tree** in the side-nav: an expandable branch listing adopted nodes (root item → fleet page/node dashboard, child items → each node's own camera sub-tree, lazily loaded over the tunnel on first expand). Selecting a node opens its `NodeDashboard`; selecting a camera under it opens that camera's full page (Live View/Detection/Recordings/Settings). A top-level **Live Views** nav item opens the cross-node camera wall. Admin pages (Users, Roles, RBAC) appear under the **Administration** group; the RBAC page includes a central **Node Access** matrix where a superadmin assigns per-role node access.

**Embedded node pages / design parity**: the camera tab components under `components/nodecam/` are the real mymatasan view source files (`vision.js`, `recording.js`, `previews.js`, `cameras.js` pieces, `ui.js`, `layout.js`, `hooks.js`, `ptz.js`, helpers/constants) copied in verbatim, so mymatasan behavior changes to those files should be ported here too. Two shims adapt them to run against a *remote* node: `nodecam/lib/helpers.js`'s `apiBase()` is repointed at the commander proxy (`setNodeProxyBase`), and `installProxyCsrf` teaches `window.fetch` to attach myseliasan's CSRF token on proxy writes (the copied components issue raw `fetch()` calls that predate the double-submit-cookie requirement below). Styling comes from mymatasan's actual stylesheets, imported as raw strings via a new `@mymatasan` webpack alias + `?raw` CSS rule (`webpack.config.js`), then injected once and CSSOM-scoped under `.nodecam-embed` (`components/node_embed.js`, `nodecam/scoped_css.js`) — this is a build-time re-import, not a manual copy, so mymatasan design changes flow into the embedded pages on the next `npm run build` here with no re-sync step.

`DataTable`, `Toast`/`ToastStack`, and the `icons` set are now sourced from the shared in-repo module at `frontend/shared/` (via `@shared` webpack alias). Per-app copies (`components/data_table.js`, `components/icons.js`) have been deleted. The `webpack.config.js` has been updated to resolve `@shared` and add `frontend/shared/src` to module search paths; the babel-loader now uses inline preset config.

**Theming**: three themes are available (Light / Dark / **High contrast**). The high-contrast theme uses black surfaces, white text, bright accents, and strong borders for accessibility. The side-nav responds to the active theme via `--nav-*` CSS tokens (soft light rail in light mode, dark gradient in dark, black in high contrast).

**Multi-language UI (i18n)**: the frontend is fully localized into English, Malay (Bahasa Melayu), Chinese Simplified, and Arabic (العربية). Arabic is RTL; selecting it sets `<html dir="rtl">` via `LangProvider` so the entire layout mirrors automatically. The active language is persisted to `localStorage`. A language switcher (`LanguageDropdown` from `@shared`) appears in the top bar as an inline row of buttons (`English | Melayu | 中文 | العربية`). App-specific strings live in `views/react-webpack/src/views/i18n.js` and layer over the shared base dictionary (`frontend/shared/src/i18n/index.js`) via `LangProvider`/`useT()`. Missing-locale keys fall back to English, then to the key itself.

`index.html` is served with `Cache-Control: no-cache, no-store, must-revalidate` (`app.go`'s `serveIndex`): it references content-hashed bundle filenames, so a cached stale copy can keep a browser on an old bundle after a rebuild — the hashed `.js`/`.css` chunks themselves can still be cached immutably.

**Shared footer**: an `AppFooter` component (`@shared`) renders at the bottom of the workspace, showing the app name, version, shared-core version, short commit hash, and build date (from `/api/version`) and the r450k product tagline. Version fields degrade gracefully when the endpoint is unreachable.

An **access pending screen** is shown to authenticated users with no role assigned (`session/me` returns `pending: true`), instructing them to contact an administrator.

A **superadmin handoff banner** is shown at the top of the workspace whenever `session/me` returns `superadminHandoffPending: true`, with a "Go to Users" shortcut for superadmins.

Because the control plane authenticates with the federated middleware, **state-changing API calls must send the double-submit CSRF token**: the `api()` helper in `lib/helpers.js` echoes the non-HttpOnly `__Host-kopiv2_csrf` (HTTPS) / `kopiv2_csrf` (dev) cookie in the `X-CSRF-Token` header on POST/PUT/PATCH/DELETE. Omitting it yields a 403 (which redirects to the SSO login). The fleet-key card includes a copy-to-clipboard button to avoid copy errors.

## Node liveness

The heartbeat reconciler consults the persistent control channel first — a node holding a live connection is authoritatively online regardless of whether its mTLS port is directly reachable from the parent. The mTLS probe is a fallback. A node is declared `lost` only after a **grace window** (3× heartbeat interval, floor 90 s) with no contact on either path, so a brief reconnect or firewall blip no longer flaps a healthy node offline. Status `self-dropped` is never overwritten by heartbeat.

The heartbeat order of operations:
1. Control channel present (`ControlServer.IsConnected`) → mark **online**.
2. Otherwise, attempt mTLS probe → mark **online** on success.
3. On failure, check grace window: if `now - lastSeenAt >= graceSeconds`, mark **lost**; otherwise hold prior status and skip the write.

## Networking / operations

- Discovery is UDP multicast (group `239.255.90.21:49531` by default) sent and received on **all** multicast-capable interfaces, so multi-homed hosts and same-host dev work. The host firewall must allow inbound UDP on the discovery port (49531) and TCP on the mTLS management port (49532), the control channel port (49533), and the media channel port (49534).
- Docker's default bridge network does not forward multicast — run with host networking, or use manual adoption by IP+port (which needs no multicast).
- Discovery is same-subnet only (UDP multicast does not route); manual adoption works across subnets as long as the node's HTTPS + mTLS ports are reachable.
- When `myseliasan` and `mymatasan` run on **separate machines**, add `"parentBaseUrl": "https://<parent-LAN-IP>:3002"` to `pairing` in `config.json`. Without it the node uses `sso.redirectBaseUrl`, which is `localhost` in the default dev config and is unreachable from another machine. `parentBaseUrl` is the address the node dials for the control and media channels and uses for enroll/self-drop callbacks.
- For cross-network node camera live view, add a `nodeStream` block to `config.json` (see `config.nodestream.sample.json`): set `publicIps` to the parent's external IP(s), `udpPort` to a single open UDP port, and optionally `iceServers` with a TURN server. For same-LAN use, the `nodeStream` block can be omitted entirely.

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
