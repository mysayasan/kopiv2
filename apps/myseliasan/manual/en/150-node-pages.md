---
title: Working inside a node
category: fleet
categoryLabel: Fleet
summary: A node's own screens, opened from here over the tunnel — and who decides what you may do.
order: 150
---

# Working inside a node

**Manage** on a node opens that node's own screens from inside this control plane: its dashboard,
its cameras, its events, and — depending on what kind of appliance it is — its devices, doors or
alert rules.

Your browser never connects to the node. Everything travels over the control channel the node
dialled outward, which is what makes an appliance at a remote site behind NAT workable from here at
all.

## What you get per kind of node {#kinds}

A **camera recorder** shows its dashboard, cameras, events and a remote console.

A **sensor hub** shows its devices, alert rules, alert log, device types and provisioning.

A **door controller** shows its doors and their access.

The pages are the node's real screens, not a summary of them: everything is read from — and written
to — the node itself.

## Live video {#video}

The camera tiles carry **full-motion live view**, relayed over the node's secure media channel and
re-broadcast to your browser via WebRTC.

A tile that cannot establish WebRTC falls back automatically to snapshots roughly every 1.5
seconds. That is a degraded view rather than a failure, and it is usually a network telling you
something about itself.

For everything a node can do — the screens that are not surfaced here — **Open node UI** goes to the
appliance's own interface directly.

## The node decides what you may do {#authorization}

This is the part worth understanding, because it is not what people expect.

Your permission to *reach* a node's pages comes from this control plane. Your permission to *do
things there* is evaluated **by the node itself**, against your [node access](users-and-roles#node-access)
grant — viewer, operator or admin.

So a refusal you meet inside these pages did not come from here. On a sensor hub, for instance, a
viewer sees devices and their current readings; an operator also sees telemetry history and can
acknowledge alerts; an admin can command devices. Ask for a higher grant on that node rather than a
broader role here.

The practical guarantee is worth stating plainly: when the node refuses a command, **nothing was
sent**. It says so, and the device was never touched.

## An empty page may mean unreachable {#offline}

If the node is offline, these pages have nothing to read.

The screens say so explicitly, and the distinction matters more than it sounds: *an empty page here
means unreachable, not empty*. A door list showing nothing because the controller is unplugged looks
exactly like a controller with no doors, and only one of those is a problem you need to act on
today.

## The remote console {#remote}

**Remote** calls the node's API over the tunnel directly.

It is the escape hatch for anything the embedded pages do not cover. The node still enforces its own
authorization — read-only access rejects writes — so the console cannot be used to get around a
grant.

Streaming endpoints (live video, server-sent events) are **not tunnelable** and will not work here;
use the camera tiles or the node's own UI for those.

## When a node's pages will not load {#troubleshooting}

1. **The node is offline.** Everything above needs it connected.
2. **You have no node access**, or not enough of it — the refusal came from the node.
3. **It is a streaming endpoint** in the remote console, which the tunnel does not carry.
4. **The node is online but slow.** These pages are live calls to another machine over a tunnel, not
   cached copies held here.
