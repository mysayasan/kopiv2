---
title: Adopting a node
category: fleet
categoryLabel: Fleet
summary: The fleet key, the claim code, and what to do when discovery finds nothing.
order: 110
---

# Adopting a node

Adoption is the one procedure that spans two products, which is why it is worth reading once
rather than guessing. It happens in three moves: share a key, consent, adopt.

## 1. Generate the fleet key {#fleet-key}

Generate the **fleet key** here, once, and paste it into every appliance you intend to adopt —
on a MyMataSan node that is **Settings → Connectivity**.

The key is what makes a node discoverable: discovery probes are signed with it, so **only a
control plane holding the same key can see the node at all**. Without a key, a node does not
answer discovery.

That is the security property, and it cuts both ways: treat the fleet key like a password.
Anyone holding it can find and attempt to adopt your appliances on the local network.

## 2. Get a claim code from the node {#claim-code}

On the node, generate a **claim code**. It is short-lived on purpose.

The two-step handshake is deliberate. The key says *which fleet you belong to*; the code says
*and I consent right now*. Neither alone is enough, so a stolen key cannot silently absorb
somebody else's appliance.

## 3. Adopt {#adopt}

Back here, **Discover** scans the local network for unpaired nodes and lists what answers. Pick
the node, enter its claim code, and adopt. The name defaults to the node's hostname — rename it to
something people say out loud.

A node that discovery cannot see can be adopted **by address** instead, which is the normal route
for a different subnet.

## One control plane, permanently {#single-parent}

An adopted node is **locked to one control plane** and stops answering discovery entirely.

That is what stops a second control plane on the same network quietly adopting an appliance that
already belongs to somebody. There is no "adopted by two fleets" state, and this is also why an
already-adopted node never shows up in Discover.

## When discovery finds nothing {#troubleshooting}

In order of likelihood:

1. **The node has no fleet key**, or a different one. Discovery is silent by design without a
   matching key — this is the most common cause by a wide margin.
2. **The node is already adopted**, by this control plane or another. It has gone silent. Check
   its own Connectivity page.
3. **The node is on another subnet or VLAN.** Discovery uses multicast, which routers do not
   forward by default. Adopt by address instead.
4. **The claim code expired.** Generate a fresh one.
5. **A firewall is dropping the discovery traffic.**

A node discovery cannot see is not a node you cannot adopt. Discovery is a convenience; adoption
by address always works.

## Releasing a node {#releasing}

**Release** it from here — the tidy route, which leaves this control plane's records consistent.

If this control plane is gone or unreachable, the node can **self-drop** from its own Connectivity
page instead. Either way the node becomes discoverable again and this control plane loses access.
If a node self-drops, clean up the stale entry here.

## After adoption {#after}

The node's cameras, health and alerts start rolling up immediately. Two things are worth doing
next:

- **Place it on the map** — at its site, and on a floor plan if you have one. Until you do, it is
  a row in a list rather than a place.
- **Check who can see it.** Access to a node is granted per role, so a newly adopted node is not
  automatically visible to everyone.

## Ports, and why remote sites work {#ports}

Fleet traffic uses its own ports, separate from the web interface: discovery is multicast on the
local network, and the node-to-control-plane channel is mutually authenticated with certificates
issued by the fleet's own authority.

The node **dials outward**. A node behind NAT at a remote site therefore needs no inbound port
forwarding, which is usually what decides whether that site can be managed remotely at all.
