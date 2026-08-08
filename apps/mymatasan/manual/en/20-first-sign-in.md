---
title: Signing in for the first time
category: getting-started
categoryLabel: Getting started
summary: Find the one-time administrator password, set your own, and what to do when you are locked out.
order: 20
---

# Signing in for the first time

## Finding the one-time password {#first-password}

The first time MyMataSan starts it creates a single administrator account and a **one-time
password generated for this install**. There is no shipped default password to look up, and no
two installs share one.

The username is `admin`. The password is put in two places, so you can find it whichever way you
are running the appliance:

- **On the console.** A banner is printed at startup with the address to open, the username and
  the password. In Docker this is in `docker logs`; on Linux it is in the service journal.
- **In a file.** `INITIAL_ADMIN_LOGIN.txt` is written into the data directory. Use this one if
  the console has scrolled away or the service runs with no visible window — which is the normal
  case on Windows.

> [!NOTE]
> If you set `localAuth` in `config.json`, or the `LOCAL_ADMIN_PASSWORD` environment variable,
> before the first start, that password is used instead and is **not** echoed anywhere. The
> banner points at your configuration rather than printing a secret you already hold.

Delete `INITIAL_ADMIN_LOGIN.txt` once you have signed in and set your own password.

## Setting your own password {#change-password}

The bootstrap account is flagged *must change password*, so the very first thing you see after
signing in is the change-password screen. This is not a suggestion you can dismiss: until the
password is changed, the account can do nothing but read its own session and change its own
password. Every other request is refused.

Enter the one-time password as **Current password**, then your new one twice. The minimum is
eight characters; a passphrase of a few unrelated words is both stronger and easier to type on a
guard-room keyboard than a short scramble.

Once it is accepted you are signed in properly and the first-run wizard starts — see
[The first-run setup wizard](setup-wizard).

If someone else should own this appliance, use **Sign in as a different user** on that screen
instead of setting a password you will hand over.

## Signing in day to day {#daily}

The sign-in screen takes a username and a password, and nothing else. Two controls sit around it:

- The **language switcher**, top corner. It applies immediately and is remembered on this
  browser, so a shared terminal can be left in whatever language the person using it reads.
- The **help link**, which opens this manual. It works before you sign in, which is exactly when
  you are most likely to need it.

## When sign-in is locked {#lockout}

After several failed attempts from the same address, that address is locked out for a period that
doubles with each further failure. The sign-in screen shows a countdown; there is nothing to do
but wait for it to reach zero. Out of the box this starts after **5 failed attempts within 5
minutes**, begins at a **1-minute** lockout and grows to at most **15 minutes**.

Two things are worth knowing:

- Only the interactive sign-in counts against the lockout. A browser tab replaying an old
  credential in the background is refused but does not consume your budget, so it cannot lock out
  a colleague who is typing the right password.
- A lockout raises a **Critical** notification. If lockouts appear in the feed that nobody can
  account for, treat it as someone probing the appliance, not as a nuisance.

An administrator cannot shorten someone else's lockout from the UI — the countdown is deliberately
not something an attacker could argue their way past. Wait it out.

## When the appliance asks for a recovery key {#recovery-gate}

If MyMataSan starts and finds that encryption at rest is switched on, that a master key existed on
this machine before, and that it cannot read that key now, it will not start normally. Instead of
the sign-in screen you get a **recovery** screen asking for your exported key file and its
passphrase.

This is a safety property, not a fault: the recordings and snapshots on disk are ciphertext, and
without the key they are unreadable. Starting normally would quietly present an appliance with no
history.

Upload the `.atrestkey` file you exported when encryption was set up, enter its passphrase, and
the appliance unlocks and restarts. If you do not have that file, the encrypted material cannot be
recovered by anyone, including you — which is the point of encrypting it.

## Where to go next {#next}

- [The first-run setup wizard](setup-wizard) — what the nine steps do.
- [A tour of the workspace](workspace-tour) — what you are looking at once you are in.
