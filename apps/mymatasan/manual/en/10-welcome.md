---
title: What MyMataSan is
category: getting-started
categoryLabel: Getting started
summary: The five ideas the whole product is built from — cameras, rules, alerts, recordings and roles.
order: 10
---

# What MyMataSan is

MyMataSan is a **network video recorder with an eye**. It connects to the cameras you already
own, watches what they see, records the footage, and tells you when something you cared about
actually happened.

It runs entirely on one machine on your own network. It does not need the internet to work: the
AI detection, the recording, the search and even this manual all live inside the appliance. If
your site has no outbound connection at all, nothing here stops working.

## The five ideas {#concepts}

Almost every screen in the product is one of these five things, so it is worth learning the words
once.

**Camera.** A video source on your network, usually reached over ONVIF or RTSP. Adding a camera
is mostly a matter of finding it and giving MyMataSan a username and password for it.

**Rule.** A standing instruction of the form *"on this camera, in this area, tell me when you see
this"*. A rule names one or more object classes (person, vehicle, fire, a licence plate, something
you taught it yourself), optionally limits itself to a drawn zone of the frame, and optionally only
applies at certain times.

**Alert.** What a rule produces when it fires. An alert carries a timestamp, the camera, what was
seen, a snapshot with the detection boxed and labelled, and — when recording was on — a link to
the footage around that moment. Alerts collect in the **Notifications** feed, and can additionally
be pushed out to a webhook, Telegram, an MQTT broker, or email.

**Recording.** The continuous footage a camera writes to disk, kept for a retention period you
choose and then automatically purged to make room. Recordings are what turn an alert from "I got
a message" into "I can prove what happened".

**Role.** What a signed-in person is allowed to do. There are three, and the difference between
them is deliberate rather than cosmetic — see [Roles and what they can do](#roles).

## Roles and what they can do {#roles}

| Role | Can | Cannot |
|---|---|---|
| **Viewer** | Watch live views. See that an alert fired. Change their own password. | Open recorded footage. Acknowledge alerts, move a camera, or talk through it. Anything in Settings. |
| **Operator** | Everything a viewer can, plus play back and download footage, search what a camera saw, acknowledge alerts, pan/tilt/zoom, and talk through a camera's speaker. | **Delete anything** — footage, alerts, cameras. Change rules, settings or cameras. Manage users. |
| **Administrator** | Everything. | — |

The line worth understanding is the one under **operator**: an operator can review an incident but
cannot destroy the evidence of it. That is what makes the recorder trustworthy as a record rather
than merely a convenience, and it is enforced by the server on every single request, not by hiding
buttons.

New accounts are created as operator unless an administrator picks otherwise.

## What the AI actually does {#detection}

MyMataSan runs a detection model over frames from your cameras and reports the objects it
recognises, each with a confidence score. A rule turns those raw detections into something worth
your attention by adding the context the model does not have: *which* camera, *where* in the
frame, *when*, and *how sure* it must be before you are told.

That division matters when you are tuning things later. If the system misses events, the model or
the confidence threshold is usually the problem. If the system finds events correctly but tells
you about the wrong ones, the rule is the problem.

## Where to go next {#next}

- Setting this up for the first time: [Signing in for the first time](first-sign-in).
- Already signed in and looking around: [A tour of the workspace](workspace-tour).
- Moving an existing install to this machine: [Restoring from a backup](restore-from-backup).
