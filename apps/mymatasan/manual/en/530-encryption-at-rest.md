---
title: Encryption at rest and the recovery key
category: administration
categoryLabel: Administration
summary: Recordings are encrypted on disk — export the recovery key, or a dead machine takes its footage with it.
order: 530
---

# Encryption at rest and the recovery key

Recordings, snapshots and training images are **encrypted on disk by default**. It is transparent:
nothing to do when recording, playing back or exporting.

Two consequences make this an administration topic rather than a footnote.

## Why it exists {#why}

A recorder holds footage of people who did not choose to be recorded. If the machine is stolen, or
a disk is replaced under warranty and leaves the site, encryption is what stops that footage being
readable by whoever ends up with it.

It also makes the factory reset **guaranteed**. Destroying the key — a *crypto-erase* — makes every
recording instantly unrecoverable regardless of size or storage medium, which plain overwriting
cannot promise on SSDs and NVMe drives, where the controller may have kept copies you cannot
address. See [Secure wipe and factory reset](secure-wipe-and-reset).

## Export the recovery key. Today. {#export}

The master key lives on this machine. Depending on how it is protected, it may be **bound to this
host** — protected by the platform keystore so it cannot be unwrapped on any other hardware. The
Backup & Recovery tab warns you when that is the case, and the warning is not decorative:

> [!WARNING]
> If the key is host-bound and the machine dies or is reimaged, **every recording becomes
> permanently unreadable.** Not difficult — impossible. Nobody can recover it, including the
> people who wrote this software.

**Settings → Backup & Recovery → Export recovery key** downloads a passphrase-encrypted copy of
the master key. The file never contains the raw key in the clear; only the passphrase opens it.

Do this on the day you commission the appliance, not the day you need it.

## Storing it {#storing}

- **The passphrase is the only thing protecting the file.** Choose it accordingly.
- **Store the file and the passphrase separately**, and both offline. A recovery key sitting next
  to its passphrase on the same appliance protects against nothing.
- A safe, a sealed envelope, or your organisation's existing key-escrow process are all fine. The
  drawer under the appliance is not.

## Verify it {#verify}

**Verify a recovery key** is a read-only check: it confirms a saved file opens with its passphrase
*and* that it protects the key currently in use. It restores nothing.

Run it now and after any key change. There are three outcomes, and the third is the one worth
catching:

- **Valid, and matches the key in use** — you are covered.
- **Wrong passphrase, or not a valid recovery key** — the file or the passphrase is not what you
  thought.
- **Passphrase correct, but it protects a *different* key** — an old export. You are holding a
  recovery key for a machine state that no longer exists, and it will not open today's
  recordings.

That third case is exactly how people discover, at the worst moment, that their escrow was stale.

## Restoring on new hardware {#restore}

The recorded procedure, which is also shown on the tab:

1. Install the app on the new machine.
2. Copy the recordings across.
3. Place the recovery file beside the key as `recovery.atrestkey` (or point `security.recoveryPath`
   at it).
4. Provide its passphrase via `security.passphraseFile` or the `ATREST_PASSPHRASE` environment
   variable.

On first start the key is restored automatically and the recordings decrypt.

Note what this is **not**: it is not a configuration restore. Cameras, rules and settings come from
a [configuration backup](backup-and-restore), which is a separate file with a separate passphrase.
A full rebuild needs both.

## The recovery screen at sign-in {#recovery-gate}

If the appliance starts, finds encryption on and a key that existed here before but cannot be read
now, it refuses to start normally and shows a recovery screen instead of sign-in.

That is the safety property working: starting normally would present an appliance that appears to
have no history. Upload the exported key and its passphrase to unlock. See
[Signing in for the first time](first-sign-in#recovery-gate).

## Turning it off {#disabling}

Encryption can be disabled in configuration. Existing plaintext files are always read
transparently, so the setting flips without migration.

Turn it off only where you have a specific reason — a machine in a controlled room whose disks
never leave, feeding a workflow that cannot cope. You lose the theft protection and the guaranteed
crypto-erase, and you gain a little CPU.

## What is not encrypted {#not-encrypted}

- **Model weights** (`.pt` files) — the detection worker reads them directly.
- **Exported clips** — a download is a normal video file.
- **Configuration backups** — encrypted, but with *your passphrase*, not this key. That is
  deliberate: it is why a backup opens on a different machine.
