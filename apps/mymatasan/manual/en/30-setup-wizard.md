---
title: The first-run setup wizard
category: getting-started
categoryLabel: Getting started
summary: What each of the nine setup steps does, which ones you can safely skip, and what to fix later.
order: 30
---

# The first-run setup wizard

The wizard runs once, the first time an administrator signs in, and walks through nine steps:
Welcome, System, AI, Capacity, Cameras, Recording, Alerts, Connectivity, Done.

Nothing here is permanent. Every setting the wizard makes can be changed afterwards in Settings or
on the camera pages, and every step except the first can be skipped. Its purpose is to get you from
an empty appliance to a working camera with recording and an alert in one pass, rather than to
extract every decision from you up front.

The step strip along the top shows where you are, and **Skip setup** in the corner leaves the
wizard for good. If you close the browser part-way through, the wizard resumes where you left off.

## Welcome {#welcome}

Confirms who you are signed in as and that your password is set. If you arrived here without being
forced through a password change — because the password was supplied by configuration — this step
offers **Change password** so you can set your own anyway.

It also offers **Restore from backup**, which is a different path through the whole wizard: see
[Restoring from a backup](restore-from-backup). Take that route only if you are adopting an
existing install's configuration onto this machine.

## System {#system}

Checks the two things that are not MyMataSan's to provide.

**Video engine (ffmpeg).** Live view, recording and the frames the AI looks at all go through
ffmpeg. If it is not found, the step offers to download and install it for you. On a platform
where the automatic download is not available, install ffmpeg yourself and point Settings →
Runtime at it. Nothing downstream works without this, so it is the one step not to skip.

**Clock and timezone.** Every alert and every second of footage is stamped with the host's clock.
MyMataSan reports what the clock currently says but deliberately does not change it — a recorder
that quietly rewrites the system time is a recorder whose timestamps cannot be relied on. If it is
wrong, fix it in the operating system, then come back.

## AI {#ai}

The detector needs two things: a runtime and a model.

**AI runtime.** Python plus the detection libraries. **Install AI support** fetches them. This is
the largest download in setup and the one most likely to want a few minutes.

**Detection model.** Bigger models see more and run slower. The step suggests a sensible default
for the hardware it found. Pick a smaller one if the machine is modest or you plan to run many
cameras, and a larger one if you have a GPU and few cameras.

Skipping this step gives you a recorder without detection — cameras, live view and recording all
work, you simply get no alerts. You can come back in Settings → AI at any time.

## Capacity {#capacity}

Answers "how many cameras can this machine actually handle?" before you commit to a number.

**Quick estimate** reads the hardware and models it. **Run calibration** actually benchmarks the
detector on this host and is considerably more accurate — worth the minute it takes, and best run
while the machine is otherwise idle.

The result names the *limiting* resource: CPU, GPU, memory or disk. That is the useful part. If
disk is the limit, the number shown is a balance against retention rather than a hard ceiling —
footage rolls over, so a smaller disk means fewer days kept, not fewer cameras allowed.

## Cameras {#cameras}

**Scan network** looks for ONVIF cameras on the local network and lists what it finds.

For each camera you want, give it a name you would actually use over a radio — *Front Door*,
*Loading Bay* — and the camera's own username and password. Most cameras will not stream without
one. If the credentials are wrong, the add fails here rather than appearing to succeed and then
showing a black tile later.

A camera that the scan does not find is not lost; it can be added by address afterwards. Do not
spend time on it now.

## Recording {#recording}

Turns on continuous recording for the cameras you just added, with a **7-day retention** default.

Check the **storage folder** before you continue. The step shows how full that volume is and warns
you if it is nearly full — recording stops when the disk does. If the appliance has a large data
drive, point this at it now; moving footage later is more work than choosing correctly here.

Per-camera schedules, quality and retention are tuned later on each camera's Recording tab. This
step is deliberately one switch.

## Alerts {#alerts}

Adds a **person** rule to every camera — the single most useful rule on most sites — and,
optionally, somewhere to deliver the alerts.

Without a destination, alerts still happen; they appear in the in-app Notifications feed and
nowhere else. With one, they also reach you when nobody is watching the screen. The wizard offers
three:

- **Webhook** — a URL to POST to. The general-purpose option.
- **Telegram** — a bot token and a chat ID.
- **MQTT** — a broker URL and a topic.

Authentication, TLS client certificates, per-rule routing and message templating are all
configured later in Settings → Notifications. Here you only need the address.

## Connectivity {#connectivity}

Optional, and only relevant if this appliance is one node of a fleet managed from a MySeliaSan
control plane.

Paste the **fleet key** from your control plane and save it — the node becomes discoverable.
Then **generate a claim code** and enter that code in the control plane to adopt this node.

If you do not run a control plane, skip this. If you are not sure, skip it: a node can be paired
later from Settings → Connectivity without redoing anything.

## Done {#done}

Summarises what was set up. **Finish** closes the wizard, marks first-run setup complete, and
opens your dashboard with the cameras you added already tiled into a live view.

## What to do next {#next}

The wizard leaves you with a working system, not a finished one. The usual next steps are:

- Draw zones on the rules that matter, so a camera watching a public footpath does not alert on
  the footpath.
- Check each camera's stream profile if live view looks soft or stutters.
- Add the accounts your colleagues will use, at the role they should have — see
  [What MyMataSan is](welcome#roles) for what the three roles mean.
