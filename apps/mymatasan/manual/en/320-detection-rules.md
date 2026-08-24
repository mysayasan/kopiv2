---
title: Creating detection rules
category: detection
categoryLabel: Detection & AI
summary: Pick a mode, choose what to detect, draw zones and lines, set a schedule, and route the alerts.
order: 320
---

# Creating detection rules

A rule is a standing instruction on one camera: *in this mode, watching for these things, in this
area, during these hours, tell me — and tell these destinations.*

Rules live on a camera's **AI Detection** page. A camera can have as many as you need, and running
several narrow rules is usually better than one broad one.

## Modes — how it watches {#modes}

| Mode | Fires when |
|---|---|
| **Presence** | The object is anywhere in view (or in the zone). |
| **Crowd / count** | At least *N* people are in the zone in one frame. |
| **Intrusion (zone)** | The object enters a drawn area. |
| **Line crossing** | The object crosses a drawn line, optionally only one way. |
| **Multi-line crossing** | The object crosses several lines in order, within a time limit. |
| **Loitering (dwell)** | The object stays inside the zone for longer than you allow. |
| **Left behind** | The object stops moving and stays put, with nobody beside it. |
| **Direction of travel** | The object crosses the zone heading a particular way. |
| **Licence plate (LPR)** | A readable plate is seen — see [Fire, smoke and plates](fire-smoke-and-plates#lpr). |

Choose by the question you are actually asking. "Is anyone in the yard?" is presence. "Did anyone
come through the gate?" is line crossing — and it will not fire on the person who was already
standing there, which is usually what you wanted.

**Multi-line crossing** is the one worth knowing about: two lines crossed in order within a time
limit expresses direction of travel through a space, which filters out the loitering, the pacing
and the passer-by that a single line cannot.

## Detect — what it watches {#detect}

Pick the object classes. Leave it as **anything** and every detected object matches, which is a
fine way to see what a camera actually produces and a poor way to run a site.

Classes come from the registry — see [Object classes and groups](object-classes). A class marked
**model not active** cannot match anything: the model that produces its labels is not running.

## Zones {#zones}

Draw the area the rule cares about on the camera's preview. A rule can have **several zones**, and
they combine as "any of them".

This is the highest-value tuning in the product. A camera watching a gate almost always also
watches a public pavement, and a rule without a zone alerts on every pedestrian who walks past
your property. Drawing around the gate does not make the detector better — it makes the rule ask
the right question.

The preview shows what percentage of the frame a zone covers. Tools let you add and delete points,
centre the box, flip, rotate, snap to a grid, and undo.

Remember that a zone is a region of the *frame*, not of the world. Pan or re-mount the camera and
the zone now covers somewhere else entirely.

## Lines {#lines}

For crossing modes, draw a line and set its direction. A green arrow shows which way across
triggers, and the shaded side marks the trigger side. Click the arrow to cycle: one way → the
other → both ways.

Place lines where things must pass rather than where they might: a gateway, a doorway, the neck of
a corridor. A line across open ground catches the one route and misses the other three.

For multi-line, set the **maximum seconds between lines**. Too short and a slow walker never
completes the sequence; too long and two unrelated events an hour apart become one alert. Time
somebody actually walking it.

## Confidence, frames and cooldown {#tuning}

**Threshold**, **minimum frames** and **cooldown seconds** — all explained in
[How detection works](how-detection-works#confidence).

The order to tune in: get the zone right first, then minimum frames, then the threshold, then
cooldown. Most people reach for the threshold first, and it is the crudest of the four.

## Schedule {#schedule}

A rule can run **always**, or on a schedule: daytime, night-time, weekdays, weekends, a custom
weekly pattern, or a specific date range.

The **policy mode** is the part to read twice:

- **Detect only during this schedule** — the rule is active inside it and silent outside.
- **Pause during this schedule** — the rule is active *except* inside it.

The second is how you say "alert on the yard, but not while the morning shift is unloading". The
schedule has its own timezone setting, so a site in a different zone from the appliance behaves as
its operators expect.

## Notification routing {#routing}

By default a rule's alerts go to every configured destination. Select specific ones to route it —
"the loading bay rule goes to the warehouse MQTT topic, not to everyone's phone at 3am".

With no destinations configured at all, alerts still appear in the in-app feed; there is simply
nowhere else for them to go. See [Notification destinations](notification-destinations).

**Sound alert** plays a sound in the browser of anyone watching. Use it on the few rules that
warrant looking up immediately, and nowhere else — a sound that fires constantly is a sound people
mute.

## Enabling, disabling and reading the results {#lifecycle}

Rules can be disabled without deleting them, which is the right way to test a theory.

Once a rule is live, the **Alert Log** on the same page is where you judge it: filter by time,
event, confidence or state. A day of real alerts tells you more about a threshold than any amount
of reasoning.

## A workable first rule {#first-rule}

For most sites, on most cameras:

1. Mode **Intrusion (zone)**.
2. Detect **Person**.
3. A zone drawn around your property only — not the pavement, not the road.
4. Default threshold, minimum frames **2**, cooldown around **30** seconds.
5. Schedule **Always** to begin with.

Run it for a day, read the alert log, then narrow. That sequence beats getting it right in
advance, because you will not.

## Rules about time {#time-rules}

Three modes ask a question no single frame can answer, so they watch an object across many.

**Loitering (dwell)** fires when something of the chosen type has been inside the zone for longer
than you allow. Stepping out of the zone starts the clock again — being seen somewhere else is
information. A briefly missed frame does not: a confidence dip or somebody walking in front is
missing information, not an exit, and treating the two the same would mean a thirty-second rule
never fired on a real camera.

The alert records **when the dwell began**, not only when it fired. That matters when you go
looking: the alert arrives at 14:05 and the interesting thirty seconds start at 14:04:30.

**Left behind** fires when something of the chosen type stops moving and stays put. The
distinction is movement, not presence — a bag being carried through the frame is not a bag left
behind, and a rule that fired on presence would alert on every passenger. Two settings shape it:
how long it must be still, and how much movement still counts as still (a fraction of the frame,
to absorb the box jitter every detector produces).

By default it only fires when **nobody is standing beside it**. A bag with its owner next to it is
not abandoned. Turn that off only if you want to know about every object that stops.

**Direction of travel** fires when something crosses the zone heading a particular way — the
wrong-way rule. Directions are relative to the **picture**, not the compass: "up" means up the
image. It needs a minimum distance travelled before it will judge a heading, because a stationary
object jitters in a random direction and would otherwise eventually satisfy any rule by accident.

> [!NOTE]
> A time rule needs the object to be **tracked** across frames. Where two people cross paths in
> front of a camera, the tracker can swap them; the dwell then follows the wrong person. Draw the
> zone where that is least likely, and prefer a longer threshold over a short one.

### Choosing a threshold {#time-thresholds}

Start longer than feels right. A thirty-second loitering rule on a doorway fires on anybody
reading their phone; two minutes fires on somebody waiting for a person who is not coming. The
alert you act on is the one that is rare.
