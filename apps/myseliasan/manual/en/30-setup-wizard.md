---
title: The first-run setup wizard
category: getting-started
categoryLabel: Getting started
summary: Six steps from an empty control plane to a working one — and which you can skip.
order: 30
---

# The first-run setup wizard

The wizard runs once, the first time an administrator signs in, and walks through six steps:
Welcome, Sign-in, First site, Add a node, Handover, Done.

**Every step can be skipped**, and everything it does is available later from the ordinary pages.
Its purpose is to get the control plane out of an empty state, not to extract every decision from
you up front. Each step is under a minute.

## Welcome {#welcome}

Confirms who you are signed in as and whether the bootstrap password has been changed, then
summarises what the remaining steps do. Nothing to fill in.

## Sign-in {#signin}

Points the control plane at your identity server, so people sign in with the account they already
have.

Import the SSO bundle MyIDSan's Apps page exports for this control plane, and the issuer,
audience, client ID and redirect URLs are filled in and saved exactly as MyIDSan wrote them —
rather than retyped between two consoles, which is where these get wrong.

**No identity server?** Skip it. The local bootstrap account you are using keeps working, and you
can connect one later from Settings.

## First site {#site}

Creates somewhere to put your appliances.

A **site** is a physical place with a position on the map. Without one, an adopted node has
nowhere to sit and the map has nothing to show. Name it the way people at your organisation refer
to it — *North Depot*, *Head Office* — because that name appears next to every alert that comes
from it.

Floor plans inside a site come later, from the Map page; this step just needs the place to exist.

## Add a node {#node}

Adopts your first appliance. This is the step the whole control plane exists for, and the one with
a prerequisite: **the node must already hold your fleet key.**

Briefly: generate the fleet key here, paste it into the node's own Connectivity settings, generate
a claim code on the node, and enter it here. Full detail — including what to do when discovery
finds nothing — is in [Adopting a node](adopting-nodes).

Skip it if the appliance is not ready yet. Nothing else in the wizard depends on it.

## Handover {#handoff}

Moves you off the bootstrap account.

The account you are using was created so you could get in. It is shared, it is not attributable to
a person, and every action it takes lands in the audit log under a name that identifies nobody.
This step is where you promote a real account to superadmin and retire the bootstrap one.

It is the step most likely to be skipped and the one most worth doing. A control plane still being
run six months later from `superadmin` has an audit log that cannot answer the only question it
exists to answer.

## Done {#done}

Summarises what was set up and closes the wizard. It does not reappear.

## What to do next {#next}

The wizard leaves you with a working control plane, not a finished one. The usual next steps:

- **Adopt the rest of your appliances** — [Adopting a node](adopting-nodes).
- **Give the map something to show** — add floor plans to your site and place nodes on them.
- **Create the roles your organisation actually has**, and stop handing out superadmin. See
  [A tour of the workspace](workspace-tour#menu-differences).
