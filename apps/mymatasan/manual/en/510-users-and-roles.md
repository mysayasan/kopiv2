---
title: Users and roles
category: administration
categoryLabel: Administration
summary: Create accounts, choose the right role, and understand what the permission matrix enforces.
order: 510
---

# Users and roles

Local sign-in accounts live in **Settings → Users**. Each account has a role, and the role decides
what it may do.

## The three roles {#roles}

| Role | Can | Cannot |
|---|---|---|
| **Viewer** | Watch live video. See that an alert fired. Change their own password. | Open recorded footage. Acknowledge, PTZ, talk-back. Anything in Settings. |
| **Operator** | All of the above, plus play back and download footage, search object sightings, acknowledge alerts, PTZ and talk-back. | Delete anything. Change rules, cameras or settings. Manage users. |
| **Administrator** | Everything. | — |

New accounts default to **operator**. Move an account to viewer when it should be watch-only.

## Why operator cannot delete {#evidentiary}

The line under operator is the point of the whole model: **an operator who was present at an
incident cannot destroy the footage of it.**

That is what makes the recorder a record rather than a convenience. It is not a comment on anyone
you employ — it is the property that lets the footage be trusted afterwards, including by the
person who was on shift.

Resist the temptation to make everyone an administrator because a permission got in the way once.
Every account that can delete footage is an account whose footage means slightly less.

## It is enforced on the server {#enforcement}

Every request is checked against the signed-in user's role, not just the ones that write. Denied
areas are genuinely unavailable, not hidden behind a URL somebody could type.

The matrix is **deny by default**: anything not granted is refused. A user with no role assigned
can do nothing at all, which is the only safe reading of an account somebody started setting up and
did not finish.

## Creating an account {#creating}

Give it a username, a password and a role. Two habits worth keeping:

- **One account per person.** Shared logins destroy the audit value of everything else here.
- **The lowest role that does the job.** Promote when somebody hits a wall, rather than starting
  everybody at administrator.

An account can be deactivated instead of deleted, which is what you usually want when somebody
leaves — deletion removes the account, deactivation keeps the name attached to its history.

## Passwords {#passwords}

Everyone can change their own password from their own session. An administrator can reset anyone
else's.

The bootstrap admin account is always forced to change its password on first sign-in — see
[Signing in for the first time](first-sign-in#change-password). A reset password is best set as
must-change so the person picks their own.

Lockout after repeated failures is automatic and applies to everyone, including administrators —
see [When sign-in is locked](first-sign-in#lockout). An administrator cannot shorten someone
else's lockout, deliberately.

## The permission matrix {#matrix}

Beneath the roles is the matrix the server actually consults: a row per governed area of the API,
with what each built-in role may do in it.

The built-in roles are seeded from that catalog on first run. Editing the matrix is for the site
that genuinely needs a rung the three roles do not express — "operators here may also purge
diagnostic alerts", say. It is a real security control, so change it deliberately and re-test with
a real account of that role afterwards.

Two behaviours worth knowing:

- **The most specific matching rule wins**, and rules do not combine. That is how a broad grant is
  carved out by a narrower denial — settings readable, but users beneath it not.
- **Defaults are only applied to a role that has no permissions at all.** Once you have tuned a
  role, an upgrade will never silently reset it.

## Federated sign-in {#federated}

mymatasan authenticates against its own local accounts. It has no single-sign-on leg — that is
myidsan's job, and myseliasan's — so the accounts here are the accounts.

Keep at least two administrator accounts. An appliance whose only administrator has left, with a
password nobody knows, is recoverable only by resetting the admin login at the console.
