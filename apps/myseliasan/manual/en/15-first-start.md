---
title: What happens on the first start
category: getting-started
categoryLabel: Getting started
summary: A fresh install configures itself in the browser, before the control plane starts.
order: 15
---

# What happens on the first start

## Nothing is configured yet {#wizard}

A freshly installed control plane has no database, no cache, and no decision about which ports
to serve on. Those are read once, at startup, before MySeliaSan has so much as a database
handle — which is why they cannot be set from inside the app. At that point there is no app.

So the first start does not start the control plane. It opens a **setup page** instead, on a port
of its own, and waits for you. When you finish, the control plane starts in the same process, on
the ports you just chose. Nothing restarts.

## Finding the setup page {#finding-it}

The address is put in three places, so you can find it whichever way you are running:

- **Your browser.** It opens by itself on a desktop install.
- **On the console.** A banner prints the address. In Docker that is `docker logs`; on Linux, the
  service journal.
- **In a file.** `SETUP_URL.txt` is written into the data directory and removed once setup is
  done. Use this when the console has scrolled away, or the service runs with no visible window.

The page listens on `127.0.0.1:39530` and is reachable **only from the machine itself**. That is
deliberate: it accepts database credentials and the first administrator password, and there is no
account to sign in with yet. If you must reach it from another machine, set `setup.allowRemote` —
the address then carries a one-time token, and you should treat that link as a password.

> [!NOTE]
> If something else on the machine already holds the port, setup moves to a free one and says so
> on the console. Read the banner rather than assuming 39530.

## What it asks {#answers}

Five steps, each pre-filled with what the install shipped:

- **Database** — PostgreSQL, MariaDB/MySQL, or SQLite (a single file, with no server to run).
- **Cache** — in-process, or Redis. Redis is needed only to run more than one instance behind a
  load balancer.
- **Web address** — the HTTPS and HTTP ports, and the hostnames to answer for.
- **Administrator** — the built-in account's name and password. Leave the password blank and one
  is generated for this install.
- **Review** — everything you chose, in one list, before anything is written.

The database and cache steps each have a **Test connection** button that makes a real connection.
Use it. A wrong password found here costs you a click; found later, it stops the control plane
from starting.

Nothing is written until you choose **Finish**. Abandoning the page leaves the install exactly as
it was.

## Finishing {#then}

Finish writes your answers into `config.json` and starts the control plane. Your browser follows
it to its new address on its own.

Only the settings you were asked about are written — every other part of the file keeps its
existing values. Then carry on with [Signing in for the first time](first-sign-in#bootstrap).

> [!NOTE]
> Environment variables still win. If `DB_HOST`, or any other configuration variable, is set in
> the environment, it overrides what you typed here. That is what you want in a container, and a
> surprise anywhere else.

## Running setup again {#again}

Setup runs once. Afterwards the install records that it is configured and starts straight up, and
settings are changed from **Settings** inside the app instead — see [Settings](settings).

There is one exception, and it is the reason this page can be brought back: if the control plane
can no longer start at all — a moved database, a changed Redis password — start it with
`KOPIV2_SETUP=1` and the setup page returns, pre-filled, so you can repair the settings that are
stopping it from booting.

A database or cache being briefly unreachable never brings it back. Setup appears only when you
ask for it, or on an install that has genuinely never been configured.
