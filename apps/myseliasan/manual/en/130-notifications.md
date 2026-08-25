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

## Being woken when nobody is watching {#push}

The feed is a screen, and the fleet's worst moments happen when nobody is at one.

**Alerts on this device**, at the top of this page, registers the browser you are reading this
in — a phone, a tablet, a desktop — so the control plane can raise a notification on it even
with the page closed. Add the page to your home screen and it behaves like an installed app.

Three separate things have to be true, and the panel reports them separately because they fail
separately:

- **This browser** must be able to receive notifications: a trusted HTTPS connection, and your
  permission when the browser asks for it. A browser that has blocked them says so, here.
- **This control plane** must be able to reach a push service. See below.
- **Each device** must have been reached. Every row shows what the last real attempt did, and
  when.

Each device has its own severity floor. That is deliberate: the phone in your pocket at three in
the morning and the laptop on your desk want different thresholds, and one setting for the whole
site forces the stricter one on everybody — or the looser one on the person who then silences
the app, which is the same as having no alerts at all.

Turning this on is an administrator's decision rather than each person's, because it makes this
machine open connections to a company outside the building. Once your role has been granted it,
the device is yours: only you, or an administrator, can remove it.

### It is measured, not assumed {#push-airgap}

Registering a device sends a real notification straight away, and the panel reports what
actually happened rather than that a setting is switched on. **"Never tested" is shown as its
own state, and it is not the same as working.**

If nothing could be reached, the panel says so plainly and names the hosts this machine would
have to talk to. **On a site with no internet access, that is the expected answer.** Web push is
delivered by whichever push service your browser uses — Google's, Mozilla's, Apple's — and on an
isolated network none of them is reachable. It is not a fault, no setting will change it, and
the feed, the email channel and this page all keep working exactly as before.

What those companies learn is that a message reached a device, and when. They cannot read it:
the contents are encrypted end to end between this control plane and your browser.

A notification is a nudge to come and look, never the record. Push may be delayed, dropped or
grouped by a service outside your control, and a device that has been reinstalled is quietly
removed the next time a message to it fails. The feed on this page is what actually happened.

## Retention {#retention}

The feed does not grow forever unless you let it.

Retention is a configuration setting, and when it is off the digest eventually raises a finding
saying so, with the row count. An unbounded table is a slow problem rather than a dramatic one,
which is exactly the kind that goes unnoticed until it is large.
