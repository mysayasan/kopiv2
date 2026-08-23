---
title: Finding and playing recordings
category: daily-use
categoryLabel: Daily use
summary: Find footage by camera and time, jump straight to an event, and export a clip.
order: 140
---

# Finding and playing recordings

The Recordings page is continuous NVR playback, with event clips picked out of it.

Viewers cannot open this page. Reviewing recorded footage is the line between viewer and operator
— see [What MyMataSan is](welcome#roles).

## Finding footage {#finding}

Pick a camera and a date. The timeline for that day shows what exists: **continuous** stretches
where recording ran, and **event clips** where a rule fired. Click anywhere on it to jump to that
time.

Gaps in the timeline are informative rather than broken. A gap means recording was not running —
the camera was offline, recording was off, or the appliance was down. It is worth knowing which,
and [Camera reliability](dashboard#reliability) usually answers it.

## Timeline playback {#timeline}

**Timeline**, next to Live Views in the side-nav, plays footage by the clock instead of file by
file: drag anywhere on the scrub bar and playback crosses segment boundaries on its own — no
finding and opening the next clip by hand. The shading on the bar is the same footage the coverage
strip above reports, so the two never disagree.

Watch up to **8 cameras** against the same moment, synchronised, at anywhere from **0.25x** to
**8x** speed. If a rule fired during the window, its events are plotted on the bar as clickable
marks — click one to jump straight to it, the same shortcut [Jumping from an alert](#from-alert)
offers from the notification side. Marks need the Alerts page as well as Recordings; without it the
bar still scrubs, just without the marks.

Scrubbing to a moment with no footage does not silently guess: playback says so and snaps forward to
the next moment that has any, and tells you how much was skipped to get there.

## Jumping from an alert {#from-alert}

The far more common route is the other direction: you have an alert and want the footage. Use
**View clip** on the notification, which lands on the moment the rule fired rather than making you
hunt for it.

An event clip includes a **pre-roll** and **post-roll** — a configurable number of seconds before
and after the trigger — because what happened just before the detection is usually the part you
need.

## Playing {#playing}

Playback works like any video player. Where the footage is in a codec the browser cannot play
directly, the appliance converts it on the way out; that costs CPU while you are watching but
means playback works without installing anything.

## Exporting {#exporting}

Download the clip you are viewing. That gives you a file you can hand to somebody who has no
access to the appliance — which is the point.

Two things to be aware of before you rely on it:

- The download is the footage as recorded, with no watermark or signature. Treat chain of custody
  as a procedural matter at your site, not something the file proves by itself.
- Recordings are encrypted on disk. The exported file is **not** — it is a normal video. Handle it
  accordingly.

## Retention, and why footage disappears {#retention}

Every camera has a retention period, and footage past it is purged automatically to make room for
new recording. That is not a fault; it is what keeps a finite disk from filling.

The practical consequence: **an incident nobody looks at within the retention period is gone.**
If your site's review cycle is weekly, a seven-day retention is already too short. Set retention
from how long it actually takes someone to get round to looking, not from how much disk you happen
to have. See [Storage and capacity](storage-and-capacity).

## Purging {#purging}

Administrators have two purge controls, and they are very different:

- **Purge expired** deletes only footage already past its retention. In-retention footage is kept.
  This is housekeeping, and it is safe.
- **Purge now** deletes **all** recordings and AI snapshots for a camera regardless of retention.
  It cannot be undone, and it runs behind a countdown you can cancel.

Operators have neither. An operator who was present at an incident cannot delete the footage of
it, and that is deliberate.
