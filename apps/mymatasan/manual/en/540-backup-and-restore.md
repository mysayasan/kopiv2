---
title: Backing up your configuration
category: administration
categoryLabel: Administration
summary: Export cameras, rules, destinations and settings to a portable file — and put them back.
order: 540
---

# Backing up your configuration

A configuration backup is a single passphrase-protected `.mmbackup` file holding your **settings**,
so a new machine can be brought up without configuring it again.

Create and restore it in **Settings → Backup & Recovery**. The first-run wizard offers the restore
side too — see [Restoring from a backup](restore-from-backup).

## Choosing what to include {#sections}

Four sections, independently selectable:

| Section | Contents |
|---|---|
| **Cameras** | Camera and ONVIF entries including saved credentials, and per-camera recording configuration. |
| **AI detection** | Detection rules and the object-class registry. |
| **Notifications** | Delivery destinations, including Telegram, webhook and MQTT secrets. |
| **App settings** | Decoder, vision and capture settings; camera-health and machine-health configuration. |

Take all four unless you have a reason not to. Partial backups are for moving one part of a
configuration between machines — lifting a tuned notification setup onto a second appliance, say.

## What is never included {#excluded}

**Your footage.** A backup is configuration, not an archive. Recordings, snapshots and alert
history stay where they were made.

**This machine's identity.** The encryption-at-rest key, fleet pairing and enrolment, mTLS
certificates, `config.json` and the setup-complete flag are never exported — so a backup cannot be
used to clone one appliance's identity onto another.

**Custom model weights.** Referenced, not embedded. Copy `.pt` files across separately.

**Local user accounts.** Recreate them on the new machine.

The practical consequence: a full rebuild needs *three* things — the configuration backup, the
[recovery key](encryption-at-rest#export), and the footage itself. Losing any one leaves a gap
nothing else fills.

## The passphrase {#passphrase}

The file carries plaintext secrets the normal API never emits: camera passwords, bot tokens, broker
credentials. It is always encrypted with the passphrase you set, and **that passphrase cannot be
recovered**.

Because the encryption is passphrase-based rather than tied to the machine that made it, the file
opens on any host. That is what makes it useful for migration and dangerous if it leaks. Treat the
file as a secret.

## Restoring {#restoring}

Load the file, enter its passphrase, and **preview** what it contains before applying anything.
Then choose:

- **Replace** — overwrites the sections in the file. Whatever is currently configured in those
  sections is gone.
- **Merge** — adds the file's contents to what is already there.

Merge is the safe choice when folding one site's cameras into an appliance already watching
others. Replace is what you want when rebuilding a machine to a known state.

In both modes, references between records are re-pointed as rows are inserted, so a rule still
attaches to the right camera even though internal ids changed.

## After a restore {#after}

1. **Restart.** Recording, detection and notification services picked up the old configuration at
   startup and will not adopt the restored one until they restart. The first-run path prompts for
   this explicitly.
2. **Check the credentials landed.** Camera passwords travel in the file; confirm cameras come
   online rather than assuming.
3. **Re-do what the backup could not carry** — encryption key, fleet pairing, user accounts,
   custom model files.

## When to make one {#when}

- After first-run setup, once the site is configured.
- After any significant change — new cameras, reworked rules, new destinations.
- Before an upgrade or hardware move.

Backups are small and cost nothing to keep. Keep several: a backup made after a misconfiguration
faithfully preserves the misconfiguration, and the only way back is an older one.

## If a restore fails {#troubleshooting}

Nearly always one of two things, and the appliance cannot tell which:

- **The passphrase is wrong** — the file cannot be decrypted at all.
- **The file is not a backup**, or was truncated in transfer.

Re-copy the file and try the passphrase again. A backup whose passphrase is lost cannot be opened
by anyone, by design.
