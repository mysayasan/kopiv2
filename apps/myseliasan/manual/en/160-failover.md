---
title: Failover
category: fleet
categoryLabel: Fleet
summary: Name a spare recorder for a site, prove it can actually reach the cameras, and hand them over when the recorder stops.
order: 160
---

# Failover

A recorder is the only thing recording its own cameras. When it stops — a dead power supply,
a failed disk, a switch port, somebody carrying it out of the building — those cameras stop
being recorded, and nothing anywhere starts recording them again.

A **failover plan** names a spare appliance for a recorder. While that recorder is healthy the
spare is given a copy of its camera list, so that when the recorder stops, the cameras it was
watching are being recorded again within minutes.

## The one thing to understand before you rely on it {#tested}

Copying is not readiness.

Copying proves the two appliances can talk to each other. It says nothing about whether the
spare can reach the **cameras** — a different network path, different credentials, and the
thing that actually fails. A spare on the wrong VLAN, a camera whose password was rotated on
the camera itself, a switch that never had a route to that site: none of those show up in a
copy, and all of them show up the moment somebody needs the footage.

So a plan that has been copied but never tested says **Never tested**, and the screen does not
soften it. Press **Test** and the spare tries to open every one of those cameras, exactly the
way recording will, and reports per camera whether it could. Only then does the plan say
**Tested and ready**.

A drill that comes back with three of forty cameras reachable is not a failure of the feature.
It is the feature: you now know, on a quiet afternoon, something you would otherwise have
learned during an incident.

A plan is also re-tested by itself once a day, because the things that break a spare — a VLAN
change, a rotated password, a machine moved to a different switch — break it while nothing is
happening.

## What it does not do {#limits}

**It restores recording, not the recordings.** Footage already on a failed appliance is still
only on that appliance. The only copies anywhere else are the clips the critical-clip archive
has already pulled off it. Failover means the cameras are being recorded again from the moment
it happens; it does not recover the past.

**A failed appliance is never stopped, even when it comes back.** The control plane cannot
tell a recorder that is dead from one it simply cannot see. Stopping the second kind would
mean stopping the only thing recording, on evidence that is incomplete by definition. So at
worst both appliances record the same camera for a while — a duplicate stream on the camera
and duplicate footage — until you hand the cameras back. Nothing recording is the one outcome
that cannot be undone, and no path here can produce it.

You are told when the recorder returns, so you can decide.

## Making a plan {#making}

Choose the recorder to cover and the spare that covers it. Both must be camera recorders. One
plan per recorder, and failover does not chain: a spare cannot itself be protected by another
plan, because a site's cameras ending up two appliances away from anyone who knows about them
helps nobody.

A spare can cover **several** recorders — that is what the "+1" in N+1 means. It needs the
capacity to run them all, which the test does not measure, and the network to reach their
cameras, which it does.

**Wait before acting** is how long the recorder must be out of contact before the plan does
anything. Long enough that a restart after an update does not trigger it; at least two
minutes, because a recorder is not declared offline any sooner than that, so a shorter number
would be a promise the system could not keep.

**Take the cameras over without asking** is off unless you turn it on. Left off, the plan tells
you and waits, and you press one button. Turned on, the spare starts recording by itself once
the wait has passed — which is right when the recorder is really dead and wrong when you
simply cannot see it. Turn it on after your own test has passed.

## When a recorder stops {#takeover}

You get a notification, and the plan's card says it is ready to fail over. Press **Take over**
(or let an armed plan do it) and the spare creates those cameras and starts recording them.

Until that moment nothing of the other site is visible on the spare: a staged camera is not a
camera. It does not appear in the spare's camera list, is not health-checked and is not
recorded. A spare covering four recorders does not show four sites' worth of cameras it is not
watching.

The takeover reports **per camera** what actually happened, read back from the recorder rather
than assumed. A camera that is recording says so. A camera whose stream could not be opened
says that instead, with the reason, rather than being counted as a success.

## Handing the cameras back {#failback}

When the recorder is healthy again, press **Hand back**. The spare stops recording those
cameras.

It does not delete them, and it does not delete the footage. Everything the spare recorded
during the outage stays on the spare and stays playable — including after you delete the plan
itself. That footage is the only record of the period the recorder was down, and nothing here
throws it away.

## Camera logins {#credentials}

Covering a recorder means the spare needs to log in to that recorder's cameras, so those
credentials have to move between the two appliances.

They travel in a sealed envelope. The spare produces a one-use key, the recorder seals its
camera list to that key and addresses it to that spare, and the control plane carries the
result without being able to open it. An envelope intercepted on the way cannot be opened by
any other appliance, and cannot be given to one.

Creating, testing and handing over plans is an administrator action, and it is recorded in the
audit log — including a takeover that happened automatically, which is the one nobody was
present for.
