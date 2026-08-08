---
title: Restoring from a backup
category: getting-started
categoryLabel: Getting started
summary: Bring a new machine up with an existing install's cameras, rules and settings instead of configuring it again.
order: 40
---

# Restoring from a backup

A configuration backup is a single `.mmbackup` file holding an install's **settings** — its
cameras, its detection rules, its notification destinations, its runtime configuration. Restoring
one lets a fresh machine adopt all of that instead of being configured by hand a second time.

## What is and is not in the file {#contents}

A backup can carry any of four sections:

| Section | What it holds |
|---|---|
| **Cameras** | Camera and ONVIF entries including their stored credentials, plus per-camera recording configuration. |
| **AI detection** | Detection rules and the class registry they refer to. |
| **Notifications** | Delivery destinations, including Telegram, webhook and MQTT secrets. |
| **App settings** | Decoder, vision and capture settings; camera-health and machine-health configuration. |

Two things are deliberately absent.

**Your footage.** A backup is configuration, not an archive. Recordings, snapshots and the alert
history stay on the machine that made them.

**This machine's identity.** The encryption-at-rest key, fleet pairing and enrolment, mTLS
certificates, `config.json` and the setup-complete flag are never exported. That is on purpose: it
means a backup cannot be used to clone one appliance's identity onto another one.

> [!WARNING]
> The file contains plaintext credentials that the normal API never hands out — camera passwords,
> bot tokens, broker secrets. It is encrypted with the passphrase you choose, and that passphrase
> is the only thing protecting it. Treat the file as a secret and pick the passphrase accordingly.

Because the encryption is passphrase-based rather than tied to the machine that made it, the file
opens on any host. That is what makes it useful for migration and what makes it dangerous if it
leaks.

## Restoring during first-run setup {#during-setup}

On the wizard's Welcome step, choose **Restore from backup**, pick the file, and enter its
passphrase.

This path applies the backup directly, replacing whatever the fresh install had. There is no
preview — on a brand-new machine there is nothing to weigh it against. Then:

1. The restore reports success.
2. **Restart to apply.** This is not optional. The recording, detection and notification services
   were started against the old, empty configuration and will not pick up the restored one until
   they are restarted. The page reloads by itself once the appliance is back.
3. The wizard does not reappear. A restored machine is treated as already configured.

Afterwards, check the things the backup could not carry: encryption at rest and its recovery key,
and fleet pairing if this node belongs to a control plane.

## Restoring into a running install {#into-running}

On an appliance that is already set up, restore from **Settings → Backup & Recovery** instead. That
route previews the file's contents before applying anything, and gives you two modes:

- **Replace** overwrites the sections in the file. Anything currently configured in those sections
  is gone.
- **Merge** appends the file's contents to what is already there.

Merge is the safe default when you are pulling one site's cameras into an appliance that already
watches others. Replace is what you want when you are rebuilding a machine to a known state.

In either mode, references between records are re-pointed as rows are inserted, so a rule still
attaches to the right camera even though the camera's internal id changed.

## If a restore fails {#troubleshooting}

Practically every failure is one of two things, and the appliance cannot tell you which:

- **The passphrase is wrong.** The file cannot be decrypted at all.
- **The file is not a backup**, or was truncated in transfer.

Re-download or re-copy the file and try the passphrase again. A backup whose passphrase is lost
cannot be opened — there is no recovery path, by design.
