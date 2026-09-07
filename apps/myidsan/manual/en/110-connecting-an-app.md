---
title: Connecting an app
category: federation
categoryLabel: Connecting apps
summary: Register a relying app, hand it a secret, and understand the hop it makes.
order: 110
---

# Connecting an app

An app that signs people in through MyIDSan is a **relying app**. Registering one is done on the
**Apps** screen, and takes four things: a code, an audience, a base URL, and at least one redirect
URI.

## What actually happens {#flow}

Nothing here is invented at sign-in time. The app sends the person's browser to MyIDSan with the
values you registered; MyIDSan runs its own gates and sends the browser back with a one-time code;
the app's **server** trades that code for the person's identity using a secret the browser never
sees.

```seq
title : One sign-in, from the relying app's side
actor app : Your app
actor browser : The person's browser
actor idsan => first-sign-in#gates : MyIDSan
app -> browser : Nobody is signed in here — go and ask
browser -> idsan : Authorize, quoting your client id, audience, redirect URI and a state you generated
idsan -> browser : The sign-in screen, and every gate behind it
browser -> app : Back to your redirect URI, carrying a one-time code and your state back
app -> idsan : Exchange the code, quoting your client secret
idsan --> app : Who they are, which role they hold, and for how long
```

Two consequences worth stating plainly:

- **The secret is used server to server.** If your app is a single-page app with no backend of its
  own, it has nowhere safe to keep the secret and cannot complete this exchange.
- **The code is one-time.** A second exchange of the same code is refused and recorded as a
  refusal in [the audit log](audit-log). That is what a replayed code looks like, and it is meant
  to be visible.

## Filling in the form {#form}

- **Code** — the short identifier for the app: lowercase letters, digits and hyphens, starting
  with a letter. It is the app's stable handle and cannot be changed later.
- **Audience** — the `aud` the issued token is stamped with. The convention in this suite is that
  it matches the code, and the form keeps it in step until you edit it. An audience that differs
  from the code is legal; it just has to be said deliberately.
- **Base URL** — where the app lives. `http://` is accepted only for `localhost`; anything else
  over plain HTTP is flagged, because the redirect carries an authorization code.
- **Redirect URI** — where MyIDSan sends the browser back to. **It is matched exactly**: scheme,
  host, port, path, trailing slash and all. A registered `https://app.example.com/api/auth/callback`
  does not match `https://app.example.com/api/auth/callback/`. The suite's own apps mount this at
  `/api/auth/callback`.

The right-hand side of the screen shows the **live authorize URL** and a configuration snippet
built from what you have typed so far. Read those rather than this page: they are generated from
this server's own address and your actual values, so they cannot be out of date.

## The client secret {#secret}

Generate the secret on the Apps screen. MyIDSan stores only a hash of it — the API never returns
it, and it exists in plaintext **only in the browser tab that generated it**.

That has one practical consequence: copy it, or export the bundle, before you leave the page. If
you lose it, you rotate it rather than recover it, and the app stops signing anyone in until it is
reconfigured with the new one. Rotation is recorded in the audit log.

## Handing the values over {#export}

Rather than retyping a client id and an audience across two consoles, use **Export** on the Apps
screen. It writes a small JSON file holding the issuer, audience, provider base URL, client id,
redirect base and path, and the session lifetime — and the secret too, but only when one was just
generated in that same tab. The file says which of the two it is rather than silently shipping a
bundle that will not work.

MySeliaSan imports that file directly on its Settings screen: it fills the form in and waits for
you to save. Two consoles, one source of truth.

## The lifetimes {#lifetimes}

```spec
title : How long each part of the exchange lives, as it ships
row code `300s` : An authorization code. It only has to survive the redirect back and your app's immediate exchange, so it is deliberately short.
row token `900s` : The access token handed to your app.
row session `259200s` : The sign-in here — three days. This is why moving between apps does not ask for a password again, and it is the number that decides how long a stolen session cookie is worth something.
```

Each can be overridden per app on the Apps screen, which is the right place to shorten them for
one sensitive app without shortening them for everybody.

> [!NOTE]
> The three-day session is also why [step-up re-authentication](sessions-and-step-up#stepup)
> exists. A cookie taken from an unlocked laptop is good for three days; step-up is what stops it
> being good for role changes and identity exports.

## When it does not work {#troubleshooting}

Almost every failure is one of four, and the audit log names which:

- **`redirect_uri is not registered`** — an exact-match failure. Compare the two strings
  character by character, including the trailing slash.
- **An unknown client** — the code or client id does not match a registered, active app. Check the
  app has not been set inactive.
- **The secret is wrong** — usually a rotation that reached one side only.
- **The code was already used** — either a genuine replay, or your app retrying the exchange after
  a timeout. Retry the whole authorize hop, not the exchange.

Everything in that list is written to [the audit log](audit-log#federation) as a refusal, with the
app it was refused for. That is the first place to look — your app only sees its own error.

## Where to go next {#next}

- [Users, roles and groups](users-roles-groups) — the role your app receives.
- [The audit log](audit-log) — who was let into which app.
