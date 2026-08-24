---
title: Live Views
category: daily-use
categoryLabel: Daily use
summary: Arrange the video wall, and use audio, talk-back and PTZ on a camera.
order: 120
---

# Live Views

Live Views is the video wall: your cameras tiled into a grid you arrange.

## Arranging the wall {#layout}

Pick a grid size, then drag tiles to reorder them. If you have more cameras than the grid holds,
the extra ones page rather than being dropped — the grid is a page size, not a cap.

The layout is remembered in this browser, per person. Two operators on two machines can each keep
the arrangement that suits their job.

## What the tile status means {#tile-status}

Each tile reports how it is being delivered, and the wording matters when you are diagnosing
something:

| Indicator | What it means |
|---|---|
| **Live** | Direct WebRTC video. The efficient path — the browser is decoding the camera's own stream. |
| **Connecting** / **Reconnecting…** | Negotiating, or recovering after a drop. Brief is normal. |
| **MJPEG** / **MJPEG fallback** | The appliance is converting frames to still images for the browser. Works everywhere, costs far more CPU per camera. |
| **WebRTC needs H264** | The camera is streaming a codec the browser cannot play directly, so playback fell back. Change the camera's stream to H.264 to get the efficient path back. |
| **Camera offline** | Unreachable right now. See [Camera health](camera-health). |
| **Live view disabled** | Switched off for this camera on its own page. |

A wall that is entirely MJPEG will load the machine far harder than the same wall on WebRTC. If
tiles are stuttering, check this column before you blame the hardware.

## Audio {#audio}

Tiles are muted by default — a control room with a dozen unmuted cameras is unusable. Unmute the
one you are actually listening to.

If a camera's audio codec is one browsers cannot play, the appliance transcodes it. That costs CPU
per listening tile, so it is another reason to unmute deliberately rather than by default.

## Talk-back {#talk-back}

Where a camera has a speaker, the microphone button talks through it. It needs permission from
your browser to use the microphone, and it needs the right password stored on the camera's
**Access** tab.

Which password depends on the camera, and this trips people up constantly:

- **Standard ONVIF cameras** use the stored camera credentials. Nothing extra to do.
- **TP-Link Tapo** cameras use your **TP-Link cloud account password** — the one you sign in to
  the Tapo app with — not the camera's stream password.
- **TP-Link VIGI** cameras use the camera admin password.

If talk-back is rejected, the camera's Access tab has a checklist for exactly this. The most
common cause is the Tapo case above; the second most common is a TP-Link account signed in with
Google or Apple, which has no password to use until you set one.

## Pan, tilt and zoom {#ptz}

On cameras that support it, PTZ controls appear on the tile. They are available to operators and
administrators, not to viewers.

Moving a camera changes what every rule on it sees. A drawn detection zone is a region of the
frame, not a region of the world — pan the camera and the zone now covers somewhere else. If you
move a camera permanently, revisit its rules.

## Adding and removing tiles {#tiles}

Add a camera to the wall from its own page or from the grid's own control; remove a tile with the
control on the tile. Removing a tile only changes your view — it does not stop recording, detection
or alerting, all of which run on the appliance regardless of whether anyone is watching.

## Saved walls {#walls}

An arrangement worth keeping is a **wall**: a name, a grid, and the cameras in the order you put
them in. Save one and it is available to everybody, on every machine — including the operator on
the next shift, which the old per-browser arrangement never was.

Use **Save wall** to update the wall you are looking at, or **Save as new** to keep the current
grid under a new name. One wall can be marked the default; that is the one a screen opens with.

> [!NOTE]
> A wall is shared, not personal. Changing one changes it for everyone who opens it. That is
> deliberate — a control room's walls are furniture, and an administrator has to be able to fix the
> wall everybody is looking at.

If a camera on a wall is deleted, the wall says so rather than quietly showing one tile fewer.
Save the wall again to forget the missing camera.

## Cycling and alert pop {#cycle}

Two numbers on the wall bar decide what the wall does while nobody is touching it:

- **Cycle (s)** advances through the pages every few seconds, so a station can watch more cameras
  than fit the grid. 0 leaves the wall still.
- **Pop alert (s)** brings a camera onto the visible page when it raises an alert, and holds it
  there for that many seconds. 0 never does.

While a popped camera is showing, cycling **pauses**. The point of a pop is that somebody looks at
it, and a wall that rotates away two seconds later has done the opposite of raising an alarm. The
popped tile is outlined so it is obvious which camera the wall jumped for.

Paging by hand restarts the dwell, so a page you turned to is not whipped away a moment later.

## A second monitor {#second-monitor}

**Second monitor** opens the selected wall in its own window with no navigation and nothing else
on screen. Drag it to the other display and put it fullscreen.

It is a real browser window on purpose: a panel inside the app cannot be dragged onto another
screen. It signs in the same way as any other window, and it shows the same wall — so changing the
wall changes both.
