---
title: Searching what your cameras saw
category: daily-use
categoryLabel: Daily use
summary: Search the object timeline across cameras and dates — including things no rule alerted on.
order: 150
---

# Searching what your cameras saw

Object search answers a question the alert log cannot: *what did this camera see, whether or not
any rule cared?*

Rules only tell you about what you thought to ask for in advance. Object search records everything
the detector recognised — people, vehicles, whatever your active models produce — as a searchable
timeline, so you can go looking after the fact.

This is the tool for "a van was here sometime on Tuesday afternoon and no rule was watching for
vans".

## Turning it on {#enabling}

Object recording is per camera, on the camera's **Recording** tab: **Record object metadata**.

It reuses the detector that is already running, so it costs almost nothing extra, and it works
whether or not video recording is on. When footage does exist, each result links straight to it.

> [!NOTE]
> Metadata is only recorded from the point you switch it on. It cannot be backfilled — there is
> nothing to reconstruct it from. Turn it on for the cameras that matter before you need it, not
> after.

## Searching {#searching}

Filter by date range, camera (or all cameras), object type (or any), and a minimum confidence.
Results are the sightings, newest first, with **Play footage** where a recording covers that
moment and **No footage** where none does.

Start broader than feels sensible. Any object, all cameras, a whole day — then narrow. The object
you are looking for is often classified as something adjacent to what you expected (a van as a
`truck`, a bicycle as a `motorcycle`), and a tight filter hides exactly the result you wanted.

## Confidence {#confidence}

Every sighting carries the detector's confidence. Raising the minimum removes uncertain sightings —
and removes real ones photographed badly.

When searching after an incident, set it low. A false result costs you two seconds to dismiss; a
missed one costs you the search.

## Sightings, not frames {#sightings}

An object that stays in view is one entry, not one per frame. An object that reappears within the
**sighting cooldown** (five seconds by default, configurable per camera) continues the same entry
rather than starting a new one.

That is why a person walking through a car park produces one row rather than four hundred — and
why briefly stepping behind a pillar does not split them into two people.

## What it is not {#limits}

- **It is not a rule.** Nothing alerts. It records so you can search later.
- **It is not better than the model.** It knows exactly what the active detection models
  recognise, and nothing else. A label that no active model produces will never appear — see
  [How detection works](how-detection-works).
- **It is not footage.** If recording was off, you get the sighting and no video. That is still
  useful — knowing a vehicle was at a gate at 14:32 narrows a search enormously — but it is not
  evidence on its own.
