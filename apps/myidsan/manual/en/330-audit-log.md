---
title: The audit log
category: security
categoryLabel: Security
summary: What is recorded, what the action names mean, and how long it is kept.
order: 330
---

# The audit log

The audit log is the append-only record of what happened on this server: every sign-in, every
refusal, every change to who may do what. It can be read and it can age out, but nothing in the
product edits or deletes a single entry.

It is **superadmin-only** for the same reason its API is. The trail names who did what from where,
and on an identity server it also reveals which usernames exist.

## Reading it {#reading}

**System › Audit log**, newest first. Filter by action, outcome (succeeded, denied, error), who,
and a date range; **Export CSV** applies the same filters, so what you export is what you were
looking at.

Every entry carries who, what, which target, from which address, with which browser, and when.
Entries with no actor are not a defect: some things genuinely happen without anybody signed in —
see [the recovery markers](first-sign-in#reset-admin).

## The action names {#actions}

The names are a closed vocabulary rather than free text, which is what makes them worth filtering
on. These are the ones worth knowing by sight:

```spec
title : Action names you will actually filter on
row login-failure `login.failure` : A credential was refused. A run of these against one account is a guessing attempt; a run across many accounts from one address is a spray.
row login-lockout `login.lockout` : An address hit the failure limit and was locked out.
row mfa-recovery `mfa.recovery_used` : Somebody spent a single-use recovery code. Recorded separately from an ordinary sign-in on purpose — it is the strongest signal that an authenticator has been lost or taken over.
row mfa-admin-reset `mfa.admin_reset` : An account's second factor was cleared by somebody other than its owner, or by a RESET_MFA marker on disk.
row stepup-failure `stepup.failure` : A session failed to re-prove its credentials. This is what somebody probing a stolen cookie produces.
row role-change `user.role_change` : Somebody's role was changed — the single most privilege-relevant edit on this server.
row sso-refused `sso.refused` : A relying app's sign-in was refused: an unregistered redirect URI, an unknown client, a bad secret, or a replayed code.
row backup-export `backup.export` : The entire identity store left this server in one file.
row audit-purge `audit.retention_purge` : The retention job trimmed the trail. Its presence is what distinguishes a trimmed log from one whose history simply starts there.
```

Alongside the name, an authentication entry records **how** the person got in — local, `ldap`,
Kerberos, OIDC, social, or a recovery code — so "how did they sign in?" is answerable without
cross-referencing the configuration as it was that day.

## Who was let into which app {#federation}

This is the part only an identity server can answer, and it is easy to overlook.

"This account was compromised — what did it reach?" is not a question a relying app can answer;
each one only ever saw a session appear. MyIDSan records `sso.authorize` and `sso.token_issue`
against the **app** each sign-in opened, so the trail says not just that somebody signed in but
what that sign-in was traded for.

The refusals (`sso.refused`) are arguably the more useful half. An unregistered redirect URI, an
unknown client and a replayed authorization code are what an attack on that flow looks like, and a
trail holding only successes cannot show one. When a relying app reports a sign-in failure its own
logs cannot explain, this is where the reason is. See
[Connecting an app](connecting-an-app#troubleshooting).

## How long it is kept {#retention}

By default, **forever**. Nothing is trimmed unless retention is switched on in `config.json`,
because unbounded growth costs disk while missing security history costs an investigation.

When it is switched on, rows past the age limit are **archived to a file first** and then removed
from the table, and the run itself is recorded. The floor is 30 days — a shorter value is raised
to it, with a warning at startup, because a trail shorter than that answers very little.

> [!IMPORTANT]
> The archive files hold email addresses, source addresses and user agents in the clear. They are
> deliberately not sealed with this host's at-rest key — an archive whose only key lives on the
> machine it was meant to outlive is not an archive — so protect the directory accordingly, and
> include it in whatever you back up.

The archive is separate from [Backup and restore](backup-restore), which does not carry the audit
trail at all.

## Where to go next {#next}

- [Sessions and step-up](sessions-and-step-up) — ending a session the trail has made you suspicious of.
- [Backup and restore](backup-restore) — the other half of an incident: getting the server back.
