---
title: What MyIDSan is
category: getting-started
categoryLabel: Getting started
summary: The one server that decides who is who, and what the rest of the suite asks it.
order: 10
---

# What MyIDSan is

MyIDSan is the **identity server** for the suite. It holds the accounts, checks the passwords and
second factors, and tells every other app who just arrived. Nothing else in the suite stores a
password.

That makes it the smallest app in the suite and the one with the widest blast radius. When MyIDSan
is down, nobody can sign in to anything that has been pointed at it — so most of this manual is
about the two things that keep that from happening: getting the first sign-in right, and being
able to rebuild the server from a backup.

## What it does {#does}

- **Accounts.** Local accounts with passwords, and accounts that come from an LDAP or Active
  Directory server you already run. See [Users, roles and groups](users-roles-groups) and
  [Connecting a directory](directory).
- **Second factors.** A code from an authenticator app, or a security key. See
  [Second factors and account recovery](second-factor).
- **Single sign-on for your apps.** Another app sends a person here to sign in and gets back a
  short-lived token saying who they are. See [Connecting an app](connecting-an-app).
- **A record of all of it.** See [The audit log](audit-log).

## What it does not do {#does-not}

Being clear about this saves an integration afternoon.

- **It is not a general-purpose OpenID Connect provider.** The sign-in hop it offers is an
  authorization-code exchange of its own shape, described in
  [Connecting an app](connecting-an-app#endpoints). Off-the-shelf OIDC client libraries expect a
  discovery document and a JWKS endpoint; MyIDSan publishes neither. Apps in this suite ship a
  client that speaks it.
- **It does not decide what you may do inside another app.** It says who you are and which role
  you hold; each app maps that role onto its own permissions. A role named the same thing in two
  apps can mean two different things.
- **It does not send mail by default.** Account recovery works as an operator queue with no mail
  server at all — see [Second factors and account recovery](second-factor#recovery).

## Where things are {#layout}

The rail on the left is grouped the way the work divides:

- **Administration** — Users, Reset requests, Groups, Roles, RBAC.
- **Federation** — Apps (the ones that sign in through this server) and Directory.
- **Access Control** — Endpoints, the catalog roles are granted against.
- **System** — Audit log, Backup & restore, Settings, and this manual.

Your own account — password, second factor, security keys, your sessions — is not in the rail. It
is behind the chip at the bottom of the rail, under your role name.

> [!NOTE]
> A short rail is not a bug. The menu is built from what your role is actually allowed to open, so
> two people signed in to the same server see different menus. If something you expect is missing,
> that is the answer: see [Users, roles and groups](users-roles-groups#matrix).

## Where to go next {#next}

- [Signing in for the first time](first-sign-in) — the bootstrap password, and the screens that
  can hold you up before you reach the workspace.
- [Connecting an app](connecting-an-app) — registering the first relying app.
- [Backup and restore](backup-restore) — do this before you need it.
