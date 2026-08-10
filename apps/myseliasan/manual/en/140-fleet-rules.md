---
title: Fleet rules
category: fleet
categoryLabel: Fleet
summary: Correlate events across different nodes, and use absence to express innocence.
order: 140
---

# Fleet rules

A fleet rule watches events across **different nodes at once** and fires only when they line up.

No single node can do this. A camera node cannot see your door contacts; a sensor node cannot see
your cameras. Only the control plane, which already receives every node's events in one feed, is in
a position to notice the conjunction.

## Why correlation beats any single sensor {#why}

On its own, a camera's motion alert at 03:00 is noise — a moth, a spider, headlights through a
window. On its own, a door contact at 03:00 is noise — cleaners, deliveries, wind.

The two together, **with no badge swipe**, is not noise.

That is the whole idea: correlation is how a fleet of individually noisy sensors becomes one
trustworthy signal. A rule you can actually leave switched on is worth more than five you mute.

## Conditions: what must happen, and what must not {#conditions}

A rule is a list of things that must have happened — and, crucially, things that must **not** have.

Each condition matches on:

| Field | What it does |
|---|---|
| **Node type** | Camera node, sensor node, door controller, or any. |
| **Node** | One specific node, or any. |
| **Category** | The event's category, e.g. `vision.alert`. |
| **Text to match** | Matched case-insensitively against the event's title and body. |

The text is matched against **the name of the rule that fired on the node** — "Person detected",
"Front door opened". Your nodes' own rule names are your vocabulary, so you do not have to invent a
taxonomy before you can write a rule.

A rule needs at least one thing that must happen. A rule made only of absences fires on nothing.

## Absence is how a rule expresses innocence {#absence}

This is the half of the feature that earns its keep, and the half that is easy to skip.

Without absences, "the door opened at 03:00" is an alert every night a cleaner works late. With
them, the rule says what it actually means: *the door opened, a camera saw motion, and nobody
badged in.*

A matching "must NOT have happened" event **disarms** the rule instead of firing it.

## Window, grace and cooldown {#timing}

Three numbers, each answering a different question.

**Window (seconds)** — how close in time the required events must be to count as one incident. It
is the difference between "a door opened, and separately a camera saw motion last Tuesday" and a
single event.

**Grace delay (seconds)** — how long to wait before believing an absence. When every required event
has arrived the rule does not fire; it **arms**, waits out the grace period, and only then decides
whether the absence really held.

This wait is not a nicety. A badge reader is routinely a second or two *behind* the door contact it
just authorised, so a swipe landing inside the grace period disarms the rule — that was an
authorised entry. Without the wait, the rule would cry intrusion on every legitimate badge entry,
all day, until somebody turned it off. Left at 0, the rule waits 5 seconds.

**Cooldown (seconds)** — how long the rule stays quiet after firing, so one incident is one alert
rather than a hundred. It survives a restart.

## Who can change them {#permissions}

Fleet rules are **written by superadmins only**. Other roles that can reach the page read them but
cannot change them — the rules decide what the whole fleet treats as an incident, so they are not
routine day-to-day editing.

Every change is recorded in the audit log.

## When a rule never fires {#troubleshooting}

In order of likelihood:

1. **The text does not match.** It is matched against the node's own rule name. Check the exact
   wording in the notification feed rather than typing what you expect it to say.
2. **The window is too short** for events that genuinely arrive a few seconds apart.
3. **An absence is disarming it.** If a badge reader reports later than you think, the swipe is
   landing inside the grace delay — which is correct behaviour, and the rule is telling you the
   entry was authorised.
4. **The node type is wrong.** Motion belongs to a camera node; a door contact or badge reader to a
   sensor node. They are not interchangeable.
5. **The rule is off**, or still inside its cooldown from a previous firing.

## When a rule fires too often {#noise}

Add an absence before you raise a threshold. "Motion at the loading bay" fires all day; "motion at
the loading bay with no badge swipe and no scheduled delivery" fires when something is wrong.

Raising the cooldown collapses a storm into one alert, but it does not make a wrong rule right.
