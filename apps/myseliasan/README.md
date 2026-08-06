# myseliasan

`myseliasan` is a relying control-plane app for `mymatasan`.

It has no public landing page. Opening `/` requires a valid local `myseliasan` session; unauthenticated users are redirected to MyIDSan through the authorization-code flow.

It communicates with `mymatasan` nodes directly over the LAN using the pairing protocol and the reverse command tunnel — no traffic is relayed through `myidsan`. A shared fleet key (PSK) authenticates UDP multicast discovery and HTTPS adoption; after adoption, ongoing management runs over mutual TLS using certificates issued by a self-contained on-prem fleet CA hosted here. `myidsan` is used for operator SSO login only; authorization and user management are self-contained in `myseliasan`.

## Self-RBAC and user management

`myseliasan` owns its own user store (`control_user` table). Two kinds of users coexist:

- **Local (stock superadmin)**: seeded from `localAuth.username` / `localAuth.password` in `config.json`; must change password on first login; intended to be retired after a real operator account is elevated. The password is resolved with precedence `LOCAL_ADMIN_PASSWORD` env → `config.localAuth.password` → a generated 16-character per-install password (`crypto/rand`, unambiguous charset) — an **empty config password no longer falls back to the literal `admin`**. When a password is generated, `app/firstrun.go` prints a one-time console banner (URL/username/password) and writes it to `INITIAL_ADMIN_LOGIN.txt` in the data dir; a generated password is never silently rotated on a later restart (only a config/env-supplied one still refreshes on each boot, and only while the account is untouched). If you're locked out, drop a `RESET_ADMIN` marker file in the data dir (the Windows installer's "reset the admin login" option does this for you) — the next start force-resets the password, re-enables the account, and re-announces it the same way.
- **Federated**: a `myidsan`-authenticated user auto-provisioned on first login with **no role** (pending clearance). They can authenticate but have zero access until a superadmin assigns a role on the RBAC page; the SPA shows an "access pending — contact your administrator" screen until a role is assigned.

Roles and the per-endpoint permission matrix are managed via the shared accessrbac surface at `/api/access-rbac` (superadmin-only). User management and the bootstrap handoff are at `/api/rbac/users/*` (also superadmin-only). The SPA's **Roles** admin page merges what used to be two separate pages (a role list and a raw RBAC permission matrix) into one: creating a role auto-seeds a **viewer default** (read-only on the fleet + notifications, plus read-only access on every currently-adopted node — a one-time snapshot; nodes adopted later still need a manual grant), a **Copy** action duplicates a role's full permission matrix and node grants onto a new role, and access is granted per role through curated feature toggles (**View fleet**, **Manage fleet**, **Notifications**) instead of raw paths — the raw path+verb matrix is still there under a collapsed **Advanced** section for edge cases. The **bootstrap handoff** (`POST /api/rbac/users/{id}/elevate`) promotes a chosen real federated user to superadmin. The stock account is intentionally left active; a persistent non-dismissible banner appears in the SPA when `session/me` reports `superadminHandoffPending: true` (stock active + real active), prompting the operator to disable it from the Users list. Disabling the stock account is guarded — the API rejects the request if no real superadmin is active yet (`SuperadminStatus` check).

The SPA gates nav tabs using `GET /api/session/me`, which returns the caller's role name, `isSuperadmin`, `pending`, `stockSuperadminActive`, `superadminHandoffPending`, and permission rows — the same data the API gateway uses. When `pending: true` the SPA shows an "access pending" screen before the main nav is rendered. Role changes by a superadmin take effect on the next request without a re-login.

## Node management

The **Mymatasan** view in the UI exposes:

