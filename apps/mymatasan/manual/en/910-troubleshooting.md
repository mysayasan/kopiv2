---
title: Troubleshooting
category: appendix
categoryLabel: Appendix
summary: The symptoms people actually hit, and the order to check things in.
order: 910
---

# Troubleshooting

Symptoms in the order they are usually reported. Each list is ordered so the cheapest check comes
first.

## I cannot sign in {#sign-in}

- **"You do not have permission for this action" right after signing in** — the account's role does
  not allow something the app needs at startup. Ask an administrator to check the role, or sign in
  with an administrator account to confirm the credentials themselves are fine.
- **A countdown is showing** — the address is locked out after repeated failures. Wait; nobody can
  shorten it. See [When sign-in is locked](first-sign-in#lockout).
- **It asks for a recovery key file** — encryption is on and the master key cannot be read. See
  [the recovery screen](encryption-at-rest#recovery-gate).
- **I have lost the administrator password** — it is reset at the console on the machine itself,
  not from the browser. See [Signing in for the first time](first-sign-in#first-password).

## A camera is offline {#camera-offline}

Full sequence in [Camera health](camera-health#troubleshooting). Short version: is it just this
one → can you open the camera's own web page from the appliance's network → have the credentials
changed → has its address changed → are its stream profiles still valid.

## Live view is black, stuttering or slow {#live-view}

1. **Read the tile's status.** "MJPEG fallback" means the browser cannot play the camera's codec
   directly, and the appliance is converting — expensive per tile. See
   [tile status](live-views#tile-status).
2. **"WebRTC needs H264"** — change the camera's stream to H.264 and the efficient path returns.
3. **Live view is pointed at the main stream.** Assign a sub-stream on the
   [Stream tab](camera-properties#stream).
4. **Black tile, camera online** — usually credentials that authenticate for management but not
   for streaming, or a stream profile that no longer exists. **Find Streams**, then **Test RTSP**.
5. **Everything stutters at once** — the machine, not the camera. See
   [Watching the machine itself](machine-health#cpu).

## I get no alerts {#no-alerts}

In order: is there a **rule**; is it **enabled** and in **schedule**; is the camera **online**; is
the **AI runtime and model** present; and only then, is **delivery** configured. Detail in
[Notifications](notifications#not-arriving).

The most common single answer is the first: recording was turned on, and no rule was ever created.
Recording produces footage, not alerts.

## I get far too many alerts {#too-many}

Almost always geometry rather than sensitivity:

1. **Draw a zone.** A camera watching a gate usually also watches a public pavement.
   See [zones](detection-rules#zones).
2. **Raise minimum frames** to 2 or 3. Kills flicker, shadows and insects.
3. **Then** raise the confidence threshold.
4. **Add a schedule**, or a *pause during* schedule for the busy period.
5. **Narrow the classes** — "anything" matches everything.

Check [noisiest cameras](dashboard#noise) to find which rule is actually responsible; it is rarely
the one people blame.

## Detection misses things {#misses}

- **Confidence threshold too high** for that camera's conditions.
- **The zone does not cover** where it actually happens. Zones are regions of the frame — if the
  camera was moved, the zone moved with it.
- **The class is not in the rule**, or its category has no matching labels, or its model is not
  active. A category marked **model not active** detects nothing.
- **The camera cannot see it.** Dark, backlit, wet, too far, out of focus. No model recovers detail
  that was never captured.

## "No clip recorded" on an alert {#no-clip}

Either recording was off for that camera at the time, or the footage has passed its retention and
been purged. Neither is a fault — see [reading a detection](notifications#reading).

## The disk is filling {#disk}

Use **Purge expired** first — it removes only footage already past retention. Then decide between
shortening retention, lowering bitrate, or adding storage. See
[Storage and capacity](storage-and-capacity#disk).

Never point the storage path at the system drive.

## Talk-back does not work {#talk-back}

Overwhelmingly the wrong password: TP-Link Tapo cameras want your **TP-Link cloud account**
password, not the camera's stream password. The camera's Access tab has a full checklist. See
[Talk-back](live-views#talk-back).

## Licence plates are not being read {#lpr}

- **The LPR mode is not offered on this camera** — its resolution is too low, and the option is
  hidden rather than allowed to disappoint. See [LPR requirements](fire-smoke-and-plates#lpr-requirements).
- **No plate model** is set in Settings → AI, or the **OCR dependencies** are missing — check
  Version & Health.
- **Readings are garbled** — geometry. Angle, speed and glare dominate; a camera dedicated to one
  slow lane reads plates that a wide forecourt camera never will.

## After an update something behaves differently {#after-update}

Check the version and the runtime dependencies on
[Version & Health](updates-and-restart#dependencies), then restart once. If a camera changed
behaviour, re-run **Find Streams** — firmware updates renumber profiles more often than you would
expect.

## Nothing here matches {#escalating}

Collect, before asking:

- The **exact version** — app, core and commit, from Version & Health.
- **What changed** immediately before it started.
- Whether it affects **one camera or all of them** — that single distinction eliminates most of the
  possibilities.
- The **notification feed** around the time it started; camera-health and machine-health entries
  frequently explain what looks like an application fault.
