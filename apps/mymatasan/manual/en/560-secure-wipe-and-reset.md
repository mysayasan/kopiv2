---
title: Secure wipe and factory reset
category: administration
categoryLabel: Administration
summary: Destroy everything on this appliance, on purpose — what it does and what survives.
order: 560
---

# Secure wipe and factory reset

The most destructive thing in the product, in **Settings → Backup & Recovery**. Read this before
you use it, not during.

## What it does {#what}

In order:

1. **Shreds** all recordings, snapshots, training data and uploads.
2. **Destroys the encryption key**, making any ciphertext that survives on the medium
   permanently unreadable.
3. **Drops and rebuilds the database** — cameras, rules, alerts, accounts, everything.
4. **Restarts** the appliance into first-run setup, as if newly installed.

Afterwards you are back at [the first-run wizard](setup-wizard), signing in with a fresh bootstrap
password.

## Why crypto-erase matters {#crypto-erase}

Overwriting files does not reliably destroy them on SSDs and NVMe drives. Wear levelling means the
controller may keep copies at physical addresses the operating system cannot even name, so
"overwrite three times" is a promise the hardware does not let software keep.

Destroying the key sidesteps that entirely. Every recording was encrypted; the key is gone;
whatever bytes remain are noise. That is why [encryption at rest](encryption-at-rest) is on by
default — it is what makes this operation actually mean what it says.

## Before you do it {#before}

> [!WARNING]
> This cannot be undone. There is no backup unless you already made one, and no recovery key
> unless you already exported it. Once the key is destroyed, nobody can read the footage again —
> not you, not anyone.

If there is any chance you will want any of it:

1. **Export a [configuration backup](backup-and-restore)** and store it off the machine.
2. **Copy off any footage you need.** Individual clips can be downloaded from the
   [Recordings page](recordings#exporting).
3. **Export the [recovery key](encryption-at-rest#export)** — only if you intend to keep the
   ciphertext readable somewhere else. If you are wiping to dispose of the machine, deliberately do
   *not* keep it.

## Doing it {#doing}

The confirmation is intentionally awkward. You type a confirmation phrase, and a countdown runs
before it starts, which you can cancel.

Once it begins, a full-screen progress overlay appears. **Leave the page open and do not power the
machine off.** The scrub of free space can take a while on a large disk, and the page reloads by
itself once the appliance is back.

While the reset runs, the appliance sheds requests with a clean "unavailable" response rather than
failing oddly — that is expected, not a fault.

## What survives {#survives}

- **`config.json`** — server ports, database engine, base configuration. The reset restores the
  application to factory state, not the machine's installation.
- **The installed binary.** The version does not change.
- **Nothing else.** Not cameras, rules, alerts, accounts, footage, keys or pairing.

A wiped node is also no longer paired to any control plane, and will need adopting again.

## When to use it {#when}

- **Decommissioning or disposing of the appliance.** This is the main case, and the one it is
  designed for.
- **Handing it to another team or customer.**
- **Rebuilding after a configuration so tangled that starting clean is genuinely faster.** Rare —
  and a [restore from backup](backup-and-restore) usually gets there with less risk.

## What it is not for {#not-for}

- **Freeing disk space.** Use *Purge expired*, or shorten retention. See
  [Recording configuration](recording-configuration#purging).
- **Fixing a bug.** Restart first, and check the runtime dependencies — see
  [Updates, restart and health](updates-and-restart#dependencies).
- **Removing one camera's footage.** *Purge now* on that camera does exactly that.

Reach for those first. A factory reset is a disposal tool that happens to also fix things.
