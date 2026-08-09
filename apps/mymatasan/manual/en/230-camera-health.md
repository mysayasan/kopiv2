---
title: Camera health and offline cameras
category: cameras
categoryLabel: Cameras
summary: How reachability is monitored, and how to work through a camera that has gone offline.
order: 230
---

# Camera health and offline cameras

## How health is monitored {#monitoring}

The appliance probes each camera on a schedule and records whether it answered. The result is the
coloured dot beside every camera in the rail, the status on its page, and the
[Camera reliability](dashboard#reliability) panel on the dashboard.

Going offline raises a **Camera Health** notification, and coming back raises another. Both matter:
a camera that drops and recovers every night at the same time is telling you something a
once-a-week glance never would.

Polling interval, how many consecutive failures count as offline, and whether offline raises an
alert are configurable in **Settings → Camera Health**. Making the appliance more patient reduces
notification noise on a flaky network — at the cost of finding out later that a camera is genuinely
gone.

## What offline actually means {#meaning}

Offline means *this appliance could not reach this camera*. It does not mean the camera is broken,
and it does not always mean it stopped recording.

Three distinct situations produce the same dot:

- The camera is off, crashed or unplugged.
- The camera is fine but the network path between it and the appliance is not.
- The camera is fine and reachable but refuses the credentials.

The third is worth separating early, because it looks identical from the rail and has a completely
different fix.

## Working through an offline camera {#troubleshooting}

In order — each step is cheaper than the next.

**1. Is it just this one?** If every camera on one switch went offline together, stop looking at
cameras.

**2. Can the appliance reach it at all?** From the appliance's own network, open the camera's web
interface. If that fails, it is a network or power problem and nothing in this product will fix it.

**3. Does it answer, but refuse?** If the camera's own interface works but the appliance still says
offline, suspect credentials. Somebody rotating camera passwords is the most common cause of a
healthy camera reading as offline. Fix them on the camera's [Access tab](camera-properties#access).

**4. Did its address change?** A camera on DHCP can get a new address after a power cut, and the
appliance is still calling the old one. Either give it a reservation on your DHCP server, or set a
static address on the camera's [ONVIF tab](onvif-management#network).

**5. Is the stream profile still valid?** Firmware updates occasionally renumber or remove
profiles. **Find Streams** on the [Stream tab](camera-properties#stream) re-reads what the camera
actually offers now.

**6. Re-probe.** The camera list can be re-checked on demand rather than waiting for the next
scheduled poll.

## Cameras that flap {#flapping}

A camera that alternates between online and offline is more damaging than one that is cleanly
down, because it produces a stream of notifications people learn to ignore, and its recording is
full of gaps nobody notices.

Usual causes, in order: wireless links, PoE budget exceeded on a switch, a marginal cable, and a
camera whose CPU is saturated by too many simultaneous streams — which can be you, if live view,
detection and recording are all pulling separate high-resolution profiles. Consolidating onto a
sub-stream for live view and detection fixes more "flaky cameras" than replacing hardware does.

Check the **outage count** rather than the uptime percentage on
[Camera reliability](dashboard#reliability); forty short outages and one long one give the same
percentage and mean entirely different things.

## What happens to detection and recording {#consequences}

While a camera is offline there is nothing to record and nothing to detect. Its timeline shows a
gap, and no rule on it can fire.

This is the reason a camera health alert deserves the same attention as a detection alert. A
camera nobody noticed was offline for a week is a week with no footage from it — and the alert
that would have told you never fired, because there was nothing to fire on.
