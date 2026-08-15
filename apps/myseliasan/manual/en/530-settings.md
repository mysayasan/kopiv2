---
title: The settings editor
category: admin
categoryLabel: Administration
summary: Edit the control plane's configuration in the app — and why a restart is part of it.
order: 530
---

# The settings editor

**Settings** edits the control plane's own configuration from inside the app. Changes are written
to `config.json` and take effect **after a restart**.

Only a **superadmin** can view or change them, and that is not a matrix rule you can delegate:
these values decide how the control plane authenticates people and reaches your fleet.

## Save, then restart {#restart}

Saving records the change; it does not apply it. The page then tells you a restart is required and
offers **Restart now**.

The split is deliberate. A configuration edit that took effect mid-request would change the rules
under sessions already in flight — the honest version is to write the file, say so plainly, and
restart when you are ready. It also means you can make several related changes and pay for one
restart.

Expect the app to be briefly unavailable when you do it.

## What you can edit here {#sections}

| Section | Covers |
|---|---|
| **Local Login** | The built-in administrator account used when single sign-on is unavailable. |
| **Single Sign-On** | Federated login through the myidsan identity provider. |
| **Connectivity** | Node discovery, adoption, and the ports the fleet uses to reach this control plane. |
| **Security** | Tokens, TLS certificates, content security policy, API rate limits. |
| **Storage & Cache** | Uploaded-file storage, its cleanup, and the cache backend. |
| **Logging & Telemetry** | Application and API logging, retention, and the metrics endpoint. |
| **AI Agent** | The daily digest and the optional language model — see [Setting up a language model](language-model). |
| **System** | The running version and process controls. |

## What you cannot edit here, on purpose {#not-here}

This is a **safe subset** of the configuration. The database connection, the server's listening
setup and the bootstrap behaviour are not editable from the app.

That is a deliberate limit rather than an oversight: a typo in a database setting saved through the
very app that needs the database to run would leave you with an appliance that cannot start and no
screen to fix it from. Those values stay in the file, where fixing them does not depend on the app
being healthy.

## Secrets {#secrets}

A secret field left **blank keeps its current value**; it is not cleared.

Stored secrets are never displayed back to you, so blank means "unchanged" rather than "empty" —
which is what lets you edit a neighbouring field without re-typing a key you may not have to hand.

## Restore defaults {#defaults}

Each section can be reset to its original values. That still needs a restart to apply, and it is a
per-section action rather than a whole-configuration reset.

It is the right move when a section has been tuned into a state nobody can explain — start from the
shipped values and change one thing at a time.

## If the control plane will not start after a change {#recovery}

An app that won't start after a settings change is almost always a bad value in the file, and the
file is where you fix it. Configuration edited here lands in `config.json` in the app's data
directory.

If a change stops the control plane starting, that file is the recovery path: correct or remove the
offending value on disk and start it again. The editor is a convenience over a file that remains
readable and fixable by hand, which is exactly the property you want on the day the convenience is
unavailable.
