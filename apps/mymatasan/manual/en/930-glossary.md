---
title: Glossary
category: appendix
categoryLabel: Appendix
summary: The words this product uses, and what each one means here.
order: 930
---

# Glossary

Terms as this product uses them. Some are used loosely elsewhere; these are the meanings that
apply on these screens.

**Acknowledge** — marking an alert as dealt with, removing it from the unread count. Operators and
administrators. See [Acknowledging](notifications#acknowledge).

**Alert** — what a rule produces when it fires: a timestamp, a camera, what was seen, a
snapshot, and a link to footage when there is any.

**Bitrate** — how much data a stream produces per second. The main driver of how much disk a
retention period costs.

**Class** (object class) — a name you define that maps onto one or more model labels.
*Vehicle* = `car`, `truck`, `bus`. What rules are written against. See
[Object classes](object-classes).

**Confidence** — how sure the model is about a detection, 0 to 1. A rule's
threshold is the minimum it accepts.

**Cooldown** — how long a rule stays quiet after firing, so one ongoing event produces one alert.

**Crypto-erase** — destroying the encryption key so the ciphertext can never be read, instead of
overwriting data. What makes a factory reset guaranteed on SSDs.

**Detection stream** — the camera stream the AI reads. Usually a sub-stream, and
independent of the recording stream. See [the Stream tab](camera-properties#stream).

**Diagnostic event** — a sample recorded to show what the detector sees, tagged so it can be
purged separately from real detections.

**Event clip** — footage extracted around a trigger, including pre-roll and
post-roll.

**Faceprint** — the mathematical representation of an enrolled person's face. Biometric data, with
legal obligations attached. See [People](people#consent).

**Factory reset** — shredding all data, destroying the key, rebuilding the database and restarting
into first-run setup. See [Secure wipe and factory reset](secure-wipe-and-reset).

**Fleet key** — the shared secret that makes a node discoverable by, and adoptable by, one
myseliasan control plane. See [Connecting to a control plane](control-plane#fleet-key).

**Label** — the exact word a model outputs for something it recognises: `person`, `car`,
`fire hydrant`. Matched exactly; grouped into classes.

**Line crossing** — a rule mode that fires when something crosses a drawn line, optionally only in
one direction.

**LPR** — licence plate recognition. Reads plate text, and where available vehicle type and colour.
See [Fire, smoke and plates](fire-smoke-and-plates#lpr).

**Main stream** — a camera's full-resolution stream. What you record.

**Minimum frames** — how many consecutive frames must contain the object before a rule fires. The
best control against flicker.

**MJPEG** — a fallback delivery mode where the appliance converts video into still images for the
browser. Works everywhere, costs far more CPU than WebRTC.

**Node** — an appliance managed by a myseliasan control plane.

**ONVIF** — the industry standard for discovering and managing IP cameras. Optional: RTSP-only
cameras work too.

**Operator** — the middle role: reviews footage, acknowledges, PTZ, talk-back. Cannot delete. See
[Users and roles](users-and-roles#roles).

**Pre-roll** — seconds of footage kept from *before* a trigger in an event clip. Usually the part
you actually need.

**PTZ** — pan, tilt and zoom.

**Retention** — how many days footage is kept before automatic purging. Set per camera. See
[Recording configuration](recording-configuration#retention).

**RTSP** — the protocol cameras stream video over.

**Rule** — a standing instruction on one camera: mode, classes, zone, schedule, thresholds and
routing. See [Creating detection rules](detection-rules).

**Segment** — a fixed-length chunk of continuous recording. Shorter segments lose less to a crash.

**Sighting** — one entry in the object timeline: an object seen, grouped across frames rather than
recorded per frame. See [Object search](object-search#sightings).

**Skill** — something a camera has been taught to recognise via
[Teach mode](teach-mode).

**Snapshot** — the still image captured at the moment a rule fired, with the detection boxed and
labelled.

**Stock model** — the always-on base detection model that knows general everyday classes. Runs
alongside any custom models. See [Models](how-detection-works#models).

**Sub-stream** — a camera's lower-resolution secondary stream. What live view and detection should
usually use.

**Threshold** — a rule's minimum acceptable confidence.

**Viewer** — the most restricted role: live video and the fact that alerts happened, nothing more.

**WebRTC** — the efficient live-view path, where the browser decodes the camera's own stream
directly. Shown as **Live** on a tile.

**Zone** — a drawn region of the frame a rule cares about. A region of the *frame*, not of the
world — moving the camera moves the zone. See [Zones](detection-rules#zones).
