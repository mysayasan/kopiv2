---
title: The dashboard
category: daily-use
categoryLabel: Daily use
summary: Read the summary screen — what the numbers mean, and which panels are worth acting on.
order: 110
---

# The dashboard

The dashboard answers one question: *is anything unusual going on?* Everything on it is a summary
over a time range you choose — **Today**, **7 days** or **30 days** — from the selector at the top.

## The four counters {#counters}

Across the top: **Total events**, **Unread**, **Critical** and **Warnings**, each with a comparison
against the previous period of the same length.

The comparison is the useful part. "412 events" means nothing on its own; "412 events, up 180% on
last week" means something changed. Look at the direction before you look at the number.

**Unread** is a work queue, not a fault. It counts events nobody has looked at yet. A number that
grows every day usually means a rule is firing more often than anyone can review — which is a
tuning problem, not a staffing one.

## Events over time {#events-over-time}

Notification volume across the range, split by category. Use it to spot *when* something changed.
A step that starts at a particular hour on a particular day almost always corresponds to something
physical: a light that came on, a gate left open, a camera nudged.

## By category, by severity, by source {#breakdowns}

Three breakdowns of the same events. **Category** separates AI detections from camera health,
machine health and login security — worth checking when the total jumps, because a spike that is
all camera-health events is a network problem, not a security event.

## Top cameras and top objects {#tops}

**Top cameras** ranks by event count. **Top detected objects** shows what the AI actually saw.

A camera at the top of this list is not necessarily the interesting one — it is the busiest one.
A camera pointed at a public pavement will win every time. Compare it against
[Noisiest cameras](#noise) before you conclude anything.

## Activity heatmap {#heatmap}

A typical week — weekday against hour, averaged over the last four weeks — optionally filtered to
one camera.

This is the panel that tells you what *normal* looks like at your site, which is what makes an
exception recognisable. Once you know the loading bay is busy 06:00–09:00 and dead after 19:00, a
Tuesday-at-midnight cluster stops being a number and starts being a question.

## Camera reliability {#reliability}

Uptime, total offline time and outage count per camera over the last seven days, with each
camera's current status.

Read the **outage count** before the uptime percentage. A camera at 99% uptime with one long
outage had a single incident; the same 99% spread over forty short outages is a camera with a
failing cable or a saturated network link, and it will keep dropping frames — and therefore
detections — between the outages the report can see.

## Noisiest cameras {#noise}

Cameras ranked by AI alert count, with the percentage nobody has read.

A high unread percentage is the signal here. It means the rule on that camera is producing alerts
people have learned to ignore, which is the failure mode that matters most: an operator who
ignores one camera's alerts is being trained to ignore all of them. Fix it by narrowing the rule —
a drawn zone, a higher confidence threshold, or a schedule — rather than by asking people to try
harder.

## Anomaly detection {#anomaly}

The one panel on this screen that is a control as well as a display.

It learns each camera's normal hourly activity over recent weeks and raises an alert when a camera
is unusually **busy** (a spike) or unusually **quiet**. The quiet case is the one people
underestimate: a camera that suddenly sees nothing at an hour it always sees something has often
been covered, turned, or unplugged.

- **Smart (baseline)** compares against what that camera has actually learned. This is the mode to
  use.
- **Manual limits** uses fixed events-per-hour numbers you set. Use it only where you know the
  right numbers better than the baseline does.
- **Sensitivity** — High, Medium or Low — controls how far from normal counts as unusual.
- **Scan last hour** previews what would have alerted, without changing anything. Use it after
  every change to sensitivity, rather than waiting to find out overnight.

## When the dashboard is empty {#empty}

A new install shows nothing until events exist. If it is still empty after cameras have been
running for a while, the usual cause is that no detection rules exist yet — recording alone
produces no events. See [Creating detection rules](detection-rules).
