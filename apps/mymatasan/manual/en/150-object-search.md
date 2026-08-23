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

## Find similar {#appearance}

Any person or vehicle row carries a **Find similar** action. Pick a sighting and the search
ranks every other sighting the camera has recorded by how much it *looks like* the one you
picked — clothing colour, build, vehicle shape and colour.

It is a separate switch from object metadata, on the same **Recording** tab: **Describe
appearance for search**. It requires metadata recording to already be on — the description
rides on the sighting metadata creates — and it is not free the way metadata is: it is a
model pass over every person or vehicle in every sampled frame, on top of the detector
itself. Turn it on for the cameras where "where else did this go" is a question you expect
to ask.

> [!NOTE]
> Nothing is described until you switch this on, and nothing recorded before that moment
> can ever be found by it — the same limit as object metadata itself.

### What a ranked result means, and does not {#appearance-scoring}

A high-ranked result is not an identification. It comes from a general-purpose image
description, not a face or person re-identification model, so it is good at telling apart
coarse appearance — a red jacket from a black one, a van from a hatchback — and considerably
weaker at recognising the same individual across a big change in pose, lighting or camera.
Every result is a shortlist for *you* to confirm by eye, never a verdict, and the screen
never claims otherwise.

There is also no match percentage, and that is deliberate rather than missing. Measured
against the real model, two photos of the *same* person score about 98%, and two photos of
two *different* people score about 95% — the raw number barely moves no matter who is being
compared, so showing it as a percentage would look like near-certainty on every single row.
What the screen shows instead is how far a result **stands out** from everything else that
was compared for that search: a result that clears the crowd by a wide margin is worth a
look, one that barely does is not, regardless of what the underlying number reads. With very
few sightings to compare against, standing out means little, and the screen says so rather
than manufacturing a rank from too little evidence.

## What it is not {#limits}

- **It is not a rule.** Nothing alerts. It records so you can search later.
- **It is not better than the model.** It knows exactly what the active detection models
  recognise, and nothing else. A label that no active model produces will never appear — see
  [How detection works](how-detection-works).
- **It is not footage.** If recording was off, you get the sighting and no video. That is still
  useful — knowing a vehicle was at a gate at 14:32 narrows a search enormously — but it is not
  evidence on its own.
