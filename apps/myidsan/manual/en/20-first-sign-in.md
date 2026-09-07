---
title: Signing in for the first time
category: getting-started
categoryLabel: Getting started
summary: Find the one-time bootstrap password, and understand the screens that can hold you up.
order: 20
---

# Signing in for the first time

## The bootstrap account {#bootstrap}

The first time MyIDSan starts it creates a single **superadmin** account with a password
**generated for this install**. There is no shipped default password to look up, and no two
installs share one.

The password is put in two places, so you can find it whichever way you are running the server:

- **On the console.** A banner is printed at startup with the address to open, the username and
  the password. In Docker that is `docker logs`; on Linux, the service journal.
- **In a file.** `INITIAL_ADMIN_LOGIN.txt` is written into the data directory, readable only by
  the account the server runs as. Use this when the console has scrolled away or the service runs
  as a Windows service with no visible window. **Delete it once you have signed in.**

> [!NOTE]
> If you set `localAuth` in `config.json`, or the `LOCAL_ADMIN_PASSWORD` environment variable,
> before the first start, that password is used instead and is **not** echoed anywhere. The banner
> points at your configuration rather than printing a secret you already hold.

The account is flagged *must change password*, so the first thing you see after signing in is the
change-password screen. Enter the one-time password, then your own twice.

## Signing in does not always mean arriving {#gates}

A correct password gets you a session. It does not always get you the workspace: there are four
separate things that can hold you on a screen of their own, and they are checked in a fixed order.
Knowing the order is usually the whole answer to "I signed in and I am stuck".

```flow
title : Why a correct password can still leave you on a screen of its own
step creds : You enter a username and a password
ask locked : Too many recent failures from this address?
end lockout : Refused until the countdown runs out
ask factor => second-factor : Does this account already have a second factor?
step code => second-factor : Enter a code from your authenticator, or a recovery code
ask pwd : Is the password marked must-change?
step change : Set a password of your own before anything else
ask enrol => second-factor#policy : Does the policy require a factor this account does not have?
step enrolnow => second-factor#enrol : Enrol an authenticator before anything else
ask role => users-roles-groups#pending : Has anyone given this account a role?
end pending => users-roles-groups#pending : Waiting for clearance — an administrator has to assign one
ok workspace : The workspace, showing only what your role may open
creds -> locked
locked -> lockout : yes
locked -> factor : no
factor -> code : yes
factor -> pwd : no
code -> pwd
pwd -> change : yes
pwd -> enrol : no
change -> enrol
enrol -> enrolnow : yes
enrol -> role : no
enrolnow -> role
role -> pending : no
role -> workspace : yes
```

The two that surprise people are the last two. **Enrolment** is asked for *after* the password
succeeds rather than instead of it — the policy is making you ADD a factor, not prove one you do
not have, so switching the policy on cannot lock out the administrators who have none yet.
**Clearance** is not a fault: a new account starts with no role at all rather than inheriting one,
so somebody has to grant it.

## The lockout, and who it actually locks {#lockout}

Repeated failures lock the **source address** out for a period that grows with each repeat. Every
failure also costs a small fixed delay, whether or not the account exists — that is deliberate, so
the response time cannot be used to find out which usernames are real.

```spec
title : The sign-in policy, as it ships
row minlength `12 characters` : The shortest password a person may choose. Character-class rules (upper, lower, digit, symbol) are all OFF by default — a longer passphrase beats a short password with a symbol bolted on. A password equal to the username is always rejected, whatever else is configured.
row attempts `8` : Failures from one address before it is locked out.
row window `300s` : The window those failures are counted over.
row lockout `60s` : The first lockout. It doubles on each repeat.
row lockoutmax `3600s` : The longest a lockout can grow to.
row delay `400ms` : Added to every failed attempt, so a wrong username and a wrong password take the same time to refuse.
```

All six are editable under **Settings › Sign-in**. Two things there are worth knowing before you
change them: the lockout cannot be set below two attempts (one would lock an account out on its
first typo), and the password floor cannot be set below eight.

> [!WARNING]
> Nobody can shorten a lockout from inside the app — not a superadmin, not the locked-out user.
> Wait for the countdown. If you have locked yourself out of the last superadmin account, use the
> recovery below.

## If you are locked out of the only superadmin {#reset-admin}

Both escape hatches are files you drop in the data directory. They are consumed on the next start
and deleted before they act, so a crash can never re-apply one behind your back.

- `RESET_ADMIN` — regenerates the bootstrap superadmin's password and announces the new one the
  same way a first start does, banner and `INITIAL_ADMIN_LOGIN.txt` both.
- `RESET_MFA` — clears the bootstrap superadmin's second factor only, leaving the password alone.
  See [Second factors and account recovery](second-factor#lost).

Both require write access to the data directory on the host. That *is* the authorisation: anyone
who has it could already read the database. The audit log records the reset with no actor, which
is the honest entry — nobody signed in to cause it.

## Signing in day to day {#daily}

The sign-in screen offers whichever methods are configured: the local username and password, your
directory (LDAP/AD) if one is connected, a Kerberos button on a domain-joined desktop, and any
social provider that has been enabled. Around the card sit three controls, each remembered in this
browser rather than on the account:

- The **language** switcher — English, Malay, Chinese and Arabic. Arabic mirrors the layout.
- The **theme** picker, including a high-contrast palette.
- The **help link**, which opens this manual. It works before you sign in, which is exactly when
  you are most likely to need it.

## Where to go next {#next}

- [Users, roles and groups](users-roles-groups) — clearing that pending account.
- [Second factors and account recovery](second-factor) — enrolment, recovery codes, security keys.
- [Connecting an app](connecting-an-app) — pointing your first app at this server.
