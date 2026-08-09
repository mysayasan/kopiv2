---
title: Settings, tab by tab
category: administration
categoryLabel: Administration
summary: What each of the nine Settings tabs is for, and which article covers it in depth.
order: 520
---

# Settings, tab by tab

Settings is administrators only. Nine tabs, each with a one-line description in the app; this page
says what each is *for* and where the detail lives.

## Runtime {#runtime}

*Decoder, streaming and detection engine tuning.*

The video plumbing: the ffmpeg path, RTSP transport (TCP is the reliable default), hardware
acceleration, probe sizes and buffering, and the WebRTC/MJPEG streaming configuration.

Come here when live view is stuttering, when the codec warning banner appears, or after installing
ffmpeg by hand. Most sites set this once during setup and never return.

Hardware acceleration is the one knob worth experimenting with on a busy machine — and worth
reverting quickly if streams start failing, because acceleration support varies enormously between
drivers.

## AI {#ai}

*Detection model, thresholds and frame sourcing.*

The AI runtime installer, the stock model choice, custom model import and activation, and the
licence-plate model.

Covered by [How detection works](how-detection-works) and
[Training custom models](training-models). The single most consequential setting on this tab is
the stock model size — see [the model table](how-detection-works#models).

## Notifications {#notifications}

*Delivery destinations, categories and alert fields.*

Destinations, per-destination filtering, payload fields, snapshots, and notification retention.
Covered in full by [Notification destinations](notification-destinations).

## Camera Health {#camera-health}

*Camera reachability monitoring and offline alerts.*

How often cameras are probed, the probe timeout, and how many consecutive failures count as
offline (and how many successes count as recovered). The tab tells you what those numbers add up
to in seconds, which is the number you actually care about.

Longer thresholds mean less noise on a flaky network and later news of a genuine failure. See
[Camera health](camera-health).

## Machine Health {#machine-health}

*Host CPU, memory and disk monitoring and safeguards.*

Thresholds for the host's own resources, plus the camera-capacity estimate and the **Run
calibration** benchmark. See [Storage and capacity](storage-and-capacity).

## Users {#users}

*Local accounts, roles and password management.*

Accounts, roles and the permission matrix — see [Users and roles](users-and-roles).

## Connectivity {#connectivity}

*Fleet pairing, discovery and node connectivity.*

Only relevant when this appliance is a node of a myseliasan fleet. See
[Connecting to a control plane](control-plane).

## Backup & Recovery {#backup}

*Back up and restore your configuration, export/verify the recovery key, and secure wipe and
reset.*

Three separate things share this tab, and confusing them is expensive:

- **Configuration backup** — your settings, portable to another machine.
  [Backup and restore](backup-and-restore).
- **Recovery key** — the encryption-at-rest key escrow. Without it, a dead machine's recordings
  are unreadable forever. [Encryption at rest](encryption-at-rest).
- **Secure wipe and factory reset** — destroy everything, deliberately.
  [Secure wipe and factory reset](secure-wipe-and-reset).

## Version & Health {#version}

*App version, runtime dependencies and health checks.*

The running version and update controls, the state of runtime dependencies (ffmpeg, the AI
runtime, OCR), the restart control, and liveness/readiness. See
[Updates, restart and health](updates-and-restart).

## Things that need a restart {#restart}

Most settings apply immediately. A few do not, and the app labels them *(restart to apply)* where
that is true — a freshly installed ffmpeg is the common one, and the notification purge interval is
another.

If a change does not seem to have taken effect, look for that label before assuming it failed.
