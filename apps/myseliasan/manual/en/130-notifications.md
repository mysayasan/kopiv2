---
title: The notification feed
category: fleet
categoryLabel: Fleet
summary: Every event from every node in one place — and how to keep it worth reading.
order: 130
---

# The notification feed

**Notifications** is every event across the fleet — this control plane and all connected nodes — in
one place.

That single list is the reason the control plane exists. Each node has its own alerts, but nobody
watches six appliances; one feed, in time order, is something a person can actually keep an eye on.

## What arrives here {#sources}

Three kinds of thing, mixed by time and separated by **Source**:

- **Rule alerts** from nodes — a detection rule firing on a camera node, a threshold on a sensor
  node, a door event.
- **Health and status** — a node going quiet, a certificate approaching expiry, storage problems.
- **Security** — sign-in events and sensitive actions on the control plane itself.

Filter by source to answer "what is *this* node doing", and by **Kind** to separate an alert you
must act on from a diagnostic you merely want to see.

## Unread is a work queue, not a badge {#unread}

The feed opens filtered to **Unread**, and that is the useful way to read it.

**Acknowledge** an item when you have dealt with it. Treated properly, unread means "nobody has
looked at this yet" and the count is a real queue. Treated as decoration — acknowledged in bulk
without reading, or never at all — the number stops meaning anything and the feed becomes wallpaper.

## Evidence attached to an event {#evidence}

An alert from a camera node carries the picture that caused it, and often the clip.

**Enlarge** opens the snapshot; **View clip** plays the recording around the event, streamed from
the node over the control channel. If a clip is missing, the node either did not record that
camera or has already rotated the footage away — the feed does not hold video itself, it points at
the node that does.

## The feed is live, and history lives in the node {#live}

New events arrive as they happen while the page is open.

The control plane keeps the event rows. The **footage** stays on the node that recorded it. That
division is deliberate — video is large and belongs where it was captured — and it is why a
released or wiped node takes its clips with it while its event history remains here.

## Keeping it worth reading {#noise}

A feed nobody reads is worse than no feed, and the usual cause is a single noisy source.

The digest names this directly: it reports a source that raised many events of which most are
unread, on the grounds that a source people never read is a source that should be tuned. When you
see that, fix the rule on the node rather than raising your tolerance — or express the rule
properly as a [fleet rule](fleet-rules) with an absence, which is what turns a noisy sensor into a
trustworthy signal.

## Retention {#retention}

The feed does not grow forever unless you let it.

Retention is a configuration setting, and when it is off the digest eventually raises a finding
saying so, with the row count. An unbounded table is a slow problem rather than a dramatic one,
which is exactly the kind that goes unnoticed until it is large.
