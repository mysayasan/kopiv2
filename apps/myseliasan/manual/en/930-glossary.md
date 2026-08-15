---
title: Glossary
category: reference
categoryLabel: Reference
summary: The words this product uses, and what they mean here specifically.
order: 930
---

# Glossary

**Absence** — a condition in a [fleet rule](fleet-rules#absence) requiring that something did
**not** happen. It is how a rule expresses innocence: a matching event disarms the rule instead of
firing it.

**Adoption** — bringing a node under this control plane's management, using the fleet key and a
claim code. See [Adopting a node](adopting-nodes).

**Audit log** — the append-only record of sensitive actions, including refused ones. See
[The audit log](audit-log).

**Basemap** — the streets and terrain drawn under the map, served from a local PMTiles file.
Independent of your own data; without one the map still shows nodes and buildings.

**Bootstrap handoff** — retiring the stock superadmin once a real superadmin account is working.

**Claim code** — a short-lived code generated **on a node**, proving the node consents to being
adopted right now. The fleet key says which fleet; the claim code says *and I agree*.

**Control channel** — the connection a node dials **outward** to this control plane, used for
management and for streaming clips. It is why a node behind NAT needs no inbound port forwarding.

**Control plane** — this application: the thing that manages many appliances from one screen. It
does not record video.

**Cooldown** — how long a fleet rule stays quiet after firing, so one incident is one alert.

**Digest** — the periodic account of what the fleet did, built from computed findings and
optionally narrated by a language model. See [The fleet digest](fleet-digest).

**Finding** — one computed observation inside a digest (a node gone quiet, a spike, a certificate
expiring). Produced by ordinary code, not by a model.

**Fleet key** — the shared secret that makes nodes discoverable to this control plane and nothing
else. Treat it like a password.

**Fleet rule** — a rule that correlates events across **different** nodes. See
[Fleet rules](fleet-rules).

**Grace delay** — how long a rule waits before believing an absence, so a badge reader reporting
slightly after the door contact does not produce a false intrusion.

**Node** — one managed appliance: a camera recorder, a sensor hub, a door controller.

**Node access** — permission to drive the node **itself** over the tunnel, at viewer, operator or
admin level. Separate from the control plane's own permission matrix.

**Pending** — an authenticated account with no role yet. It sees an access-pending screen until an
administrator clears it.

**Permission matrix** — the per-role rules over API path prefixes and verbs. Longest matching
prefix wins; no rule means denied. It governs the API **and** the menus. See
[Users, roles and access](users-and-roles#access).

**Placement** — a camera pinned to a spot on a floor plan. A camera has exactly one.

**PMTiles** — the single-file map-tile format used for the offline basemap.

**Release** — removing a node from this control plane so it can be adopted again. Compare **wipe**,
which erases the node.

**Self-drop** — a node unpairing from its own side, for when the control plane is gone or
unreachable. Leaves a stale row to clean up here.

**Sidecar** — the local inference server this app starts and supervises when the language model
runs on the control plane itself. See [Setting up a language model](language-model#modes).

**Site** — a location grouping everything that stands there. Maps and reports can be scoped to one.

**Superadmin** — a role that bypasses every permission check. Model installation and the settings
editor are superadmin-only regardless of the matrix.

**Unrecognized node** — a node holding a valid certificate that this control plane has no record
of. See [Managing a node](managing-nodes#unrecognized).

**Window** — how close in time a fleet rule's required events must be to count as one incident.

**Wipe** — a remote factory reset of a node: recordings, cameras, rules, users and settings erased,
then a restart. It cannot be undone.
