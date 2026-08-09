---
title: Storage and capacity
category: recording
categoryLabel: Recording & storage
summary: How many cameras this machine can carry, and how retention trades against disk.
order: 420
---

# Storage and capacity

Two separate questions, and mixing them up is the usual source of a disappointing install:

- **How many cameras can this machine process?** Bounded by CPU, GPU and memory.
- **How long can it keep the footage?** Bounded by disk.

## The capacity estimate {#estimate}

**Settings → Machine Health** carries the camera-capacity estimate, and the setup wizard shows the
same figure.

It models AI detection, memory and recording as a continuous workload and reports the number of
cameras the host can carry plus the **limiting resource** — CPU, GPU, memory or disk. Live view is
deliberately not counted: it is on-demand, and nobody watches every camera at once.

It comes in three grades of confidence, and the difference is real:

| Grade | Where it comes from |
|---|---|
| **Ballpark estimate** | Detected hardware only. |
| **Measured from live load** | Extrapolated from what running cameras are actually costing. |
| **Calibrated on this host** | An actual detector benchmark on this machine. |

**Run calibration** before you buy cameras. It takes about a minute, is best run while the machine
is idle, and is worth far more than the spec-sheet guess — real throughput on real hardware
diverges from theory by a lot.

## What actually consumes capacity {#drivers}

In rough order of impact:

1. **The detection stream's resolution.** Pointing detection at a 4K main stream instead of a
   sub-stream can cost several times the CPU for no additional detections. This is the first thing
   to check on an overloaded machine. See [the Stream tab](camera-properties#stream).
2. **The number of active models.** Every active model inferences every frame. Two models is
   roughly twice the work — deactivate what you are not using.
3. **The stock model size.** Nano to extra-large spans a very wide range. On CPU or a Raspberry
   Pi, stay on nano or small.
4. **MJPEG live view.** A wall where tiles say "MJPEG fallback" instead of "Live" costs far more
   per camera. Fixing the camera's codec to H.264 gives that capacity back.
5. **Face recognition**, on each camera where it is enabled.

Notice that four of those five are configuration, not hardware. A machine that "cannot cope"
usually has one of these wrong.

## Disk, retention and cameras {#disk}

Disk does not limit how many cameras run — it limits how much history you keep. Three things
multiply:

```
storage  ≈  cameras  ×  bitrate  ×  retention days
```

Change any one and the others move. If the estimate says disk is your limiting resource, you have
four levers:

- **Add storage.** The honest fix.
- **Lower the recording bitrate or resolution.** Cheap, and costs evidential detail.
- **Shorten retention.** Cheap, and costs history.
- **Record fewer cameras continuously.** Some cameras genuinely only need event clips.

The capacity estimate treats recording as a rolling buffer rather than a hard wall: rather than
declaring a small disk unable to run cameras, it caps the count at roughly a one-day minimum
retention and tells you what retention you would actually achieve. That is the number to negotiate
with.

## Set retention from review time {#retention-policy}

The single most common storage mistake is buying disk for a retention nobody chose.

Ask instead: **how long does it take, at this site, for somebody to get round to reviewing an
incident?** If that is two weeks, a seven-day retention means the answer to "can we see it?" is
regularly no, and every pound spent on cameras bought nothing for those events.

Size the disk from that number. If you cannot afford it, shorten retention deliberately and tell
the people who will ask, rather than discovering it during an incident.

## Machine health {#machine-health}

**Settings → Machine Health** also monitors the host's CPU, memory and disk and raises Machine
Health notifications when they run short.

Take the disk warning seriously — it is the one with a hard consequence. Recording stops when the
disk stops, and the mitigation is scoped to the recordings volume, which is why pointing the
storage path at the system drive is a bad idea: a full system drive takes the whole appliance down
with it, not just recording.
