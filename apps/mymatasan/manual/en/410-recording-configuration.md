---
title: Recording configuration
category: recording
categoryLabel: Recording & storage
summary: Turn recording on per camera and set segments, pre-roll, retention and storage.
order: 410
---

# Recording configuration

Recording is configured **per camera**, on that camera's **Recording** tab. There is no global
switch — a site normally wants continuous footage from the gate and nothing at all from the
meeting room.

## Turning it on {#enabling}

**Enable recording for this camera** starts continuous recording. Everything below only matters
once it is on.

Recording is independent of detection. A camera can record without any rules, and rules can fire
without recording — the alert simply has no clip attached. Most cameras want both.

## Segment length {#segments}

Footage is written in segments of a fixed number of minutes rather than one endless file.

Shorter segments recover better from a crash or a power cut — you lose at most the segment in
progress. Longer segments produce fewer files. The default is a reasonable balance; if your site
loses power often, shorten it.

## Pre-roll and post-roll {#rolls}

When a rule fires, an **event clip** is extracted around the trigger: this many seconds before it
and after it.

Pre-roll is the important one. The interesting part of an incident is almost always what happened
in the seconds *before* the detection — how they arrived, from where. A pre-roll of a few seconds
turns a clip of someone standing in a doorway into a clip of them walking up to it.

## Retention {#retention}

How many days footage from this camera is kept. Past that, it is purged automatically to make room.

Set it per camera, from consequence rather than from disk size:

- A gate or till where incidents surface weeks later wants a long retention.
- A corridor nobody has ever reviewed wants a short one.

The question to ask is *how long does it actually take somebody here to get round to looking?*
Anything shorter than that means the answer to "can we see it?" is routinely no. See
[Storage and capacity](storage-and-capacity).

## Storage path {#storage}

Where footage is written. Point it at the machine's data drive, not the system drive.

Changing it later does not move existing footage. Get it right early — the wizard asks precisely
so you do.

The tab warns when the chosen volume is nearly full, and it is worth believing: recording stops
when the disk does.

## Object metadata {#metadata}

**Record object metadata** logs what the camera sees as a searchable timeline, independent of
whether footage is kept. See [Searching what your cameras saw](object-search).

**Presence gap** and the **object sighting cooldown** control how sightings are grouped: an object
that reappears within the cooldown continues the same entry instead of starting a new one. The
default is five seconds, which keeps one person crossing a car park as one row rather than
hundreds.

## When the disk fills {#disk-full}

Two behaviours are available, and the choice is a policy decision, not a technical one:

- **Overwrite oldest footage** — recording continues, and the oldest material is discarded
  regardless of its retention setting. You always have recent footage.
- **Pause** — recording stops until space is freed. You keep everything you have, and you record
  nothing new.

Most sites want overwrite: a recorder that quietly stopped last Tuesday is worse than one holding
slightly less history. Choose deliberately, because the failure looks completely different in each
case.

## Purging {#purging}

**Purge expired** deletes only footage already past its retention — housekeeping, safe to run.

**Purge now** deletes *all* footage and AI snapshots for a camera regardless of retention. It runs
behind a cancellable countdown and cannot be undone. Administrators only.

## Encryption {#encryption}

Recordings, snapshots and training images are encrypted on disk by default. It is transparent —
nothing to do when recording or playing back — but it changes what a factory reset means and it
makes the recovery key something you must not lose. See
[Encryption at rest](encryption-at-rest).
