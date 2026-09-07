---
title: Second factors and account recovery
category: security
categoryLabel: Security
summary: Authenticator codes, security keys, recovery codes, and the way back in when they are gone.
order: 310
---

# Second factors and account recovery

A second factor applies only to credentials **this server checks itself** — local accounts. A
directory or social account's second factor belongs to whoever issues it, unless you say
otherwise; see [the policy](#policy) below.

Everything on this page for your own account lives behind the chip at the bottom of the rail,
under **Profile**.

## An authenticator app {#enrol}

Enrolment stages a factor and only activates it once you prove a code, so a mis-scanned QR cannot
lock you out. Scan the code with any TOTP app — Google Authenticator, Aegis, 1Password — or type
the key in by hand if the camera is a problem, then enter the six digits it shows.

Codes are the usual six digits on a thirty-second step. One step either side of *now* is accepted
and no more, so a device whose clock has drifted by more than about a minute will be refused
every time. If codes stop working suddenly and nothing else changed, check the phone's clock
before anything else.

## Recovery codes {#recovery}

Enrolling mints **ten single-use recovery codes**, shown once and never again. Each one gets you
past the second-factor prompt exactly once and is then spent.

Save them somewhere that is not the phone holding the authenticator. That is the whole point: the
event they exist for is the one where the phone is gone.

Your Profile shows how many are left. Regenerating replaces the whole set — the old ten stop
working immediately — and requires a current code, so somebody with only your cookie cannot mint
themselves a fresh set.

Using one is recorded in [the audit log](audit-log) as its own action, separate from an ordinary
sign-in. A recovery burn is the strongest available signal that someone has lost an authenticator
— or taken one over — and collapsing it into "signed in" would hide exactly that.

## Security keys {#keys}

A hardware key or a built-in authenticator (Windows Hello, Touch ID) is stronger than a code: it
proves itself with a signature, its secret never leaves the device, and there is nothing on this
server to phish or copy. Add them under **Profile › Security keys**.

Two things to know:

- **They need HTTPS.** On a plain `http://` address the browser will not offer them at all, in any
  browser. That is the browser's rule, not this server's.
- **An account can hold several**, and should. One key is a single point of failure; a key plus an
  authenticator app means losing either is an inconvenience rather than a recovery procedure.

## Requiring a factor {#policy}

Under **Settings › Sign-in**, the policy is one of three:

- **off** — nobody is prompted. Factors that already exist are still honoured at sign-in.
- **optional** — self-service only. This is the default.
- **required** — everyone in scope must enrol before they can use the app.

"Required" can be narrowed to specific roles, which is the common shape: mandatory for
administrators, optional for everyone else.

Enforcement happens **after** the password succeeds. Someone who owes a factor still gets a
session and is pinned to the enrolment screen until they add one — see the gate order in
[Signing in for the first time](first-sign-in#gates). That is what makes the policy safe to switch
on while people are using the server: it makes them ADD a factor, rather than prove one they do
not have.

Directory accounts are **not** in scope unless *apply to directory* is also on. See
[Connecting a directory](directory#mfa).

## When the authenticator is gone {#lost}

```flow
title : Getting back in when the authenticator is gone
step lost : Your authenticator is lost, wiped or replaced
ask codes : Do you still have one of your recovery codes?
ok signin : Sign in with it, then enrol a new authenticator straight away
ask other : Is there another superadmin who can act for you?
step admin : They clear the second factor on your account
ask sole : Is the locked-out account this server's ONLY superadmin?
step marker => first-sign-in#reset-admin : Drop a RESET_MFA file in the data directory and restart
end wait : Ask a superadmin — the factor cannot be bypassed from the sign-in screen
lost -> codes
codes -> signin : yes
codes -> other : no
other -> admin : yes
admin -> signin
other -> sole : no
sole -> marker : yes
sole -> wait : no
marker -> signin
```

A superadmin clearing somebody else's factor has to re-prove their own credentials first — it is
exactly what an attacker holding a stolen cookie would try, so it sits behind
[step-up](sessions-and-step-up#stepup). Today that clearing is an API call rather than a button on
the Users screen.

The `RESET_MFA` marker clears the **bootstrap superadmin's** factor and nothing else — not the
password, not anybody else's factor. It is consumed and deleted on the next start before it acts,
and recorded in the audit log with no actor, because nobody signed in to cause it. Whoever dropped
the file had write access to the data directory, and that is what the entry is pointing at.

## Forgotten passwords {#password-recovery}

The **Forgot password** link on the sign-in screen works for local accounts and always tells the
person the same thing, whether or not the account exists. An identity server that says "no such
user" is a directory listing for anyone who wants one.

Behind that, one of two things happens:

- **The operator queue.** A pending request appears under **Reset requests**. A superadmin resolves
  it by issuing a fresh temporary password, flagged must-change, and hands it over by whatever
  channel they trust. This path always works, including with no mail server anywhere — which is
  the normal case on an air-gapped install.
- **A self-service link**, only when an internal SMTP relay has been configured. The link lives
  half an hour and sets a password for one account, once.

Directory and social accounts are not handled here; their passwords belong to their provider, and
the screen says so rather than pretending.

## Where to go next {#next}

- [Sessions and step-up](sessions-and-step-up) — what re-authentication protects.
- [The audit log](audit-log) — every factor change and every recovery burn.