- **Fleet key**: generate or set the shared PSK (`POST /api/nodes/fleet-key`). Both the control plane and every node must have the same key for discovery to work.
- **LAN scan**: discover unpaired nodes on the local subnet (`POST /api/nodes/scan`); each result shows whether the node is already adopted.
- **Adopt**: opens a pop-up **Adopt** dialog — pre-filled with the address/hostname when opened from a discovered node, or blank via an **Adopt manually** button (for a node on another subnet, not reachable by multicast). Provide the HTTPS port and the claim code generated on the node UI to bind it (`POST /api/nodes/adopt`); name/description/icon are optional and can be edited later. After adoption, the node automatically contacts `POST /api/nodes/enroll` with a CSR; the control plane signs it and returns a certificate (default 90 days). That initial enrollment is always allowed, but every *subsequent* renewal (the node re-enrolling before its cert expires, exactly as it always has) is refused unless an operator turns on **auto-renew** for that node (off by default — see "Certificate auto-renew" below) — a per-node dead-man's switch so a forgotten or decommissioned node falls out of the fleet on its own instead of needing an explicit revoke.
- **Adopted nodes**: table showing all adopted nodes with their status (`online`, `lost`, `self-dropped`), cert expiry, a **Manage** button (opens the node Settings dialog, below), a **Wipe** button, and a **Release** button. Release revokes the node's certificate and removes the registry row. A **node kind** column shows **"Camera node"** (a `mymatasan` NVR) or **"Sensor hub"** (a `myiotsan` device hub) — the AUTHORITATIVE value the node itself returned over the fleet-key-signed, claim-code-gated adopt call, never the unsigned display hint carried in a LAN scan result. A node adopted before this field existed shows as a camera node, since every one of those is.
- **Wipe**: remotely factory-resets an adopted node over the control tunnel — the same secure wipe mymatasan can run on itself (see `Secure Wipe & Reset`), erasing its recordings/config and restarting it. Clicking Wipe first checks the node's `bootstrap.allowReset` gate (`GET .../proxy/api/system/reset/state`); if allowed, an auto-proceeding countdown modal (cancellable) confirms before `POST .../proxy/api/system/reset` is sent. The node returns immediately (reset runs asynchronously) then drops offline while it wipes and restarts.
- **Node Settings dialog**: **Manage** opens a tabbed modal — Details, Camera Health, Users, Backup & Recovery, and Version & Health — styled like mymatasan's own Settings page (via the same embedded-node styling described under "Frontend"). Every tab except Details tunnels its reads/writes over the node proxy (`/api/nodes/{id}/proxy/...`):
  - **Details** edits the control-plane record itself — name, description, icon — via `PUT /api/nodes/{id}` (not tunneled; this is myseliasan's own record of the node, distinct from anything on the node). Also shows the node ID and a clickable link to the node's own address. A **Certificate** section on the same tab shows the cert's expiry (status-tinted: ok / expiring within ~14 days / expired / not yet enrolled) and an **Auto-renew** toggle (`PUT /api/nodes/{id}/auto-renew`) — with it off, a warning explains the node will lose its fleet connection when the certificate expires; turning it on lets the node's next automatic re-enrollment through.
  - **Camera Health** reads/writes the node's camera-health monitor settings (`GET/PUT .../proxy/api/settings/health`).
  - **Users** lists/adds/deletes the node's local users and can toggle admin/reset password (`.../proxy/api/settings/users`).
  - **Backup & Recovery** shows the node's recovery-key configured state and hosts the **Wipe** danger-zone action; the actual `.mmbackup` file transfer stays on the node itself (it can exceed the control-channel frame cap, so it is not proxied here).
  - **Version & Health** mirrors mymatasan's own tab: software version + shared-core version + commit + build date, update check/apply, service health (API liveness via `.../proxy/api/health` and readiness via `.../proxy/api/ready` — the node-side `/api/ready` mirror described in `docs/modules/infra/apphost/run.go.md` — including per-component Db/Cache/Machine/Cameras status), and host CPU/memory/disk with the node's own warn/critical thresholds. An **older node whose API predates `/api/ready`** shows the readiness pill as unavailable rather than failing the panel.
- **Heartbeat**: the control plane probes every adopted node over mTLS (`GET :<mtlsPort>/heartbeat`) on a configurable interval (default 60 s) and marks each node `online` or `lost`. The reconciler is also proactive rather than passive: a node dropping to `lost`, a lost node recovering, or a node certificate nearing expiry each raise a notification in the unified feed (see "Fleet-health alerting" below) — the control plane is no longer just a relay that silently stops hearing from a crashed node.
- **Certificate auto-renew**: node certificates used to renew automatically and silently forever. Renewal is now gated per node by an **Auto-renew** toggle (`PUT /api/nodes/{id}/auto-renew`, shown on the node's Details tab — see below), **off by default for a newly adopted node**. With it off, the node's own automatic re-enrollment attempt before its cert expires is refused by the control plane (it needs no claim code — the node still authenticates with its existing pairing token — the control plane just declines to sign); the certificate then lapses on schedule, the node drops off the control channel, and it goes `lost` — a per-node dead-man's switch so a forgotten or decommissioned node quietly falls out of the fleet without an operator needing to remember to revoke it. Turning it on lets the node's next renewal through and keeps it current indefinitely, same as before this feature. Upgrading an existing fleet does not surprise-expire anything: a one-time startup pass (`INodeRegistry.BackfillAutoRenew`) turns auto-renew on for every node already enrolled before this feature shipped. The node certificate's default lifetime is also longer now (90 days, up from 7) so an un-renewed node stays reachable for a meaningful window rather than lapsing within a week.
- **Command tunnel**: after pairing, each node dials a persistent WebSocket-over-fleet-mTLS connection to `myseliasan`'s control channel port (`pairing.controlPort`, code fallback 49533; shipped `config.json` sets 39533). The `/api/nodes/{id}/proxy/<node-path>` endpoint tunnels any HTTP command to the node's own API router; the node's authorization stack enforces viewer/operator/admin based on the per-node access grant. Node-pushed event frames (AI alerts, health, going-offline) are ingested into the control plane's notification feed at `/api/notifications`. If a node's control channel drops while a tunneled command is still in flight, the proxy now fails fast with a `404 node is not connected` instead of hanging until a 30 s timeout — but for a non-idempotent write (e.g. a settings change) the outcome on the node at the moment of disconnect is unknown and is **not** automatically retried, since there is no tunneled-write idempotency key yet.
- **Node camera live view (media relay)**: the node also dials a separate media channel (`pairing.mediaPort`, code fallback 49534; shipped `config.json` sets 39534) over fleet mTLS. When a browser requests live view of a node camera (`POST /api/nodes/{id}/cameras/{cam}/webrtc/offer`), `myseliasan` asks the node to stream that camera's RTP over the media channel, then re-broadcasts it to the browser over WebRTC — at full frame rate, without the browser needing any direct path to the node.
- **Embedded node camera pages**: selecting a camera in the side-nav node tree opens that camera's real `mymatasan` page inside `myseliasan` — Live View (relay video + PTZ + audio + "Add to Live Views"), Detection, Recordings, and Settings tabs, matching mymatasan's own tab bar exactly. The page header is the shared `@shared/CameraHero` (the same component mymatasan's own camera page renders): a breadcrumb trail (`Nodes > <node name> > <camera name>`, both non-final crumbs navigating back via the node manager's own back/clear-focus handlers) over a status-tinted camera tile with health/stream chips. This replaced a flat title row that used to show the camera's ONVIF/host URL underneath the name — that URL is no longer in the header, but is still available as the ONVIF URI / Host chips in the Live tab's info grid. These tabs are the actual mymatasan React components (copied into `components/nodecam/`), routed over the control/media channels instead of same-origin, so behavior and design track mymatasan automatically — see "Frontend" below. Recorded video plays through a dedicated range-capable endpoint rather than the command proxy (see the "Recording playback over the tunnel" bullet below). The Recordings tab includes the same **Purge now** action as mymatasan's own Recordings tab (see mymatasan's README) — deletes ALL footage and AI-event snapshots for the camera immediately, behind the same 5-second cancellable countdown — tunneled to the node's `POST /api/recording/purge-camera` via the control proxy. A cross-node **Live Views** wall (its own nav item, positioned above the Nodes tree) lets an operator pin cameras from any adopted node into one grid, built for parity with mymatasan's own Live Views: grid layout picker, pagination, fullscreen, drag-to-reorder tiles, per-tile maximize and PTZ, and an in-wall cross-node **Add camera** picker for adding tiles without leaving the wall; a **Node Dashboard** mirrors mymatasan's own analytics dashboard over the tunnel. Older nodes whose API predates a given feature show an inline "unavailable on this node" banner instead of a silent blank panel.
- **Objects** (its own nav item): the fleet-scale counterpart to mymatasan's combined Objects page, with **Search** and **Classes** tabs. Search fans a query out to every adopted node's (or one selected node's) `GET /api/observations` over the proxy and merges the results client-side, sorted newest-first (capped at 300 rows, with a truncation notice) — filterable by node, one or more objects (aggregated label picker), date range, and minimum confidence. Each result's footage cell matches mymatasan's exactly: a boxed snapshot (detection box + label drawn server-side) with Play and Maximize overlay buttons, a play modal that seeks reliably to the detected moment (re-asserting `currentTime` on `loadedmetadata` and `canplay`, clamped to duration) with the bounding box drawn over the video, and a maximize modal for the snapshot alone. Classes manages one chosen node's detection-class registry over the proxy, reusing mymatasan's own classes panel embedded under scoped CSS.
- **Teach** (its own nav item): because teaching (capture → train → activate) runs entirely on a node, the flow is "Where-first" — the operator picks a target camera via a Node→Camera tree dropdown, and the real mymatasan Teach wizard (copied under `components/nodecam/teach.js`) then runs against that camera's node over the tunnel, pre-scoped to the selected camera. A **Share** tab lets an operator push a trained skill across the fleet: export a skill from a source node as an encrypted `.mmskill` package (over the proxy) and import it into any number of selected target nodes in one action; because both the export payload and each import travel through the control tunnel's frame/body caps, it defaults to model-only (no training images).
- **Dashboard** (the landing tab): a fleet-wide analytics view mirroring mymatasan's own camera dashboard, but the unit of aggregation is the adopted **node** instead of the camera. It reads `myseliasan`'s own notifications table (node-pushed events + control-plane events) via `GET /api/notifications/stats` — no per-node fan-out is needed since the control plane already stores every event — with selectable Today/7-day/30-day ranges, KPI cards, and a per-node activity breakdown (`Stats.BySource`, keyed `"node:<id>"`). A **Certs expiring** KPI card reads `GET /api/nodes/fleet-status` and shows the combined count of certs within the warn window plus already-expired certs, tinted danger/warning/default and hinting the warn-window size or "all node certs valid". Distinct from the per-node **Node Dashboard** described above, which mirrors one node's own analytics over the tunnel.
- **Embedded node IoT device pages**: the same trick as embedded node camera pages, above, but for an adopted `myiotsan` "Sensor hub" node — selecting an IoT node in the side-nav routes (`node_manager.js`, `kind === 'iot'`) into `components/node_iot_manager.js`, which embeds that node's own Devices/Rules/Alerts/Discovery/Commands pages (copied into `components/nodeiot/`) inside `myseliasan`, mirroring `components/nodecam/`. `apiBase()` is overridden the same way, so every call is tunneled through `/api/nodes/{id}/proxy/...` — **the browser never talks to the node directly**, which is what lets an IoT sensor node sit behind NAT with no inbound firewall rule, same as a camera node. The node's own CSS is scoped to the embed's container, and the `@shared` stylesheets are concatenated in alongside it — a scoped rendering context does not inherit the host document's stylesheets, so anything styled via `@shared` would otherwise render unstyled. See `docs/MYIOTSAN_PLAN.md` §8f.
- **Recording playback over the tunnel**: the control channel caps each message at 16 MiB, and an encrypted or HEVC-stored clip isn't seekable end-to-end through it, so recorded video for a node camera streams through `GET /api/nodes/{id}/recording-stream/{segId}` instead of the generic command proxy. It caps every browser `Range` request to 8 MiB, forwards it to the node's now-Range-capable `GET /api/recording/segments/{id}/download` (which materializes a seekable plaintext temp copy of encrypted/HEVC segments on first touch), and returns `206 Partial Content` — so the `<video>` element can play and seek a clip of any size without any single tunneled message exceeding the cap. See `docs/REQUEST_FLOW.md` → "Recording Playback over the Control Tunnel Flow".
- **Per-node access grants**: the adopting role and **superadmin roles** own the node (full access without a grant); other roles need an explicit grant via `GET/POST/DELETE /api/nodes/access`, set to one of three device-access levels — **Viewer** (`canRead`, watch live), **Operator** (`canOperate`, + review recorded footage, acknowledge alerts, PTZ, talk-back), or **Admin** (`canWrite`, everything including deleting footage) — mirroring mymatasan's own three local roles. The levels escalate (admin implies operator implies viewer) and are normalised on save, so a grant is really one choice of level. A superadmin can also query a role's grants across all nodes with `GET /api/nodes/access?roleId=ID` (central RBAC node-access matrix on the RBAC page).
- **Adoption metadata**: when adopting a node, the operator can now set a custom **Name** (overrides the node's reported hostname), a **Description** (shown as a tooltip in the nav tree), and an **Icon** (glyph displayed in the side-nav node tree).
- **Unrecognized (stranded) nodes**: a node can hold a valid fleet-CA certificate but have no managed record here anymore — typically released here without being reset on its own side. Previously that was entirely invisible: the control channel just refused the connection over and over. An **Unrecognized nodes** panel (only rendered when the list is non-empty) now surfaces each one — node id, refusal reason, remote address, attempt count, last-seen time (`GET /api/nodes/unrecognized`) — with **Block** (revokes the cert so it can never enroll or connect again — the control-plane-side "remove" for a node with no row to Release; a confirm dialog is required) and **Dismiss** (clears the entry without revoking; it reappears if the node dials again).
- Adopting a node whose record fails to save on the control plane (after the node itself already committed to pairing) no longer leaves it permanently stranded: the pairing is automatically rolled back so the node stays discoverable and can be re-adopted with a fresh claim code, and the operator sees an actionable error instead of a raw database message.

Both app and node must be on the same LAN segment for UDP multicast discovery to reach the node. Manual adoption by IP+port works across subnets if the node is reachable by HTTPS. The mTLS management port (code fallback 49532; shipped `config.json` sets 39532) must also be reachable from the control plane.

## Fleet Map

A **Map** nav item (top of the **Workspace** group, next to Dashboard) gives the fleet a
geographic and indoor spatial view, entirely offline — no tile CDN, no external map service — so
it works on an air-gapped/intranet install exactly like everything else in this app. It renders
with [OpenLayers](https://openlayers.org/) (`ol` + `ol-pmtiles`, vendored into
`apps/myseliasan/views/react-webpack`), lazy-loaded only when the tab is opened so the ~110KB
gzipped mapping library never weighs down the initial bundle.

The geographic view **is** the Map module — there is no separate "floor plans" tab. A building is
created, positioned and authored entirely from it:

- **Geographic view — site-only digital twin, never a bare node pin**: the map draws exactly
  **one kind of thing**: a **site** (`Site` with a geographic position, `GET /api/sites/overview`).
  A site is one of three **kinds**, chosen once at creation and shown as a distinct marker
  silhouette (disc/square/diamond) so the shape reads even when the name label doesn't: a
  **building** (has storeys, drawn with walls/doors/stairs and stacked by elevation in 3D), an
  **outdoor area** (one open ground surface — a park, yard, campus, car park — with exactly one
  plan, no "how many areas" question), or a **point asset** (a junction, pole, gate, barrier — no
  plan at all; its cameras reach it only through the appliance(s) assigned to it). A node appliance
  **never** gets its own pin — a site, not the node that happens to record its cameras, is the
  map's true anchor for "where is this camera physically": a node's own box can sit in a rack,
  another building, or off-site, while its cameras are placed on that site's floor plans (or, for a
  point asset, simply assigned to it) regardless. There is no standalone/off-site node placement
  any more (the old "place on map" button, node drag-to-place, and the Nodes-layer toggle are all
  gone; the node-pin map layer is kept in the code but is fed no data and stays hidden). Drag a
  site's marker to reposition it (`PUT /api/sites/{id}/position`). An unplaced site (never dragged
  onto the map) stays in the rail rather than plotted at `(0,0)` — see `Site.MapPlaced` in
  `entities/site.go.md`. A marker takes the *worst* status among the nodes that answer for it (own
  cameras inside it, or are assigned to it for a point asset), and its unread-notification badge
  sums only those cameras' alerts (`GET /api/notifications/tally?unread=true`), never a whole
  node's — a node recording cameras in several sites would otherwise over-count every one of them.
  Clicking a building or outdoor marker opens its **floor plans with every camera inside, from any
  node** (`BuildingFloorView`, see below); clicking a point marker instead opens the device card of
  the appliance mounted there (or a chooser when several share the point, or an empty state when
  none do) — there is no plan surface for a point asset to drill into.
- **Site-centric rail**: the side rail lists **every site**, grouped by kind (buildings, outdoor
  areas, point assets — a stable order so the headings don't jump around) — placed and unplaced —
  each with a status dot (worst among the nodes that answer for it) and camera count. A building
  or outdoor row has an expand caret that lazily lists its **floors/areas** (1st floor, Kitchen,
  Carporch…, `GET /api/sites/{id}/floors`, fetched once per expand and invalidated whenever the
  building editor changes its areas) — clicking a floor row jumps straight into the building editor
  on that specific area (not just the first one). A placed row flies the map to it on click; an
  unplaced one enters placing mode the same way it always did. A point asset's row has an **edit**
  (pencil) button that opens a small rename/re-glyph dialog directly, since it has no editor to
  jump into. Nodes not yet assigned to any site appear in a separate **"Appliances"** section below
  the site list, each row offering a site-selector dropdown (`PUT /api/nodes/{id}/building`) —
  assigning one is now the *only* way an appliance is represented on the map, since it stops
  needing a pin of its own the moment it belongs somewhere.
- **Adding an asset**: a **`+ Add`** button in the rail opens a wizard (`asset_wizard.js`,
  replacing the old building-only `building_wizard.js`) whose first question is **what is being
  added** — building / outdoor area / point asset — because the kind decides everything after: a
  **glyph** picker drawn from that kind's own palette (`Site.Icon`, `site_kinds.js`), and, for a
  building only, whether it is a **single area** or has **several areas** (floors, wings, rooms —
  "Ground floor", "1st floor", "Kitchen"); an outdoor area always gets exactly one ground plan and
  a point asset gets none at all — there is no area question for either. Each area becomes a
  `FloorPlan` row under the new `Site`. On save the site and its areas are created (`POST
  /api/sites` with `{name, icon, kind}`, then one `POST /api/sites/{id}/areas` per area — each area
  is a **generated blank white canvas**, not an upload) and the map enters placing mode so the
  operator clicks/drags the marker onto its geographic spot; dropping it opens the building editor
  for a building/outdoor area, so "add an asset" ends on the plan surface rather than back at a map
  with an unexplained new marker — a point asset simply lands on the map with nothing further to
  author until an appliance is assigned to it.
- **Authoring a building or outdoor area**: dropping (or re-opening) one opens
  `building_editor_dialog.js`, a near-fullscreen modal over the map: area tabs along the top
  (add/rename/delete an area — a single-plan outdoor area has none of this), a node/camera palette
  down the side (drag or click-to-pick, then click the plan to place — the same click-first pattern
  as the geographic view; an already-placed camera is greyed out with a note naming where it sits,
  since placement is now **exclusive** — see "Floor plans" below), and the `FloorEditor` canvas
  (`floor_editor.js`) doing the actual drawing, with a **toolset that follows the site's kind**: a
  building offers **Select/Move, Wall, Room** (drag a rectangle → four walls), **Round** (drag a
  box → an elliptical room, decomposed into short wall segments so it reuses the same 2D/3D
  pipeline as a straight wall), **Door** (click a wall to cut an opening, hinge/swing mirrorable for
  the four real door hands, carved in both 2D and 3D with a lintel above it, width adjustable from
  the inspector, a toast if you click off a wall), **Window** (a glazing symbol cut into a wall,
  no swing), **Stairs** (drag a footprint for a straight flight, a chosen step count, its own
  climb height, rotatable ascent direction, an up/down toggle, and — when dropped on a **Platform**
  — locks onto it as its base so the flight's labelled climb reads next to the platform's own
  height), and **Platform** (a raised floor with an adjustable rise in metres, carved under any
  stairs that land on it); an outdoor area swaps the building-only tools for **Gate** (the outdoor
  counterpart of a door) and **Parking** (striped bays), keeping Wall/Room/Round/Platform/Erase. In
  **Select** mode: a multi-select **transform gizmo** (drag to move, corner/edge handles to resize,
  a rotate knob — oriented to the object's own rotation when exactly one is selected) replaces the
  plain move-only selection, **copy/cut/paste** (Ctrl+C/X/V) duplicates geometry with a stepped
  offset on repeated pastes (camera markers are never copied — a camera is placed exactly once
  fleet-wide, so a copy would be something the server must refuse), a **delete** button/key removes
  the selection, and a selected camera gets on-canvas **POV drag handles** to aim and widen its
  coverage wedge directly on the plan instead of only through the inspector. Undo/redo snapshots
  every authored layer together (walls/stairs/doors/windows/platforms/parking) as one history. A
  wall run in progress can be cancelled with **Esc** (Enter or double-click still finishes it)
  without exiting the whole editor. The tool palette and the properties inspector are both
  **dockable panels** — drag either by its grip to float it, or drop it near the left/right edge to
  dock there (dropping both on one side stacks them). **Zoom** — toolbar zoom in/out/fit-to-100%, or
  Ctrl/⌘+wheel — lets the canvas grow past the viewport (scrollbars appear only once zoomed past
  fit). A **2D ⇄ 3D** toggle sits in its own header (a Select-mode marker inspector also sets a
  camera's `mountHeight`/`pitch` for the 3D coverage cone). Walls/scale/wall-height autosave
  (debounced, `PUT /api/floors/{id}/model`); camera placement/move/aim persist immediately
  (`POST`/`PUT /api/floors/{id}/placements`, `PUT /api/placements/{id}`) — there is no separate
  "save" step. A blank area's generated canvas can be replaced with a real uploaded plan (scan/CAD
  export) at any time from the editor's toolbar without losing the drawn walls or placements, and
  a **Remove plan** button (beside Upload plan, confirm-gated) clears an uploaded/drawn plan back
  to that same blank canvas (`DELETE /api/floors/{id}/image`) — walls, stairs, doors and camera
  placements all survive; only the picture is cleared (`FloorPlan.HasPlanImage` is what lets the
  editor know whether there is a plan to remove — see `entities/site.go.md`). Re-entry for an
  existing building/outdoor area: an **edit** button on an unplaced site's rail row (it has no
  marker to click into yet), and an **Edit plan** button in the read-only building drill-down
  (`BuildingFloorView`, below). A point asset has no plan surface — its rail-row pencil opens the
  small rename/re-glyph dialog instead (see "Site-centric rail" above).
- **Floor plans**: an operator's placements (`NodePlacement`) are **myseliasan's own record**, not
  fetched from the node — that is deliberate: the live camera list is fetched over the tunnel and
  returns nothing when a node is offline, so a placement carries a name snapshot and stays
  rendered (using that snapshot) even while its node is unreachable; a placement whose camera no
  longer exists on an *online, reachable* node (as opposed to one simply unreachable right now) is
  flagged as a **ghost marker** for cleanup. **Placement is exclusive**: a camera (or a node's own
  marker) holds at most one pin fleet-wide — dropping one already placed elsewhere is refused
  (`409`, naming the site/area it already sits in) rather than silently duplicating it; move it by
  unplacing it first (`GET /api/placements` backs the palette's "already placed, and where" state
  across the whole fleet, not just the floor being edited). A camera placement carries a
  **coverage arc** (`heading`/`fov` in degrees, dragged into aim via on-marker handles, or the
  editor's on-canvas POV drag handles) drawn as a translucent wedge on the plan, so an operator can
  see at a glance which part of a room a camera actually watches; in the **3D view** the same
  placement's `mountHeight`/`pitch` stand its coverage as a cone over the extruded walls. The 3D
  view renders **only the walls the operator drew** — a floor with no authored `segments[]`
  extrudes as a bare slab, never an invented perimeter box, since an outer wall is something
  authored, not assumed. A floor's authored **stairs**, **doors**, **windows**, **raised floors**
  and **parking** (see "Authoring a building or outdoor area" above) round-trip alongside the wall
  segments in `FloorPlan.Grid` and extrude the same way in every 3D view, not just the editor's
  own; windows render as glazing, a raised floor as a slab (carved underneath any stairs that land
  on it), and stairs rest on their platform when they have one, descending stairs carving a
  stairwell opening into the floor slab above when going down. The **read-only 2D drill-down**
  (`BuildingFloorView`/`node_floor_view.js`) now renders this same authored geometry as a vector
  overlay over the plan image too, not just the 3D tab — a floor with drawn walls no longer looks
  empty in the 2D view. A camera marker with unread
  notifications carries a severity-coloured count badge in **both** the 2D plan and the 3D view
  (`GET /api/notifications/tally?unread=true`, same per-camera attribution as the building marker's
  own badge); the 3D view additionally pulses a beacon on the floor beneath that camera — the same
  two-wave animation as the geographic view's building beacon — shown on every stacked floor but
  only animated on the active/non-dimmed one, so an alert a storey down stays visible without
  piling up motion. Clicking a building marker on the geographic view shows
  `BuildingFloorView` — every floor's placements from **every owning node**, each marker/wedge
  coloured by *its own* node's status and streaming live over *that* node's tunnel, with its own
  2D/3D toggle (three.js code-split in, only loaded when 3D is opened) and, for a multi-floor
  building, a "stack floors" option that renders every floor at its `Elevation` at once — the view
  that makes "the building is the twin, not any one node" concrete.
- **Locate on plan**: from a camera's own context (e.g. the node manager's embedded camera pages),
  **Locate on plan** (`GET /api/node-floorplan/{nodeId}`) jumps straight to the floor plan holding
  that camera's placement and focuses its marker — no need to know which site/floor it lives on.
  Clicking a camera marker on a plan opens a small, draggable, resizable floating window
  (`CameraWindow`) with its live footage (PTZ overlay when supported) on top and recent
  events below; clicking a footage event opens its recorded snapshot/clip **inline over the same
  live panel** (a Back button returns to live without ever tearing down the underlying WebRTC
  stream, since the live tile stays mounted underneath). All windows float over the map, can
  be dragged by their title bar, resized from a corner grip, and toggled small ⇄ maximized without
  restarting the underlying stream.

The geographic view's building markers and the building drill-down's per-node camera
markers/rail rows share the same status vocabulary: **online** (green), **warning** — amber, cert
expiring soon — (reusing the same cert-health signal the Dashboard's "Certs expiring" KPI and the
Nodes table already surface), **critical** —
red, the node is `lost` — and **idle** — grey, `self-dropped` or a legacy/unknown status.

**Offline basemap**: the geographic view's cartography is one or more self-hosted
[Protomaps](https://protomaps.com/) `.pmtiles` **region** archives (a fleet spanning several
disjoint areas is served as several region files rather than one planet-sized archive),
Range-served per region (`GET /api/basemap/tiles/{name}` — the browser fetches byte ranges of one
file and does tile lookup client-side, so there is no tile-server process and no new Go
dependency). Regions are normally provisioned out-of-band (`pmtiles extract`) and dropped at
`<dataDir>/basemap/*.pmtiles`; an empty/absent directory is a supported state
(`GET /api/basemap/info` reports `available: false`) — the map still renders, just without
cartography, rather than a fleet with no archive being unable to see positions at all. Attribution
(`"© OpenStreetMap contributors"`) is a static string, never a network call. **Optionally**, when
an operator (or `MYSELIASAN_BASEMAP_SOURCE`/`MYSELIASAN_PMTILES_BIN` env vars) configures a remote
pmtiles source and the `pmtiles` tool is installed, the UI can **download a new region on
demand** (`POST /api/basemap/download`, a bounding box + max zoom, capped at 25°×25°/zoom 14) —
this is the one action in the whole app that deliberately reaches the internet, and it stays off
by default so an air-gapped install is unaffected.

Floor-plan images are **encrypted at rest** under `<dataDir>/floorplans`, using the same fleet
cipher that protects the CA key and fleet PSK (see "Fleet secret encryption at rest" below) —
only metadata and pixel dimensions live in the database.

Endpoints (all `AuthOnly`, session-gated like every other operator route in this app):
`GET/POST /api/sites` (create body `{name, description, icon, kind}` — `kind` is
`building`/`outdoor`/`point`), `GET /api/sites/overview` (per-site rollup for the geo map),
`PUT/DELETE /api/sites/{id}`, `PUT /api/sites/{id}/position` (drag a site's marker),
`GET/POST /api/sites/{id}/floors`, `POST /api/sites/{id}/areas` (create an area with a generated
blank canvas — the asset wizard/editor's "add an area" path), `GET /api/sites/{id}/floorplans`
(multi-node building/outdoor-area drill-down), `GET/PUT/DELETE /api/floors/{id}`,
`PUT /api/floors/{id}/model` (autosave the 3D layout: grid/scale/wallHeight/elevation),
`GET /api/floors/{id}/image`,
`POST /api/floors/{id}/image` (replace, used by the floor editor and to upload a real plan over a
generated blank canvas),
`DELETE /api/floors/{id}/image` (clear the plan back to a blank canvas — walls/stairs/doors and
placements survive — the building editor's **Remove plan** button),
`GET /api/floors/{id}/background` (pristine background for re-editing),
`GET/POST /api/floors/{id}/placements` (`POST` is `409` when the camera already holds a pin
elsewhere — placement is exclusive), `GET /api/placements` (fleet-wide placement index — every
placement with the site/floor it sits on, for the editor palette's "already placed, where"
state), `PUT/DELETE /api/placements/{id}` (position, and/or
`heading`/`fov` coverage aim, and/or `mountHeight`/`pitch` for the 3D coverage cone),
`GET /api/node-floorplan/{nodeId}` (locate-on-plan drill-down),
`GET /api/basemap/info`, `GET/PUT /api/basemap/config`, `POST /api/basemap/download`,
`GET /api/basemap/tiles/{name}`, `PUT /api/nodes/{id}/position` (a node's own geographic
coordinates), and `PUT /api/nodes/{id}/building` (assign/clear the site a node resides in).
See `docs/modules/apps/myseliasan/apis/{basemap,sites,nodes}.go.md`.

**In-flight redesign (not wired in):** `components/map/` (`geo_map.js`, `inspector.js`,
`asset_browser.js`, `floor_view.js`, `styles/map-workspace.css`) holds early scaffolding for a
unified three-pane "map workspace" shell — asset-browser rail, reworked geographic stage, and a
single contextual inspector card replacing the current scatter of floating popups — intended to
eventually supersede this section's `map_page.js`/`fleet_map.js`/`fleet-map.css`. Nothing imports
these files yet: `App.js` still lazy-loads the shipped `map_page`, so none of the behavior
described above has changed and the new components are not reachable from the UI.

## Fleet rules — cross-domain correlation

**This is the reason the suite has a fourth app (`myiotsan`).** Once `myseliasan` has adopted
both camera nodes and IoT sensor nodes, it can express rules that no single node can evaluate
on its own:

> *motion on Camera 3 (mymatasan) AND a door contact opening (myiotsan) AND no badge swipe
> (myiotsan) → intrusion*

Neither `mymatasan` nor `myiotsan` can see the other's events. Only the control plane, which
already receives every adopted node's events in one feed, is in a position to notice the
conjunction — and the conjunction is where the signal is: a camera's motion alert at 03:00 is a
moth; a door contact at 03:00 is a cleaner; the two together with no badge swipe is an
intrusion.

A **Fleet rules** page (its own nav item) lets a superadmin build these rules: a plain-English
rule list, a clause builder (**required** conditions that must happen, styled apart from
**absent** conditions that must not), each clause optionally scoped to one node, a node kind
(camera/sensor hub), a notification category, and/or a text match against the alert's title —
plus a **window** (how close in time the required clauses must occur to count as one incident)
and a **grace period**.

**The grace period is the part that makes this usable rather than a nuisance.** A badge reader
typically reports a second or two behind the door contact it just authorised. The correlator
never fires the instant a required condition is met — it *arms*, waits out the grace period,
and only then checks whether the absent condition still holds. A badge swipe that arrives
inside the grace window disarms the rule silently: that was an authorised entry, and no alert
is ever raised. Firing on the door alone (no grace) would cry intrusion on every legitimate
badge entry, all day, until someone disabled the rule — and the one real intrusion would then
go unnoticed too.

Writing or deleting a fleet rule (`POST`/`DELETE /api/fleet-rules`) is **superadmin-only** — a
rule that spans the whole estate is itself a security control, and whoever can write one can
write one that never fires. Reading the rule list (`GET /api/fleet-rules`) is open to any
authenticated session, since seeing what a rule does is not the same power as authoring one. A
rule made only of "absent" clauses is refused outright: it would fire on nothing at all,
forever, which is worse than no rule because somebody would trust it.

See `docs/modules/apps/myseliasan/services/correlate.go.md` for the evaluation engine and
`docs/MYIOTSAN_PLAN.md` §8e for how this was verified against real events from a live camera
node and a live sensor node, including a deliberately late badge swipe that correctly
disarmed the rule.

## Notifications

A consolidated **Notifications** page (its own badged nav item under a **System** group) lists the control plane's unified feed — `myseliasan`'s own events (node going-offline, login/security, and now proactive fleet-health alerts — see below), and every event a managed node pushes up its control channel, tagged `source: node:<id>` by `ingestNodeEvent` in `app.go` (any event kind the node reports, not just recognized ones, now surfaces here rather than being dropped). It reads `GET /api/notifications`, with **Unread**/**All** toggle, a per-node source filter, and infinite scroll. The side-nav badge and an SSE-driven live update both come from `GET /api/notifications/stream`: the App shell keeps one `EventSource` open and bumps a refresh signal + re-polls the unread count on every arrival. Clicking **Acknowledge** marks the notification read (`POST /api/notifications/{id}/read`) and — for a node AI detection (`refType: "alert_event"`) — also propagates the acknowledgement to the source alert on the node over the proxy (`POST /api/nodes/{id}/proxy/api/vision/alerts/{id}/ack`), so the node's own review state stays in sync. AI-detection rows show the annotated event snapshot, streamed through the node proxy so the browser never contacts the node directly.

## AI Agent

An **AI Insight** nav item (top of the **Workspace** group, gated on `GET /api/agent` like every other permission-matrix-driven tab) surfaces two things: a deterministic daily **fleet digest**, and an OPTIONAL **"ask the fleet"** chat over the control plane's own data. Together this is the second capability myseliasan has previously had none of at all — the same kind of gap the Settings page and the setup wizard closed.

**The digest is always on and needs no language model.** Every finding it reports — event-volume swings, a spike/quiet break against the fleet's own learned baseline, per-node outages, certificates expiring, noisy sources, fleet-rule firings, sensitive audit activity — is computed in plain Go from data the control plane already holds (`services/agent_findings.go`, `docs/modules/apps/myseliasan/services/agent_findings.go.md`). No LLM is involved in that computation, so a wrong number in a digest is a code bug, never a hallucination. Findings are **structured**, not prose — the frontend localizes each one through its own i18n dictionary (`agent.finding.<code>` in `en`/`ms`/`zh`/`ar`), so a digest generated at 07:00 reads natively in every UI language the suite supports, with nothing baked into English on the server. The digest runs on a schedule (default **07:00 local**, configurable in Settings → agent, or per-operator on demand via **Generate now**) and its own feed entry is published to the unified notification feed like any other system event; the narrator deliberately never reads its own past digest entries back as evidence, so a critical digest can't keep re-triggering itself as "critical activity" forever.

**The language model is a strictly optional layer on top**, off by default (`agent.llm.mode: "off"`). Three modes:

- **off** — the digest ships narrator-only findings, no prose; chat is unavailable (the Insight page shows a plain "no language model enabled" card instead).
- **external** — point at any operator-run OpenAI-compatible endpoint (`llama-server`, Ollama, vLLM, LM Studio) via `agent.llm.endpoint`/`apiKey`/`model`; a **Test** button in Settings probes the submitted values before you save.
- **sidecar** — myseliasan supervises its own `llama-server` child process on loopback (port **49540** by default), so an operator with no inference server of their own can still turn the feature on. The pinned build (llama.cpp release `b10289`) and the default model (**Qwen2.5-1.5B-Instruct Q4_K_M**, ~1.1GB, small enough for usable CPU-only inference and genuinely multilingual across the suite's four UI languages) are fetched from Settings → agent with two **Download** buttons, each SHA-256-verified against a pinned constant before install — a compromised CDN or a repointed release cannot slip a different binary onto the control plane. For **air-gapped sites**, an **Import** button opens the same whitelisted server-side file picker the rest of Settings uses, so an operator can carry a `llama-server` build or any other `.gguf` in on removable media instead; imports aren't checked against the pins (they can be any build), but their SHA-256 is written to the install log and the audit trail so what was imported is on the record. **Downloads are the one action this feature ever takes onto the internet** — same posture as the fleet map's basemap downloader (see "Fleet Map" above): off by default behind `agent.allowDownloads` (default true, i.e. available-but-operator-triggered, never automatic), and the environment variable `MYSELIASAN_AI_DOWNLOADS=off` hard-locks the download route off regardless of config, for sites whose policy is "this box must never egress." The sidecar's binaries/model live under `<dataDir>/llm` and are erased by **Reset to factory settings** (see "Settings" above) along with everything else.

**"Ask the fleet" chat** (only shown once a language model is enabled and ready) answers questions grounded in a single compact document the server assembles per question — fleet status, windowed stats, per-source anomalies, the latest digest's findings, recent high-severity events, capped at 8KiB — under a system prompt that forbids answering from anything else and requires citing `[notif <id>]`/`[node <id>]` when referencing a record. This is a **route → fetch → summarize** design, not an agentic tool loop: the 1-2B CPU models this has to work with can't reliably drive multi-step tool calling, but they can answer a question over a document they're handed. **Grounding is central-only** in this release — everything comes from the control plane's own tables, never a live fan-out to nodes over the tunnel (which could stall a reply behind a 30-second-per-node timeout) — so a question about a specific camera's live state on one node isn't answerable yet. The reply streams token-by-token over Server-Sent Events, since CPU inference is slow enough that perceived latency is the whole UX.

Reading the digest (`GET /api/agent/*`) follows the permission matrix like the rest of the fleet surface, and a brand-new role gets it by default (the same viewer-default seed as Notifications and Nodes); **generating a digest on demand and using chat** (`POST`) is its own curated feature toggle on the Roles page, since both burn real CPU on the control plane. Installing/testing/restarting the LLM layer (`/api/agent/llm/*`) is **superadmin-only** regardless of the matrix, the same self-gating pattern the Settings and System pages use.

See `docs/modules/apps/myseliasan/apis/agent.go.md`, `services/agent_digest.go.md`, `services/agent_findings.go.md`, `services/agent_chat.go.md`, `services/llm_manager.go.md`, `services/llm_sidecar.go.md`, `services/llm_install.go.md`, `services/llm_catalog.go.md`, and `docs/modules/infra/llm/client.go.md`.

## Audit log

`myseliasan` keeps an immutable, append-only audit trail of sensitive control-plane actions in its own `audit_log` table — distinct from `api_log`, which is a per-request HTTP access log subject to retention-based deletion and carries no action semantics. There is no update/delete path and no retention cleanup for `audit_log`: entries are written once by the handler that performed the action and never touched again.

Actions recorded:

- **Node adopt / release** (`node.adopt`, `node.release`) — release notes that the node's certificate was revoked.
- **Node self-drop** (`node.self_dropped`) — a node reports it unpaired itself; attributed to the node (`"node:<id>"`), not an operator.
- **Fleet-key rotation** (`fleet.key_rotate`) — records that a rotation happened; **the key value itself is never recorded**.
- **RBAC changes** (`rbac.set_role`, `rbac.set_disabled`, `rbac.elevate`) — role changes record the before→after role transition.
- **Node access grant changes** (`node_access.set`, `node_access.revoke`).
- **Mutating tunneled node commands** (`node.command`) — every `POST`/`PUT`/`PATCH`/`DELETE` sent through the reverse command tunnel (`/api/nodes/{id}/proxy/...`) is audited, since that single choke point is how remote wipe, factory-reset, and settings writes reach a node. Read-only tunneled traffic (`GET`/`HEAD`) is intentionally not audited, to keep the trail free of routine page-load noise.
- **AI agent actions** (`agent.digest.generate`, `agent.chat` — question text only, truncated, never the model's answer; `agent.llm.test`, `agent.llm.install.binary`/`.model`, `agent.llm.import`, `agent.llm.sidecar.restart`) — see "AI Agent" above.

Recording is best-effort: a failure to write an audit entry is logged but never blocks or fails the action being audited. Each entry captures the actor (user id, email/name, role), the target (type + id), an outcome (`success`/`denied`/`error`), a short detail string, optional structured `Metadata`, and the client IP.

The trail is read-only over `GET /api/audit?limit=&offset=&action=&targetType=&targetId=`, gated to **superadmins only** (the audit log can expose sensitive operator activity). The SPA exposes it as its own **Audit Log** nav item under **Administration** (superadmin-only), styled as a standard admin page (settings-panel + header, like Users/Roles) with per-column filtering and sorting on the shared `DataTable`.

## Settings

`myseliasan` previously had **no in-app settings** — every infra config change (TLS, CSP, rate limit, cache, logging, telemetry, SSO, pairing, local login) needed a manual `config.json` edit and a restart. A **Settings** page (its own nav item under the **System** group, superadmin-only in both the nav gate and the API) now edits a SAFE SUBSET of `config.json` in the browser: `localAuth`, `sso`, `pairing`, `agent` (the fleet AI agent's digest schedule and LLM mode/endpoint/sidecar controls — see "AI Agent" below), `security` (JWT secret, allowed origins, TLS paths, CSP, rate-limit tiers), `storage` (file storage + cleanup, cache/Redis — the cache provider is a dropdown, and when **Redis** is selected the Redis card exposes a **Test connection** button that live-pings the server via `POST /api/settings/cache/test` before you commit, a blank password falling back to the stored one), and `logging` (app/API log retention, Prometheus telemetry). The blocks that could take the whole app offline if mis-set — `db`, `server`, `bootstrap` — are deliberately never exposed here and remain file-only.

Because the shared apphost reads these infra blocks only once at boot, an edit is not a live change: `PUT /api/settings/{section}` validates and writes the change back into both the in-memory config (so the UI reflects it immediately) and `config.json` itself (a surgical, order-preserving patch — untouched blocks keep their exact original bytes/formatting, see `services/settings_materialize.go.md`), then reports `needsRestart: true`. The page shows a persistent "restart required" banner with a one-click **Restart now**, which calls `POST /api/system/restart`; the SPA then polls `GET /api/health` until the relaunched process answers and reloads. A `System` tab on the same page mirrors mymatasan's system panel — running build (application + version, shared-core version, commit, build date, all from `GET /api/version`) and live service health (API, liveness `/health`, readiness `/ready` with its `db`/`cache` advisory items shown as status pills), refreshable on demand — alongside the restart control. Secret fields (`localAuth.password`, `sso.clientSecret`, `jwt.secret`, `cache.redis.password`) are never sent to the browser — the API returns them blank plus a `"<field>Set"` flag, and leaving one blank on save keeps the current value rather than clearing it.

To minimize error-prone typing, the form leans on pickers over free text wherever the value is constrained: every field carries an **(i)** tooltip with plain-language guidance; filesystem paths (TLS cert/key, SSO CA cert, file-storage folder, log file) have a **Browse…** button that opens a whitelisted server-side file/folder picker (`GET /api/settings/fs/browse`, superadmin-gated, confined to the app dir + data dir + home + common install locations, read-only names); the cache provider and the Redis database number are dropdowns; and fields with canonical values (default ports, the multicast address, session-TTL presets, `/metrics`, `/api/auth/callback`, `localhost:6379`) offer one-click autocomplete suggestions. **Save** is disabled until the section's working copy actually differs from what was loaded (a deep compare against the last-loaded values) and shows an inline "unsaved changes" indicator while dirty — saving reloads the section and re-seeds the form so it goes clean again, so there is no way to submit a no-op save or lose track of an unsaved edit by switching tabs unknowingly.

A **Restore defaults** button per section (`POST /api/settings/{section}/reset`) reverts to the values captured once on first run — a snapshot taken automatically the first time the settings service starts and stored (encrypted, when encryption at rest is enabled) in the control-plane database, so defaults are still recoverable even after `config.json` has been hand-edited or overwritten since. Every save and reset is written to the audit trail (`settings.save`/`settings.reset`, section name only — never the values) alongside the other sensitive actions this app records (see "Audit log" above). See `apis/settings.go.md`, `apis/system.go.md`, and `services/settings.go.md`.

The System tab's **Danger zone** offers **Reset to factory settings** — `myseliasan` previously had no way to wipe itself (only the ability to remotely wipe an *adopted node*, see "Wipe" under "Node management" above). Confirmation is GitHub-style: the operator must type `myseliasan` exactly before the button enables, and the server independently re-checks the typed phrase (`POST /api/system/reset`) rather than trusting the dialog. A confirmed reset stops background pollers, crypto-erases the fleet secret key (so the encrypted CA private key and PSK it protects become unrecoverable immediately), erases floor plans/the cached basemap/file storage, drops and rebuilds the control-plane database, and restarts into first-run setup — a blocking overlay polls progress, then `/api/health`, then reloads. **Adopted nodes are not notified**: they keep running with a certificate this control plane no longer recognises and have to be re-adopted afterward. Hidden unless `bootstrap.allowReset` is `true` in `config.json` (myseliasan ships it `false`). See `docs/modules/domain/shared/services/system_reset.go.md`.

The **Single Sign-On** section's hero carries an extra **Import from myidsan** button: it loads the `<app-code>-sso.json` bundle myidsan's Apps page exports (see that app's README), checks the file's `kind` and refuses a `version` newer than this build understands, then fills the section's form fields — issuer, audience, client ID, secret, provider base URL, redirect base URL/path, session TTL — client-side, entirely in the browser. It deliberately does not save: the operator still reviews the filled-in fields and presses the section's own **Save**, since this setting decides who can log in, and nothing is sent to the server until then (no new endpoint or permission is needed). A field the bundle omits is left untouched rather than blanked, so a partial file cannot wipe working config; in particular a bundle exported without a client secret (myidsan only ever exports the plaintext right after generating it) leaves the stored secret in place, per the same "blank = keep current" semantics the rest of this page uses for secret fields.

## Setup Wizard

`myseliasan` previously had **no first-run wizard at all** — every other app in the suite
did. A signed-in superadmin who has cleared the mustchange/pending-clearance gates and has
not yet completed setup (`GET /api/setup/state`) now sees a 6-step wizard before the normal
app shell: welcome, sign-in (import a myidsan `<code>-sso.json` bundle via the same
client-side import the Settings page's SSO section uses, and actually **save** it here,
unlike Settings' review-first import), first site, adopt a node, handover (elevate a real
account to superadmin so the install stops running on the stock one), done. Every step is
skippable; completion is `POST /api/setup/complete`, a single server-side flag
(`sharedservices.ISetupStateService`, the same `domain/shared` seam mymatasan, myidsan and
myiotsan use) shared across browsers, so the wizard never reappears once finished or an
unreachable probe never blocks the operator out of their own control plane.

Deliberately **not** offered, unlike mymatasan's and myidsan's wizards: an alerts step
(myseliasan has no notification-destination API to configure yet) and a restore-from-backup
step (myseliasan has no backup/restore capability at all). See `apis/setup.go.md`.

## Reports

A **Reports** page (its own nav item alongside **Notifications** under the **System** group) generates printable PDF reports of the fleet on demand, rendered entirely pure-Go on the control plane (`domain/report` over `github.com/go-pdf/fpdf`, see `domain/report/doc.go.md`) — no headless browser, so it keeps working on an air-gapped install. Four reports are available, each a `GET /api/reports/*.pdf` under `apis/reports.go`:

- **Fleet Health** (`fleet-health.pdf?range=`) — online/offline status of every node, a certificate expiry roster, and an alert summary (by category and noisiest source) over a selectable trailing window (7/30/90 days).
- **Site & Asset Inventory** (`inventory.pdf?siteId=`) — an asset register per building (or one selected site): the rendered floor plan for each floor/area — including the authored walls/doors/windows/stairs/parking/raised floors from the floor editor's grid, not just the flat plan image — with camera coverage wedges and placement markers, plus a table of on-site appliances.
- **Incident Detail** (`incident.pdf?range=`) — recent alerts over the selected window with per-event detail, including a snapshot inline when the event carried one.
- **Security & Access** (`security.pdf?range=`) — users, roles, the endpoint permission matrix, the audit trail for the selected window, and a data-protection attestation paragraph. **Superadmin only**: the API 403s a non-superadmin caller, and the SPA hides the card entirely so it is never offered to a session that cannot use it.

Every generation — including a superadmin's own Security report — is written to the audit trail (`report.fleet_health`/`report.inventory`/`report.security`/`report.incident`, `TargetType: "report"`) alongside the other sensitive-action entries this page describes above, since a report is a sensitive bulk read of fleet data even though it changes nothing.

**Preview before print.** The page never triggers a blind download: `ReportsPage` (`views/components/reports.js`) fetches the PDF bytes as a blob and opens them in a full-screen modal using the browser's own PDF viewer (an `<iframe>`), with **Print** (drives the iframe's own print dialog), **Download** (saves with the server-suggested filename), and **Close** actions. Report generation needs no CSRF header (`GET`); only the write endpoints elsewhere in the app do.

## Fleet CA

`myseliasan` is its own on-prem certificate authority (ECDSA P-256, 10-year root). The CA key is stored in the local `ControlSetting` table (`pairing.caCert`/`pairing.caKey`) and is never transmitted off-prem. Node private keys are generated on the node and never sent to the control plane — only a CSR crosses the wire. Revocation is "refuse to renew + short TTL" (no CRL/OCSP). The `ManagedNode.Fingerprint` field is reserved for future use; identity is currently verified through the certificate CN.

### Fleet secret encryption at rest

The CA private key (`pairing.caKey`), the control plane's own parent leaf private key (`pairing.parentKey`), and the fleet PSK (`pairing.fleetKey`) previously sat in **plaintext** in the control-plane database — anyone who could read that DB could impersonate the fleet CA or the shared secret. These three values are now encrypted at rest (AES-256-GCM, the same reusable `infra/atrest` module mymatasan uses for its recordings/snapshots/training images) whenever encryption at rest is enabled. Public certificates and the revocation list are not secrets and remain plaintext.

- **Config**: this reuses the exact same `security` block mymatasan documents (`security.encryptAtRest`, `keyPath`, `keyProtector`, `passphrase`/`passphraseFile`/`passphraseEnv`, `recoveryPath` — see the root `README.md` and `docs/TECHNICAL_SPEC.md` "Security Model"); it is not a myseliasan-specific config surface. `encryptAtRest` defaults to **true**, and `keyPath` defaults to `<dataDir>/secret/atrest.key` (myseliasan's own data dir, so its key is independent of mymatasan's).
- **No migration needed**: a legacy plaintext value written before encryption was enabled is read back transparently — enabling the feature (or upgrading to a build that has it) requires no data migration or manual re-encryption step.
- **Fail-closed recovery**: unlike mymatasan (which falls back to a public recovery gate page), myseliasan has no equivalent gate for the control plane — if the key existed before but is missing at startup, `myseliasan` **refuses to boot** rather than mint a new key, since a replacement key would silently orphan the encrypted CA key/PSK and reset the whole fleet's trust (every node would need re-enrollment). Restore the key file or configure `security.recoveryPath`, then restart.

## Frontend

The UI is a React/webpack SPA under `apps/myseliasan/views/react-webpack/`, built into `apps/myseliasan/static/` (content-hashed bundles), mirroring `mymatasan`'s frontend architecture. Myseliasan-only styling lives in `styles/app.css` and the shared RBAC-standard rail in `styles/rbac-standard.css`. Build with `npm install && npm run build` in that directory.

The shell uses the standardized dark icon side-nav (`SideNav` from `components/layout.js`). The **Workspace** group holds **Dashboard**, an **AI Insight** nav item (the fleet digest + ask-the-fleet chat, see "AI Agent" above — only rendered when the caller's role can `GET /api/agent`), and a **Map** nav item (the fleet map — geographic view with in-place building creation/authoring, see "Fleet Map" above; its OpenLayers-based components are lazy-loaded on first open). Below that sit top-level **Live Views**, **Objects**, and **Teach** nav items positioned above a bespoke **Nodes tree**: an expandable branch listing adopted nodes (root item → fleet page/node dashboard, child items → each node's own camera sub-tree, lazily loaded over the tunnel on first expand). Selecting a node opens its `NodeDashboard`; selecting a camera under it opens that camera's full page (Live View/Detection/Recordings/Settings). A single click on a node row now both navigates **and** expands its camera sub-tree (matching the root Nodes row); the caret or a double-click collapses/toggles it. Each camera row shows a liveness dot (green online / red offline / grey unknown) driven by the node-reported camera health, mirroring mymatasan's own camera nav. Admin pages (Users, Roles, Audit Log) appear under the **Administration** group — the former separate RBAC permission-matrix page is now part of the **Roles** page (see "Node management" above), which includes a central **Node Access** matrix where a superadmin assigns per-role node access (**Viewer** / **Operator** / **Admin**). A **System** group holds the badged **Notifications** nav item (see "Notifications" above), a **Reports** nav item (see "Reports" above), and the superadmin-only **Settings** page (see "Settings" above). The side-nav's internal list area now scrolls independently of the fixed brand/account chrome (`--nav-scroll` tokens in `styles/rbac-standard.css`), matching mymatasan. A **pin/auto-hide toggle** in the brand slot (`nav-pin-toggle`, ported from mymatasan's own rail) lets the rail collapse to a 68px hover-expanding icon strip instead of always sitting in the grid flow; the choice is persisted to `localStorage` (`myseliasan_nav_pinned`) and applied via a `nav-autohide` class on `.app-shell`. It only takes effect at `min-width: 1081px` — mymatasan's rail stacks at `<=860px` but this app's stacks at `<=1080px`, and auto-hide is neutralized below that breakpoint since a fixed hover-strip makes no sense in a stacked layout.

**Embedded node pages / design parity**: the camera tab components under `components/nodecam/` are the real mymatasan view source files (`vision.js`, `recording.js`, `previews.js`, `cameras.js` pieces, `ui.js`, `layout.js`, `hooks.js`, `ptz.js`, helpers/constants) copied in verbatim, so mymatasan behavior changes to those files should be ported here too. Two shims adapt them to run against a *remote* node: `nodecam/lib/helpers.js`'s `apiBase()` is repointed at the commander proxy (`setNodeProxyBase`), and `installProxyCsrf` teaches `window.fetch` to attach myseliasan's CSRF token on proxy writes (the copied components issue raw `fetch()` calls that predate the double-submit-cookie requirement below). Styling comes from mymatasan's actual stylesheets, imported as raw strings via a new `@mymatasan` webpack alias + `?raw` CSS rule (`webpack.config.js`), then injected once and CSSOM-scoped under `.nodecam-embed` (`components/node_embed.js`, `nodecam/scoped_css.js`) — this is a build-time re-import, not a manual copy, so mymatasan design changes flow into the embedded pages on the next `npm run build` here with no re-sync step. `components/nodeiot/` mirrors this exact trick for an adopted `myiotsan` node's own device-management pages, scoped under its own embed container and concatenating the `@shared` stylesheets in — see "Node management" above.

`DataTable`, `Tabs`, `CameraHero`, `Toast`/`ToastStack`, and the `icons` set are now sourced from the shared in-repo module at `frontend/shared/` (via `@shared` webpack alias). `Tabs` is the one standardized tab bar (icon + label, accent-underline active state, fixed 16px bar→content gap) used across the Objects, Teach, node Settings dialog, and Nodes pages here as well as mymatasan's own tab surfaces. `CameraHero` is the one standardized camera-page header (breadcrumb + status-tinted tile + name/description + status chips), rendered above the Tabs bar on the node camera page here and on mymatasan's own camera detail page — see "Node management" above. Per-app copies (`components/data_table.js`, `components/icons.js`) have been deleted. The `webpack.config.js` has been updated to resolve `@shared` and add `frontend/shared/src` to module search paths; the babel-loader now uses inline preset config.

**Theming**: three themes are available (Light / Dark / **High contrast**). The high-contrast theme uses black surfaces, white text, bright accents, and strong borders for accessibility. The side-nav responds to the active theme via `--nav-*` CSS tokens (soft light rail in light mode, dark gradient in dark, black in high contrast).

**Multi-language UI (i18n)**: the frontend is fully localized into English, Malay (Bahasa Melayu), Chinese Simplified, and Arabic (العربية). Arabic is RTL; selecting it sets `<html dir="rtl">` via `LangProvider` so the entire layout mirrors automatically. The active language is persisted to `localStorage`. A language switcher (`LanguageDropdown` from `@shared`) appears in the top bar as an inline row of buttons (`English | Melayu | 中文 | العربية`). App-specific strings live one-per-locale under `views/react-webpack/src/views/i18n/` (`en.js`/`ms.js`/`zh.js`/`ar.js`); `views/react-webpack/src/views/i18n.js` ships only English eagerly and dynamically imports the other three as separate chunks (`i18n-ms`/`i18n-zh`/`i18n-ar`) on demand, so a single-language user only ever downloads the translations it uses. A returning non-English user briefly gates first paint while their locale chunk loads (English users never wait), and switching language loads the target chunk before applying it so the UI never flashes English. Translations layer over the shared base dictionary (`frontend/shared/src/i18n/index.js`) via `LangProvider`/`useT()`. Missing-locale keys fall back to English, then to the key itself.

`index.html` is served with `Cache-Control: no-cache, no-store, must-revalidate` (`app.go`'s `serveIndex`): it references content-hashed bundle filenames, so a cached stale copy can keep a browser on an old bundle after a rebuild — the hashed `.js`/`.css` chunks themselves can still be cached immutably.

**Login screen.** The login, forced-password-change, and "access pending" screens (`views/components/auth_screens.js`) now share mymatasan's `.login-screen`/`.login-panel` card chrome, and the brand mark comes from `@shared/BrandLogo` (steel `--brand-mark` tint) instead of a hand-copied SVG. This also fixed a real bug: the card's background was `var(--bg-panel, #fff)`, and `--bg-panel` was never defined, so the card stayed white in dark/high-contrast themes. The login screen calls `GET /api/auth/config` on load and hides the "Continue with myidsan" button when it reports `ssoEnabled: false` (i.e. `sso.providerBaseUrl` is empty) — a standalone install with no federated provider configured does not offer a button that cannot work. `apps/myseliasan/static/favicon.ico` is now a real generated icon (the previous file was an unrelated image); a matching `favicon.svg` is also served. `.login-screen` was also missing `position: relative` and the `.login-lang-switch` out-of-flow rule mymatasan has, so the top-corner language switcher rendered as an in-flow flex child and pushed the centered login card off-center on the login, forced-password-change, and pending-clearance screens; both are now defined here to match mymatasan.

**Hardened response headers.** Like mymatasan, every response gets the shared `securityHeaders` hardening (`X-Content-Type-Options: nosniff`, `X-Frame-Options: SAMEORIGIN`, `Referrer-Policy: strict-origin-when-cross-origin`, `Strict-Transport-Security` over TLS, no `Server` header) — see `docs/modules/domain/utils/middlewares/security_headers.go.md`. `myseliasan`'s `config.json`/`config.dev.json` now also ship the same tested opt-in `Content-Security-Policy` mymatasan uses (`securityHeaders.contentSecurityPolicy`). To keep the CSP free of cross-origin allowances, the front-end self-hosts its Quicksand font (`assets/fonts.css`, copied from mymatasan) instead of loading it from Google Fonts.

**Shared footer**: an `AppFooter` component (`@shared`) renders at the bottom of the workspace, showing the app name, version, shared-core version, short commit hash, and build date (from `/api/version`) and the r450k product tagline. Version fields degrade gracefully when the endpoint is unreachable.

An **access pending screen** is shown to authenticated users with no role assigned (`session/me` returns `pending: true`), instructing them to contact an administrator.

A **superadmin handoff banner** is shown at the top of the workspace whenever `session/me` returns `superadminHandoffPending: true`, with a "Go to Users" shortcut for superadmins.

Because the control plane authenticates with the federated middleware, **state-changing API calls must send the double-submit CSRF token**: the `api()` helper in `lib/helpers.js` echoes the non-HttpOnly `__Host-kopiv2_csrf` (HTTPS) / `kopiv2_csrf` (dev) cookie in the `X-CSRF-Token` header on POST/PUT/PATCH/DELETE. Omitting it yields a 403 (which redirects to the SSO login). The fleet-key card includes a copy-to-clipboard button to avoid copy errors.

## Node liveness

The heartbeat reconciler consults the persistent control channel first — a node holding a live connection is authoritatively online regardless of whether its mTLS port is directly reachable from the parent. The mTLS probe is only a fallback for nodes without a live control connection, and now runs **concurrently** (a bounded 16-worker pool under a 30 s per-sweep budget) instead of one node at a time, so a handful of unreachable nodes can no longer stall the whole sweep past the heartbeat interval — a fleet-scale fix, not a behavior change for a healthy fleet. A node is declared `lost` only after a **grace window** (3× heartbeat interval, floor 90 s) with no contact on either path, so a brief reconnect or firewall blip no longer flaps a healthy node offline. Status `self-dropped` is never overwritten by heartbeat. The adopted-nodes list itself also now pages through the full table (500 rows per page) instead of stopping at a fixed 1000-row cap, so a fleet larger than that is no longer silently truncated in the Nodes list, `Scan` adoption dedup, or the heartbeat sweep.

The heartbeat order of operations:
1. Control channel present (`ControlServer.IsConnected`) → mark **online**.
2. Otherwise, queue for the concurrent mTLS probe pool → mark **online** on success.
3. On failure or on timing out inside the sweep budget, check grace window: if `now - lastSeenAt >= graceSeconds`, mark **lost**; otherwise hold prior status and skip the write.

### Fleet-health alerting

The control plane no longer just tracks liveness passively — every heartbeat sweep also checks each node's certificate health, and every liveness/cert transition is turned into a notification in the unified feed (category `health.check`, source `node:<id>`), so a crashed, partitioned, or renewal-failing node no longer goes unnoticed until someone happens to open the Nodes page:

- **Node offline**: fires once, the instant a node crosses from any status into `lost`.
- **Node back online**: fires once, when a previously-`lost` node becomes reachable again.
- **Node certificate expiring**: fires once per distinct expiry when a node's certificate is within its renewal warn window of expiring, or has already expired — but **only for a node whose auto-renew toggle is off** (see "Certificate auto-renew" above); a node with auto-renew on will have its renewal honoured before it lapses, so its approaching expiry is not actionable and never warns. The window is `pairing.renewBeforeHours` when set (the shipped `config.json` sets `336`, i.e. 14 days, to match the longer 90-day cert lifetime; `config.dev.json` still sets `48`); if unset (`0`) the control plane falls back to 7 days (the node's own renewal-attempt fallback is a separate 48 h — the two sides can differ when the key is left unset). A renewal that pushes the expiry out re-arms the warning for the next approach.

`GET /api/nodes/fleet-status` returns a rollup (`{total, online, lost, selfDropped, unknown, certsExpiring, certsExpired, certWarnDays}`) that backs the Dashboard's **Certs expiring** KPI card.

## Runtime Metrics

Prometheus is enabled by default (see root `README.md` → Telemetry) and scraped from `/metrics`.
Before this, myseliasan exposed 0 app-specific series — a live scrape confirmed it. A control
plane is a relay with no sensors of its own, so its failures are fleet failures with no other
symptom: a node dropping off the control channel looks in the UI identical to one an operator
released on purpose, and a certificate creeping toward expiry has no symptom until it expires.

| Metric | Type | Labels | What it tells you |
|---|---|---|---|
| `myseliasan_nodes_connected` | gauge | — | Nodes currently holding a live control channel — the fleet actually reachable right now. |
| `myseliasan_nodes_adopted` | gauge | — | Nodes adopted in total. The gap to `connected` is the fleet that's supposed to be here and isn't. |
| `myseliasan_control_channel_up` | gauge | — | `1` while the control-channel listener is serving. `0` means no node can reach the control plane — check this first when the whole fleet appears to vanish at once. |
| `myseliasan_fleet_events_total` | counter | `kind` (`node_lost`/`node_recovered`/`cert_expiring`) | Fleet-health transitions (see "Fleet-health alerting" above). A burst of `node_lost` is a network partition or the control channel dying; a steady trickle of `cert_expiring` is enrollment quietly failing across the fleet. |
| `myseliasan_fleet_rule_fired_total` | counter | `severity` | Cross-domain correlation rules firing (see "Fleet rules" above). Low-volume by nature; a spike is either a real incident or a rule mistuned into crying wolf. |
| `myseliasan_notifications_purged_total` | counter | — | Feed rows removed by the retention purge. Flat at zero with retention configured means the purge loop is dead — invisible until the disk fills. |
| `myseliasan_agent_digest_runs_total` | counter | `outcome`, `narrative` (`llm`\|`none`) | Fleet digest generations (see "AI Agent" above). `outcome=ok` with `narrative=none` while a model is configured means the LLM is quietly failing — the digest degrades silently by design, and this is where it shows. |
| `myseliasan_agent_digest_duration_ms` | gauge | — | The last digest generation's wall time, including any LLM polish call. |
| `myseliasan_agent_chat_requests_total` | counter | `outcome` | Ask-the-fleet requests by outcome (`ok`/`llm_unavailable`/`llm_error`/`timeout`/`bad_request`). |
| `myseliasan_agent_llm_requests_total` | counter | `purpose` (`digest`\|`chat`\|`probe`), `outcome` | Raw LLM completions. |
| `myseliasan_agent_sidecar_restarts_total` | counter | — | Managed `llama-server` crash-restarts. A climbing value is a model that doesn't fit the host (OOM) or a corrupt binary/model file. |
| `myseliasan_agent_install_total` | counter | `artifact` (`binary`\|`model`), `method` (`download`\|`import`), `outcome` | Sidecar artifact installs. |
| `myseliasan_task_panics_total` | counter | `task` | Recovered panics in `infra/safego`-supervised background tasks (the heartbeat reconciler, the metrics sampler, etc). A supervised task is restarted automatically on panic, but that alone leaves no other trace than one log line. |

`myseliasan_nodes_connected`/`_adopted`/`control_channel_up` are sampled off the control server
every 10 seconds, keeping the control-channel accept path free of a metrics lock.
`myseliasan_fleet_events_total`, `myseliasan_fleet_rule_fired_total`, and the AI-agent counters
above are counted directly at their respective sites — all are rare-to-moderate, discrete events,
not a hot path.

What's worth alerting on:
- `myseliasan_control_channel_up == 0` — the entire fleet is unreachable.
- `myseliasan_nodes_adopted - myseliasan_nodes_connected` growing — more of the fleet is missing than expected.
- Any increase in `myseliasan_fleet_events_total{kind="cert_expiring"}` — a node with auto-renew off is approaching (or has passed) its certificate expiry and will drop out of the fleet unless an operator enables auto-renew for it.
- A rising `myseliasan_task_panics_total` for any `task`.
- `myseliasan_agent_digest_runs_total{outcome="ok",narrative="none"}` climbing while `agent.llm.mode` is not `off` — the LLM layer is enabled but silently never contributing.
- A rising `myseliasan_agent_sidecar_restarts_total` — the managed model process is crash-looping.

## Networking / operations

- Discovery is UDP multicast (group `239.255.90.21:49531` by default) sent and received on **all** multicast-capable interfaces, so multi-homed hosts and same-host dev work. The host firewall must allow inbound UDP on the discovery port (49531) and TCP on the mTLS management port (39532), the control channel port (39533), and the media channel port (39534) — the shipped `config.json` defaults; the code falls back to 49532/49533/49534 respectively if any of those fields are unset.
- Docker's default bridge network does not forward multicast — run with host networking, or use manual adoption by IP+port (which needs no multicast).
- Discovery is same-subnet only (UDP multicast does not route); manual adoption works across subnets as long as the node's HTTPS + mTLS ports are reachable.
- When `myseliasan` and `mymatasan` run on **separate machines**, add `"parentBaseUrl": "https://<parent-LAN-IP>:3002"` to `pairing` in `config.json`. Without it the node uses `sso.redirectBaseUrl`, which is `localhost` in the default dev config and is unreachable from another machine. `parentBaseUrl` is the address the node dials for the control and media channels and uses for enroll/self-drop callbacks.
- For cross-network node camera live view, add a `nodeStream` block to `config.json` (see `config.nodestream.sample.json`): set `publicIps` to the parent's external IP(s), `udpPort` to a single open UDP port, and optionally `iceServers` with a TURN server. For same-LAN use, the `nodeStream` block can be omitted entirely.
- The API rate limiter exempts the node command-tunnel proxy (`/api/nodes/{id}/proxy/...`), node camera WebRTC signaling, and range-streamed recording playback (`/api/nodes/{id}/recording-stream/...`) from its generic per-path bucket, since a single node session fans many requests through those few paths — without the exemption a normal session could trip `429` and the node would appear "online but can't load data." `authOnly.requests` in `config.json` was also raised from 120 to 1200 per window as a further margin. These surfaces stay authenticated and per-node-access-gated at their own handlers; node CRUD/adopt/release/wipe/access endpoints are unaffected and remain rate-limited normally.
- The AI agent's optional download route (`POST /api/agent/llm/install/{binary,model}`, "AI Agent" above) is the second deliberate internet-reaching feature in this app, after the basemap downloader — both default to operator-triggered-only and both honor an environment hard lock: `MYSELIASAN_AI_DOWNLOADS=off` disables the LLM downloads regardless of `agent.allowDownloads`, for sites whose policy is "this box must never egress." Air-gapped sites use the Settings page's Import button instead, which never touches the network.
- `myseliasan`'s own `GET /api/ready` (the control plane's, not a node's — see "Node management" above for the per-node mirror) now reports fleet-listener health as **advisory** fields alongside the usual `ok`/`db`/`cache`: `controlChannel` (up/down — is the parent↔node control-channel listener's serve loop active), `connectedNodes` (how many nodes currently hold a live control connection), and `mediaRelay` (up/down — is the node-camera media relay listener active). These never flip the readiness verdict itself, which stays gated on db + cache only, so a crashed fleet listener alone won't make an orchestrator stop routing traffic — it's a signal for an operator/monitor that the control plane can no longer talk to any node, not a liveness gate. Gating readiness on the fleet listeners would be a separate core-scoped change to the shared `apphost.ReadinessReporter` contract and was deliberately not made here.

## Packaged Releases

Besides `go run . -app myseliasan` from a source checkout, `myseliasan` ships to full
packaging parity with `mymatasan`: GoReleaser (`.goreleaser.myseliasan.yaml`) builds
linux/windows/darwin × amd64/arm64 archives, `.deb`/`.rpm` packages (install to
`/opt/myseliasan` with a bundled systemd unit and a dedicated `myseliasan` service
user), a Windows Inno Setup installer (`packaging/windows/myseliasan.iss`, registers a
native Windows service and shows a one-time generated admin password on first
install), and multi-arch Docker images (`ghcr.io/mysayasan/myseliasan`). It is pure Go
— no ffmpeg, no Python, no model weights — so the archive is just the binary, `static/`,
and a default `config.json`.

`myseliasan` releases under its **own tag namespace** (`myseliasan-v<version>`, via
`.github/workflows/release-myseliasan.yml`) and is published with `--latest=false`, so
it is versioned and released independently of `mymatasan` and never becomes this
repository's GitHub "latest" release — that stays `mymatasan`'s, since its in-app
self-updater reads `releases/latest`. See `docs/TECHNICAL_SPEC.md` → "Versioning
Model" for the full tagging mechanics and `deploy/README-myseliasan.md` for
install/first-run/upgrade details.

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
- `localAuth.username` / `localAuth.password`: stock superadmin credentials (default `admin`/`admin123`; must change on first login). The dev config omits `localAuth` entirely, so a fresh local `data/myseliasan.db` generates a random per-install password each first run instead — check the console banner or `INITIAL_ADMIN_LOGIN.txt` (see "Self-RBAC and user management" above), or set `LOCAL_ADMIN_PASSWORD` before `go run` for a stable dev credential.

Run MyIDSan first, then run:

```bash
ENVIRONMENT=dev JWT_SECRET=replace-with-strong-secret go run . -app myseliasan
```
