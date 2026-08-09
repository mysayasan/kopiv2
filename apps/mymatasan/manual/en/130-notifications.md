---
title: Notifications and the alert log
category: daily-use
categoryLabel: Daily use
summary: Work the event feed, read a detection, and acknowledge what you have dealt with.
order: 130
---

# Notifications and the alert log

The Notifications feed is where everything the appliance wants to tell you arrives. Most people
spend more time here than anywhere else in the product.

## Categories {#categories}

The feed carries four kinds of event, and the filter along the top separates them:

- **AI Detection** — a rule fired. These carry a snapshot and detail.
- **Camera Health** — a camera went unreachable or came back.
- **Machine Health** — the host is short of CPU, memory or disk.
- **Login Security** — a sign-in lockout.

Mixing them in one feed is deliberate: "the camera stopped reporting" and "the camera saw
somebody" are both things you want to know, and separating them into two screens means one of them
goes unwatched.

The **Unread / All** filter is the other axis. Unread is the working view.

## Reading a detection {#reading}

An AI detection expands into the snapshot from the moment it fired, with the detected object boxed
and labelled, plus the rule that fired, the confidence, the camera and the timestamp.

**View clip** appears when a matching recording exists. When it does not, the entry says **No clip
recorded** — and that means one of two things:

- Recording was off for that camera when the event happened, or
- the footage has aged past its retention period and been purged.

Neither is an error. If you need clips for a camera, turn recording on for it; if events are
routinely older than your retention, raise the retention. See
[Recording configuration](recording-configuration).

## Acknowledging {#acknowledge}

Acknowledging marks an event as dealt with and takes it out of the unread count. Operators and
administrators can acknowledge; viewers cannot.

Treat it as a work queue rather than a formality. The value of acknowledgement is that the unread
count becomes a real measure of outstanding work — and the dashboard's
[noisiest cameras](dashboard#noise) panel becomes a real measure of which rules are wasting
people's attention.

## Diagnostic events {#diagnostic}

Some entries are tagged **diagnostic**. These are samples the system recorded to show you what the
detector is seeing — useful while tuning a rule, noise afterwards.

They can be purged separately from real detections, so clearing out tuning clutter never touches
your actual detection history.

## The alert log {#alert-log}

The feed is the recent, live view. The **Alert Log** on a camera's AI Detection page is the full
searchable history for that camera: filter by time, event, confidence or state, sort any column,
and page through it.

The log queries the database directly rather than filtering what is already on screen, so it stays
usable on a camera with a long history — which is exactly the camera you will be searching.

## When alerts are not arriving {#not-arriving}

Work down this list in order:

1. **Is there a rule?** Recording produces no alerts. Detection rules do. Check the camera's AI
   Detection page.
2. **Is the rule enabled, and is it in schedule?** A rule with a schedule is inactive outside it.
3. **Is the camera online?** Check the dot in the rail, or
   [Camera health](camera-health).
4. **Is the AI runtime ready?** Settings → AI. Without a model, nothing detects.
5. **Are they arriving in the feed but not on your phone?** Then the detection is fine and the
   delivery is not — see [Notification destinations](notification-destinations).

That order matters: each step is cheaper to check than the one after it, and step 5 is where most
people start and waste the most time.
