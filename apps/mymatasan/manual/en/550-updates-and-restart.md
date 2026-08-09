---
title: Updates, restart and health checks
category: administration
categoryLabel: Administration
summary: Keep the appliance current, restart it safely, and read what the health endpoints say.
order: 550
---

# Updates, restart and health checks

**Settings → Version & Health** covers the running version, updating, restarting, runtime
dependencies and service health.

## The running version {#version}

The version, the shared-core version, the commit and the build date are shown here and in the page
footer.

Quote the full set when reporting a problem. "The latest one" is not a version, and the difference
between two builds of the same version number is exactly the information that identifies a
regression.

## Updating {#updating}

How you update depends on how the appliance was installed, and the tab tells you which case you
are in:

- **In-app update.** An update is offered and applied from here.
- **Managed by a package manager.** Update with your platform's tools —
  `sudo apt update && sudo apt install --only-upgrade mymatasan` or the `dnf` equivalent. In-app
  update is deliberately unavailable so the package manager stays the source of truth.
- **A container.** Pull the new image and recreate the container.

Before any update:

1. **Take a [configuration backup](backup-and-restore).**
2. **Confirm your [recovery key](encryption-at-rest#export) is exported and verifies.**
3. Update when somebody can watch it, not on a Friday evening.

An update restarts the appliance. Live view drops, recording stops for the restart, and any
in-flight detection is lost. On a busy site that is a minute of missing footage — schedule it
accordingly.

## Restarting {#restart}

**Restart app** restarts cleanly: recorders are stopped properly so segments are finalised rather
than truncated.

Restart when a setting is labelled *(restart to apply)* — a newly installed ffmpeg is the usual
case — and when the appliance is misbehaving in a way you cannot explain. It is safe, and it is a
legitimate first diagnostic.

Always restart from here rather than killing the process. An abrupt stop can leave the segment in
progress unfinalised.

## Runtime dependencies {#dependencies}

The tab reports whether the things the appliance leans on are present:

- **ffmpeg** — live view, recording and the AI capture path. Nothing works without it.
- **The AI runtime** — Python plus the detection libraries. Without it, no detection at all.
- **OCR dependencies** — needed for [licence plates](fire-smoke-and-plates#lpr) specifically.

Each can be installed from within the app. Check this tab first whenever a whole capability seems
absent rather than merely misconfigured — "detection has never worked on this install" is usually
a missing runtime, not a bad rule.

## Liveness and readiness {#health}

Two health signals, meaning different things:

- **Liveness** — the process is responding. A supervisor uses this to decide whether to restart the
  appliance.
- **Readiness** — additionally, the database and cache are reachable. A load balancer uses this to
  decide whether to send traffic.

The app's own monitors — machine health, camera health — appear as **advisory** readiness fields.
They never block readiness, because a camera being offline is not a reason to take the recorder out
of service. A degraded value is still worth investigating; it just is not an outage.

If you are integrating with external monitoring, watch liveness for "is it alive", readiness for
"can it serve", and the notification feed for "is anything wrong" — the third catches what the
first two are designed not to.
