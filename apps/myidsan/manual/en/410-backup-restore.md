---
title: Backup and restore
category: operations
categoryLabel: Operations
summary: The one thing to set up before you need it, and what a restore really replaces.
order: 410
---

# Backup and restore

MyIDSan is the app where losing the database locks every person out of every app at once. It holds
every account and password hash, every role, every registered relying app, the SSO certificate
authority's private key, all the two-factor secrets, and the directory bind password.

A backup is a single `.idbackup` file, encrypted with a passphrase you choose. **System › Backup &
restore.**

## Take one now {#export}

Pick a passphrase of at least twelve characters and download the file. The passphrase is the
**only** protection on it and there is no way to recover the contents without it, so store the file
and the passphrase safely and separately.

Exporting requires you to re-prove your own credentials first — see
[step-up](sessions-and-step-up#stepup). Removing the entire identity store from a server is not an
action a stolen cookie should be able to take.

Both the export and the restore are written to [the audit log](audit-log).

## What travels, and what does not {#contents}

The file carries the things that make this server *this* server:

- roles and permissions,
- accounts, groups and password hashes,
- second factors — authenticator secrets, recovery codes and security keys,
- registered apps, their auth policy and their redirect URIs,
- the directory configuration and its group mappings,
- the SSO certificate authority, certificate **and** private key.

It deliberately leaves behind everything that belongs to the *host* rather than the server:
`config.json`, TLS certificates, the at-rest encryption key, the request and runtime logs, pending
password-reset requests (restoring stale ones would hand out temporary passwords nobody asked
for), and live sessions. **The audit trail is not included either** — it has its own archive; see
[the audit log](audit-log#retention).

> [!IMPORTANT]
> Because `config.json` is not in the file, a restored server is not a configured server. Keep a
> copy of the configuration too, or be ready to redo the sign-in policy, ports and storage
> settings by hand.

## How the secrets survive the move {#secrets}

This is the part that decides whether you have a backup or an inert file.

Two-factor secrets and the directory bind password are sealed on disk with **this host's** at-rest
key. Copying the sealed bytes into the archive would produce a backup that restores "successfully"
onto a fresh host and then fails every second-factor check — the worst possible outcome, because
nobody finds out until someone tries to sign in.

So they are **unsealed on the way into the archive** (which is itself encrypted with your
passphrase) and **re-sealed with the destination host's own key** on the way out. The at-rest key
itself is never in the file. Secrets travel inside the encrypted archive; machine identity does
not.

## Restoring {#restore}

**Open and inspect** first. The file's manifest says which version created it, when, and which
sections it holds — read that before committing to anything.

Then choose what happens to what is already here:

- **Replace what is here** clears the matching records first. This is the right choice when
  rebuilding a server that was lost.
- **Keep both** adds the backup's records alongside the existing ones. Use it to fold one server's
  registrations into another, and expect to reconcile duplicates by hand.

Restoring replaces the accounts and roles on this server **including the one you are signed in
with**. Everyone is signed out, you included, and has to sign in again with an account from the
backup. If some records refer to something that was not part of the restore, they are skipped and
listed rather than silently dropped.

Like the export, a restore asks you to re-prove your credentials first.

## Rebuilding a lost server {#rebuild}

1. Install MyIDSan on the new host and let it start. It creates its own bootstrap superadmin —
   ignore it; the restore is about to replace the account list.
2. On the first-run screen, choose **restore from a backup** rather than working through the
   setup. It is offered there for exactly this reason.
3. Sign in with an account from the backup.
4. Put `config.json` back, or redo the settings, and restart.
5. Check that a relying app can still sign somebody in. The SSO certificate authority travelled
   with the backup, so it should — and the moment to find out is now, not the next working day.

> [!NOTE]
> If the new host has a different address, the redirect URIs your relying apps are registered with
> still point at the old one. They are matched exactly; see
> [Connecting an app](connecting-an-app#form).

## Where to go next {#next}

- [Connecting an app](connecting-an-app) — re-checking a relying app after a rebuild.
- [The audit log](audit-log) — where the export and the restore are recorded.
