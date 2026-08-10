---
title: Troubleshooting
category: reference
categoryLabel: Reference
summary: The things that actually go wrong, in the order they actually happen.
order: 910
---

# Troubleshooting

Each entry starts with the most likely cause, not the most interesting one.

## Discovery finds no nodes {#discovery}

Almost always the **fleet key**: discovery is silent by design without a matching one, and a node
with no key or a different key does not answer at all.

Then, in order: the node is **already adopted** (adopted nodes go silent), it is on **another
subnet** (multicast is not routed), the **claim code expired**, or a firewall is dropping the
traffic. Adoption **by address** always works when discovery cannot see the node — see
[Adopting a node](adopting-nodes#troubleshooting).

## A node shows offline {#node-offline}

Check the **certificate** before anything else. A node renews its own certificate, but only while
auto-renew is on for it; left off, the certificate lapses and the node drops out of the fleet with
nothing breaking loudly. The Nodes list shows *Cert expires* for this reason.

Otherwise: the node is genuinely off, it **cannot reach this control plane** (it dials outward, so
check its side — address, proxy, firewall), or it was released or reset from its own end. See
[Managing a node](managing-nodes#offline).

## A node keeps connecting but is not in the list {#unrecognized}

It appears under **Unrecognized nodes**: a valid certificate with no record here, usually a node
released on this side but never reset on its own.

**Block** revokes its certificate. To stop it trying at all, factory-reset the node too.

## A fleet rule never fires {#rule-silent}

The **text does not match** far more often than anything else — it is matched against the node's
own rule name, so check the exact wording in the [feed](notifications) rather than typing what you
expect it to say.

Then: the **window is too short**, an **absence is disarming it** (a badge arriving inside the
grace delay is correct behaviour — the entry was authorised), the **node type is wrong**, or the
rule is off or still in cooldown. See [Fleet rules](fleet-rules#troubleshooting).

## Too many alerts {#noise}

Add an **absence** before you raise a threshold: "motion at the loading bay" fires all day; "motion
with no badge swipe and no scheduled delivery" fires when something is wrong.

Raising the cooldown collapses a storm into one alert but does not make a wrong rule right. If one
source dominates, fix the rule **on the node** — see [the feed](notifications#noise).

## A colleague cannot see a page {#permissions}

A missing menu is a missing permission; there is no display setting.

In order: they have **no role yet** (the pending screen), their role has **no rule** for that path
(no rule means denied), the **menu switch** is off, they lack **node access** rather than
control-plane access, or the account is disabled. See
[Users, roles and access](users-and-roles#troubleshooting).

## The map has no streets {#map-blank}

No **offline basemap** is installed. Markers, buildings and floor plans still work — only the
background is missing. See [The fleet map](the-map#basemap).

Region download needs the `pmtiles` tool on the server and **reaches the internet**, which is the
wrong move on a site meant to have no egress.

## Names are blank in a PDF report {#report-blank}

Report text uses a Latin-script font, so **CJK and Arabic characters in names render blank**. Give
the affected buildings, areas or nodes Latin-script names, or keep a Latin name alongside the local
one. See [Reports](reports#latin-only).

## The assistant will not answer {#assistant}

- *No language model is enabled* — the expected state of a fresh install. The digest still works,
  and you can still **search the manuals**. See [Ask the fleet](ask-the-fleet#no-model).
- *Still loading* — a model takes time to load; retry shortly.
- *Failed* — check the sidecar state and use **Restart sidecar**. See
  [Setting up a language model](language-model#degradation).
- *Answers in the wrong language* — a small model drifting back to English is a model limitation,
  not a setting.

## A setting did not take effect {#settings}

Settings are written to `config.json` and apply **after a restart**. Saving alone changes nothing
running.

If the app will not start after a change, correct the value in `config.json` on disk — see
[The settings editor](settings#recovery).

## An event has no clip {#no-clip}

The feed holds the event row; the **footage stays on the node** that recorded it. A missing clip
means that node did not record that camera, or has already rotated the footage away. See
[the feed](notifications#evidence).

## When you need to know what happened {#audit}

Read the [audit log](audit-log#using). A node that disappeared was either released by somebody,
self-dropped from its own side, or is simply offline with nothing logged — three different answers
that look identical from the Nodes list.
