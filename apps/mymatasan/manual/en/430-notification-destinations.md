---
title: Notification destinations
category: notifications
categoryLabel: Notifications
summary: Deliver alerts to a webhook, Telegram or MQTT — with per-destination filtering and payloads.
order: 430
---

# Notification destinations

Alerts always appear in the in-app feed. A **destination** is somewhere else they also go, so
somebody hears about them without watching a screen.

Configure them in **Settings → Notifications**.

## The three channels {#channels}

**Webhook** — an HTTP POST to a URL you provide. The general-purpose option: anything that can
receive JSON can consume it.

**Telegram** — a bot token and a chat id. The quickest way to get alerts onto phones with no
infrastructure of your own. Snapshots arrive as photos.

**MQTT** — a broker URL and topic, with client id, QoS (0, 1 or 2), retain, and full TLS including
CA and client certificates. This is the one for integrating with building automation or an
existing operations bus.

## Each destination is independent {#independent}

This is the part that makes the feature useful, and it is easy to miss. Every destination has its
own:

- **Minimum severity** — a floor. Set an escalation route to Critical only and it stays quiet
  through the ordinary day.
- **Which notification types it receives** — AI detections, camera health, machine health, login
  security. Nothing checked means all of them.
- **Which detection fields it includes.**
- **Custom fields.**
- **Enabled/disabled**, without deleting it.

So a phone gets Critical only, an operations MQTT topic gets everything, and a maintenance webhook
gets camera and machine health and no detections at all. The in-app feed always shows everything
in full regardless.

## What is in a payload {#payload}

For AI alerts you choose which detection fields to include: rule, camera, object, confidence,
timestamp, zone, and so on.

**Licence-plate alerts automatically include** the plate number and, when detected, the vehicle
type and colour — in both the message text and the payload (`plate`, `vehicleType`, `color`). There
is no toggle; you do not have to remember to enable it.

## Snapshots {#snapshots}

Two delivery modes:

- **Inline** embeds the image — base64 in webhook and MQTT payloads, a photo in Telegram. The
  recipient sees the picture with no further work.
- **Link only** sends a reference and lets the consumer fetch the image. Much smaller payloads.

Use inline for people (a message with a picture is worth ten without), and link-only for machines
and for MQTT brokers where large retained payloads are a problem.

## Custom fields and placeholders {#custom-fields}

Custom key/value fields are added to the payload, and values can contain placeholders that resolve
at send time — camera name, rule name, timestamp and so on. The editor lists the available tokens
and copies them on click.

This is how you make a payload fit a system that already exists rather than rewriting the system
to fit the payload: add `site: "north-depot"`, or shape a field into the exact key your automation
already reads. A custom field with the same key as a built-in one overrides it, and the editor
says so. A token that resolves to nothing is left out rather than sent empty.

## Per-rule routing {#routing}

By default every rule's alerts go to every destination. On a rule, select specific destinations to
narrow it — see [Creating detection rules](detection-rules#routing).

Between per-destination filters and per-rule routing you can express most of what a site actually
needs: the after-hours perimeter rule wakes somebody up, the daytime loading-bay rule does not.

## Testing {#testing}

**Send Test** delivers a System notification to the destinations subscribed to that type.

Test when you set it up, and test again after changing anything about the receiving side. A
delivery route that has silently broken looks exactly like a quiet night.

Note the interaction: a destination whose minimum severity is above the test notification's, or
which does not receive System notifications, will not get the test. That is not a failure — it is
your filter working.

## When alerts are not being delivered {#troubleshooting}

First establish which half is broken: **is the alert in the in-app feed?**

- **Not in the feed** — nothing was detected. This is a detection problem, not a delivery one; see
  [Notifications](notifications#not-arriving).
- **In the feed but not delivered** — then work through:
  1. Is the destination **enabled**?
  2. Does its **minimum severity** admit this alert?
  3. Does it **receive this type** of notification?
  4. Does the **rule** route to it?
  5. Does **Send Test** arrive? If yes, the transport is fine and the filtering is what is stopping
     it.

Delivery is retried with a backoff when a destination is temporarily unreachable, and MQTT waits
for its broker rather than discarding messages at startup — so a broker that comes up late does not
lose the first alerts.

## Notification retention {#retention}

Old notifications are purged on a schedule you set: keep-for days, purge interval, and optionally
only purging ones that have been read. Zero disables automatic purging.

Keeping read notifications forever is rarely useful; the footage and the alert log are the record.
Interval changes take effect after a restart.
