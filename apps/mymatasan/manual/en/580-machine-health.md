---
title: Watching the machine itself
category: administration
categoryLabel: Administration
summary: CPU, memory and disk monitoring, what the safeguards do, and what to do when it complains.
order: 580
---

# Watching the machine itself

Cameras are monitored; so is the host they run on. **Settings → Machine Health** sets the
thresholds, and breaches arrive as **Machine Health** notifications in the feed.

## What is watched {#watched}

- **CPU** — sustained load. The appliance is CPU-bound in normal operation, so this is the first
  thing to move when you add cameras or a bigger model.
- **Memory** — headroom. Detection models are resident; several active models multiply it.
- **Disk** — free space on the **recordings** volume specifically, not every mount. That is the one
  with a hard consequence.

## Why the disk warning is different {#disk}

CPU and memory pressure degrade things. A full disk **stops recording**.

Whether it stops or overwrites is your choice — see
[When the disk fills](recording-configuration#disk-full) — but either way the disk warning is the
one to act on the same day. Mitigation is scoped to the recordings volume, which is precisely why
the storage path should not be on the system drive: a full system drive takes the whole appliance
down, not just recording.

## Reading a sustained CPU warning {#cpu}

Almost never "buy a bigger machine". Work through
[what actually consumes capacity](storage-and-capacity#drivers) first — in practice:

1. Is **detection** pointed at a main stream instead of a sub-stream? This is the usual answer.
2. How many **models** are active? Each one inferences every frame.
3. Is the **stock model** larger than this hardware wants?
4. Are live-view tiles falling back to **MJPEG** instead of WebRTC?
5. Is **face recognition** on cameras that do not need it?

Four of those five are settings. Fixing them typically recovers more headroom than any hardware
you could buy in the same afternoon.

## Setting thresholds {#thresholds}

Set them so that a notification means *do something*.

A threshold that fires every afternoon during the busy period trains everyone to ignore Machine
Health notifications — and then the disk one, which mattered, is ignored too. If a value is
routinely breached and the appliance is coping, the threshold is wrong, not the machine.

## Capacity, before and after {#capacity}

The same tab carries the camera-capacity estimate and **Run calibration**. Calibrate:

- before adding cameras, so the number reflects this hardware;
- after changing the stock model or activating another one, because the calculation changed;
- after a hardware change.

See [Storage and capacity](storage-and-capacity#estimate).

## What it does not watch {#limits}

- **Network throughput.** A saturated link shows up as flapping cameras and stuttering tiles, not
  as a machine-health alert. See [cameras that flap](camera-health#flapping).
- **Temperature and fans.** Use the host operating system's own monitoring; small appliances
  throttle silently when hot, and throttling looks exactly like "the CPU threshold keeps firing".
- **Other volumes.** Only the recordings volume is mitigated. If the system drive is filling for
  some other reason, your platform monitoring has to catch it.

## Where this fits with the rest {#related}

Three monitors, three questions, and it is worth keeping them straight:

| Monitor | Answers |
|---|---|
| [Camera health](camera-health) | Can the appliance reach the cameras? |
| **Machine health** | Can the machine keep up? |
| [Liveness and readiness](updates-and-restart#health) | Is the service alive and able to serve? |

A site that watches all three has almost no way to be quietly broken.
