---
title: A camera's pages, tab by tab
category: cameras
categoryLabel: Cameras
summary: Reference for Details, Access, Stream, Recording and ONVIF — and picking the right stream for each job.
order: 220
---

# A camera's pages, tab by tab

Selecting a camera in the rail opens its own pages: a live preview, its AI Detection rules and
alert log, and a Settings area split into five tabs.

## Details {#details}

Name, description and the basic identity of the camera.

The description is worth filling in. It appears as a tooltip in the rail, and *"covers the gate
and the first ten metres of the drive"* saves the next person a walk.

The **Danger Zone** at the bottom removes the camera — see
[Removing a camera](adding-cameras#removing).

## Access {#access}

Everything to do with credentials.

**Camera username and password** are what the appliance uses to reach the camera. Change them here
when they change on the camera. A stored password is never shown back to you; the field says one
is saved.

**Change camera password** goes the other way: it changes the password *on the camera itself*
through ONVIF, and updates the stored copy to match.

**Camera Users** manages the camera's own ONVIF accounts — list them, add one, remove one, change
a password. Roles are the camera's own (Administrator, Operator, User), not this appliance's.

**Two-way audio** is where talk-back is configured, including the speaker password. Which password
is required depends on the make, and it is not always the streaming one — see
[Talk-back](live-views#talk-back).

## Stream {#stream}

The tab that decides picture quality and how hard the machine works.

**Find Streams** asks the camera what it offers. A typical camera has two or three profiles: a
high-resolution main stream and one or two smaller sub-streams. Each profile shows its RTSP URI
and tracks, and **Test RTSP** proves one actually plays before you commit to it.

You then assign profiles to four roles:

| Role | What it feeds | What to pick |
|---|---|---|
| **Live view** | The video wall | A sub-stream. You are watching a tile, not a cinema screen. |
| **Detection** | The AI detector | A sub-stream, usually. The detector does not need 4K to see a person, and a smaller frame is dramatically cheaper. |
| **Recording** | Footage on disk | The main stream. This is the one you will be squinting at later. |
| **Fallback** | Used when the preferred stream fails | Anything that reliably works. |

Getting this wrong is the single most common cause of "the machine cannot keep up". Pointing
detection at a 4K main stream can cost several times the CPU of a sub-stream for no gain in what
gets detected.

The exception is [licence plates](fire-smoke-and-plates#lpr), which genuinely need resolution and
will use the camera's highest stream automatically.

## Recording {#recording}

Per-camera recording: whether it records at all, segment length, pre-roll and post-roll for event
clips, retention in days, and the storage path. This is also where **Record object metadata** lives
— see [Searching what your cameras saw](object-search).

Covered in full in [Recording configuration](recording-configuration).

## ONVIF {#onvif}

Device management: what the camera reports about itself and what you can change on it — clock,
network, users, reboot and factory reset.

Covered in [Managing a camera over ONVIF](onvif-management).

## AI Detection {#ai-detection}

Not a settings tab but its own page on the camera: the detection rules for this camera and the
full alert log. See [Creating detection rules](detection-rules).

## Live preview and the detection zone {#preview}

The preview on the camera's page doubles as the canvas for drawing detection zones and crossing
lines.

One thing confuses everybody once: **the preview is a periodic snapshot, not live video.** It
looks choppy and pauses while you drag. Detection itself runs at full rate on the real stream — a
zone you draw has no effect on detection speed or accuracy, and the stuttering preview is telling
you nothing about the detector's performance.
