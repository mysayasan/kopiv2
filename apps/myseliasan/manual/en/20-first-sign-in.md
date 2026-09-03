---
title: Signing in for the first time
category: getting-started
categoryLabel: Getting started
summary: Find the one-time bootstrap password, then hand over to real accounts.
order: 20
---

# Signing in for the first time

## The bootstrap account {#bootstrap}

The first time MySeliaSan starts it creates a single **superadmin** account with a password
**generated for this install**. There is no shipped default password to look up, and no two
installs share one.

The password is put in two places, so you can find it whichever way you are running the control
plane:

- **On the console.** A banner is printed at startup with the address to open, the username and
  the password. In Docker that is `docker logs`; on Linux, the service journal. On a desktop
  install the address also opens in your browser by itself.
- **In a file.** A credential file is written into the data directory — use this when the console
  has scrolled away or the service runs with no visible window.

> [!NOTE]
> If you set `localAuth` in `config.json`, or the `LOCAL_ADMIN_PASSWORD` environment variable,
> before the first start, that password is used instead and is **not** echoed anywhere. The banner
> points at your configuration rather than printing a secret you already hold.

The account is flagged *must change password*, so the first thing you see after signing in is the
change-password screen. Enter the one-time password, then your own twice.

## This account is temporary {#handover}

The bootstrap superadmin exists to get you in. It is not meant to be the account you run the
estate with, and the first-run wizard has a **Handover** step for exactly that reason.

The intended end state is that real people sign in as themselves and the bootstrap account is
retired. Until that happens, every action in the audit log is attributed to a shared account,
which makes the audit log considerably less useful than it should be.

See [The first-run setup wizard](setup-wizard#handoff).

## Signing in with your identity server {#sso}

MySeliaSan can hand authentication to **MyIDSan**, so people sign in with the account they already
have.

You do not type the connection details twice. MyIDSan's Apps page exports the client it registered
for this control plane as a small JSON file; import that file here and the issuer, audience,
client ID and redirect URLs are filled in exactly as MyIDSan wrote them. Two consoles, one source
of truth.

If you do not run an identity server, skip it — the local account keeps working, and you can
connect one later.

> [!NOTE]
> The sign-in hop to MyIDSan is the one place a browser leaves the control plane, and it stays
> inside your own network. Nothing in this path reaches the internet.

## Signing in day to day {#daily}

The sign-in screen takes you either to the local account form or out to your identity server,
depending on how the control plane is configured. Around it sit two controls, both remembered on
this browser:

- The **language switcher** — English, Malay, Chinese and Arabic. Arabic mirrors the layout.
- The **help link**, which opens this manual. It works before you sign in, which is exactly when
  you are most likely to need it.

## When sign-in fails {#troubleshooting}

- **"Your account has no role assigned"** — a real account exists but nobody has given it a role
  yet. That is deliberate: a new user starts with nothing rather than inheriting access. An
  administrator assigns one under Roles & Access.
- **Repeated failures lock the address out** for a period that grows with each attempt. Wait for
  the countdown; nobody can shorten it. The **account** is counted separately from the address, so
  someone guessing at your username from elsewhere can lock you out of it even though you have
  typed nothing wrong. That is the deliberate cost of stopping a guessing attack spread across many
  addresses, and it clears the moment anyone signs in correctly. Changing your own password on the
  change-password screen counts the same way: repeated wrong *current* passwords lock it too.
- **The redirect from the identity server fails** — usually a certificate the control plane does
  not trust, or a redirect URL that does not match exactly. Re-import the SSO bundle rather than
  retyping the fields.

## Where to go next {#next}

- [What happens on the first start](first-start) — the setup page that runs before this one.
- [The first-run setup wizard](setup-wizard) — what the six steps do.
- [A tour of the workspace](workspace-tour) — what you are looking at once you are in.
