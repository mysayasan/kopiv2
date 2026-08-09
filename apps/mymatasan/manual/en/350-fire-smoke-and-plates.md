---
title: Fire, smoke and licence plates
category: detection
categoryLabel: Detection & AI
summary: The two specialist detections — what each needs to work, and where each fails.
order: 350
---

# Fire, smoke and licence plates

Two detections behave differently enough from ordinary object detection to be worth their own
page.

## Fire and smoke {#fire}

Fire and smoke are ordinary object classes as far as rules are concerned — you detect them like
you detect a person. What is different is that the **stock model does not know them**. You need a
model trained for fire and smoke, imported and activated in **Settings → AI**.

You source that model yourself. When one is imported, its labels are detected automatically and
mapped onto the *Fire* and *Smoke* hazard categories, including the common naming variants
different models use, so you rarely have to wire anything up by hand.

### Making it useful, not annoying {#fire-tuning}

Fire and smoke detection has a characteristic false-positive profile, and it is worth planning for:

- **Sunsets, headlights, brake lights, welding, hi-vis clothing** read as fire.
- **Steam, exhaust, dust, fog and low cloud** read as smoke.

Which means:

- **Zone tightly.** Exclude sky, road and anything with a light on it.
- **Raise minimum frames.** Real fire persists; a passing headlight does not.
- **Use schedules where the false source is time-bound** — a west-facing camera at sunset is
  reliably a problem at a predictable hour.

> [!WARNING]
> This is not a fire alarm. It is a camera noticing something that looks like fire. It does not
> replace smoke detectors, sprinklers, or any life-safety system you are required to have, and it
> must never be the only thing standing between a fire and the people in the building.

## Licence plate recognition {#lpr}

LPR reads vehicle plates in a zone and, where available, reports the vehicle type and colour
alongside the plate text.

### What it needs {#lpr-requirements}

- **A plate model**, set in **Settings → AI → License Plate Model**.
- **Resolution.** Plates are unreadable on low-resolution streams. LPR automatically uses the
  camera's highest stream, and the mode is **hidden entirely** on a camera whose resolution is too
  low to be worth attempting — if you cannot find LPR on a camera, that is why.

Resolution at the plate is what matters, not the camera's headline megapixels. A 4K camera
covering a whole forecourt may put fewer pixels on a plate than a 1080p camera aimed down a single
lane. If plates matter, dedicate a camera to the lane.

### When to alert {#lpr-modes}

Three modes, and choosing the right one is most of the value:

| Mode | Fires on | Use for |
|---|---|---|
| **Any readable plate** | Every plate it can read | Logging traffic through a point |
| **Only plates on the watchlist** | Plates you listed | VIP or fleet arrival — "tell me when the director's car is here" |
| **Any plate NOT on the list** | Anything unlisted | An unknown vehicle at a staff gate — the security case |

The third is usually the one people actually want, and it is the one they configure last.

### The plate list {#lpr-list}

One plate per line, or comma-separated. Spaces and dashes are ignored, and matching tolerates a
single character error — because OCR confuses `0`/`O` and `8`/`B` for a living, and an exact match
would miss half the arrivals of a car that is definitely on the list.

That tolerance cuts both ways: on a large exclusion list, a plate one character away from a listed
one will be treated as listed. Keep watchlists as short as the job allows.

### OCR confidence {#lpr-confidence}

**Min OCR confidence** is separate from the detection threshold. Detection decides there is a
plate; OCR decides what it says.

Raise it and you get fewer, more trustworthy readings. Lower it and you get more readings,
including garbled ones — which matters more than usual in *exclusion* mode, where a misread plate
is an alert about a car that was on your list all along.

### Realistic expectations {#lpr-limits}

Angle, speed, weather, dirt, headlight glare and non-standard plates all cost accuracy. A
well-placed camera on a slow lane reads most plates most of the time; a camera at an angle across
a fast road does not, and no setting fixes the geometry.
