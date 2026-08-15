---
title: Managing a node
category: fleet
categoryLabel: Fleet
summary: Rename it, open its own screens, keep its certificate alive, release it, or wipe it.
order: 120
---

# Managing a node

Once a node is adopted it appears in **Nodes** with its status, when it was last seen, and when its
certificate expires. Everything you can do to it starts there.

## Details are labels, not settings {#details}

**Manage → Details** sets the node's name, description and nav icon.

These are **control-plane labels — they change nothing on the node itself**. Renaming a node here
does not rename the appliance; it renames the row you and your colleagues read. That is the point:
name it something people say out loud, and put the location in the description, because a fleet of
"nvr-01, nvr-02" is a fleet nobody can reason about at three in the morning.

## Working on the node itself {#node-pages}

**Manage** also opens the node's own screens — its cameras, its dashboard, its recordings — from
inside this control plane.

Those pages reach the node over the fleet's control channel. Your browser never connects to the
node directly, which is what makes a node at a remote site behind NAT manageable from here at all.

A node must be **online** for any of this to work. Discovery, camera scans and settings changes all
run on the node, not here, so an offline node can be renamed but not configured.

## The certificate, and the quiet way a node leaves the fleet {#certificate}

Every node holds a certificate issued by the fleet's own authority, and it expires.

A node renews its own certificate before expiry — no action needed — **but the control plane only
allows that renewal when auto-renew is on for that node**. Leave it off and the certificate simply
lapses. Nothing breaks loudly: the node just loses its fleet connection one day and goes offline.

The Nodes list shows **Cert expires** for exactly this reason, and the digest raises a finding as
expiry approaches. If you only read one column on that page, read this one.

## Releasing {#releasing}

**Release** removes the node from this control plane and lets it be adopted again.

This is the tidy route and it leaves both sides consistent. If this control plane is gone or
unreachable instead, the node can self-drop from its own Connectivity page — after which you should
clean up the stale row here.

## Unrecognized nodes {#unrecognized}

Sometimes a node keeps trying to connect with a **valid certificate** while having no record here.

That is usually a node that was released on this side but never reset on its own, or one whose
record was lost. It appears in **Unrecognized nodes** rather than being silently ignored, because a
certificate that still works and an unknown owner is exactly the combination worth looking at.

**Block** revokes that certificate, so it can no longer connect or re-enroll. To stop it trying
altogether, factory-reset the node itself as well.

## Wiping a node remotely {#wipe}

**Wipe** factory-resets the node: it erases recordings, cameras, AI rules, users and settings, then
restarts. It cannot be undone.

It runs behind a countdown you can cancel, and it only works when the node is online **and** allows
remote reset (`bootstrap.allowReset` on the node). A node that refuses is not broken — it is
configured not to let a remote control plane erase it, which is a defensible thing for an appliance
to insist on.

Release is what you want when you are re-homing a node. Wipe is for decommissioning or handing the
hardware to somebody else.

## When a node shows offline {#offline}

In order of likelihood:

1. **The node is genuinely off**, or has lost its network.
2. **Its certificate lapsed** because auto-renew was off. Check the Cert expires column first — this
   is the one that surprises people.
3. **The node cannot reach this control plane.** The node dials outward, so check its side: a
   changed control-plane address, a proxy, or a firewall now blocking the outbound connection.
4. **The node was released or reset on its own side** and no longer belongs to this fleet.

An offline node keeps its history here. Nothing you have already collected is lost while it is
away.
