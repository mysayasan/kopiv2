---
title: What MySeliaSan is
category: getting-started
categoryLabel: Getting started
summary: The control plane, and the four words the rest of the product is built from.
order: 10
---

# What MySeliaSan is

MySeliaSan is the **control plane** for a fleet of appliances. Each appliance — a MyMataSan
recorder, a MyIoTSan hub, a MyPintuSan door controller — does its own job on its own site. This is
where you watch all of them from one screen.

It does not replace them. Your footage still lives on the recorder that captured it, detection
still runs there, and a site keeps working exactly as before if this control plane is switched
off. What you gain is one place to see the whole estate, and one place to manage it from.

## No way out to the internet {#air-gap}

MySeliaSan is built to run with **no outbound connection at all**. The map ships its own tiles,
the fonts are served from the appliance, the AI assistant runs locally, and this manual is
compiled into the binary.

That is not a limitation somebody worked around — it is the point. A control plane that watches
every camera on an estate is precisely the machine you do not want reaching the internet.

## The four ideas {#concepts}

**Node.** One adopted appliance. A node is discovered on the network, adopted once, and from then
on belongs to this control plane and no other. See [Adopting a node](adopting-nodes).

**Fleet.** Every node together. Anything described as "fleet-wide" — the live-view wall, object
search, rules — spans nodes rather than living on one.

**Site.** A physical place, with a position on the map and optionally floor plans inside it. Nodes
and their cameras are placed on those plans, which is what turns a list of appliances into a
picture of an estate.

**Role.** What a signed-in person may do. Unlike a single appliance, a control plane usually has
several kinds of user — someone who watches, someone who administers, someone who only ever reads
a report — so roles here are yours to define rather than a fixed set. See
[A tour of the workspace](workspace-tour#menu-differences).

## What it does with a node {#what-it-does}

Once a node is adopted:

- Its **cameras and health** roll up here, so an offline camera three sites away is visible from
  this screen.
- Its **alerts** arrive in the shared notification feed.
- Its **pages open from here**, over a secure tunnel — your browser never has to reach the node
  directly, which is what makes a node behind a NAT at a remote site manageable at all.
- It appears on the **map**, at its site, and on a floor plan if you have placed it.

The connection is dialled **outward, by the node**. That is why a remote site needs no inbound
port forwarding, and it is usually the deciding factor in whether a site can be managed remotely
at all.

## Where to go next {#next}

- Setting this up for the first time: [Signing in for the first time](first-sign-in).
- Getting your first appliance in: [Adopting a node](adopting-nodes).
- Looking around: [A tour of the workspace](workspace-tour).
