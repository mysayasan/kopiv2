---
title: Setting up a language model
category: agent
categoryLabel: AI agent
summary: Off, an endpoint on your network, or a model this app runs itself — including on an air-gapped site.
order: 330
---

# Setting up a language model

A language model is **optional**. With none, the [digest](fleet-digest) still computes its findings
and the manual is still searchable; what you lose is the readable narrative and the
[assistant](ask-the-fleet).

Everything below runs on your own network. Nothing is sent to any cloud.

## Three modes {#modes}

**Off** — digest only, no language model. This is how a control plane ships.

**External** — an OpenAI-compatible server already on your network. You provide the endpoint URL,
an optional API key, and the model name. Use this when you already run inference somewhere with
more hardware than the control plane has.

**Sidecar** — a `llama-server` process this app starts, supervises and restarts for you, listening
only on loopback. Use this when the control plane is the only machine available.

**Test** probes the endpoint you have typed *before* you save it, so a typo is caught while you are
still looking at the field rather than the next time the assistant is asked a question.

## Installing the sidecar {#install}

Two artifacts: the **server binary** and a **model file** (`.gguf`).

Where the control plane has internet access, download both from the settings page. The default
model is around a gigabyte; progress appears below the buttons.

## Air-gapped installs {#air-gap}

An air-gapped control plane cannot download anything, and this is the case the feature was built
for.

**Import** installs from a file you carried in — a server archive, or any `.gguf` model you already
have. Imports are deliberately unpinned: the point is to accept the artifact an operator brought on
a USB stick. The checksum of what you installed is recorded in the install log and the audit trail,
so provenance is *recorded* rather than *enforced*.

Downloads can also be switched off entirely for the control plane, in which case the page says so
and offers Import instead.

## Choosing a model {#model-choice}

Bigger models answer better and answer slower. On CPU that trade is the whole decision.

The default model is small enough to run on a modest control plane and is adequate for digest
narrative and straightforward questions. A larger model writes noticeably cleaner narratives, cites
more reliably, and — the practical difference — **holds non-Latin languages properly**. If your
operators read Arabic or Chinese and a small model keeps answering in English, that is the model,
not a setting.

## Tuning {#tuning}

| Setting | What it decides |
|---|---|
| **Context size** | How much the model can be given at once. The digest and the assistant both budget against it. |
| **CPU threads** | 0 means auto, which is usually right. |
| **Request timeout** | How long a slow answer is allowed to take. |
| **Max answer length** | Caps a reply, in tokens. |
| **Loopback port** | The sidecar listens here; it is not exposed off the machine. |

Inference on CPU is legitimately slow — a first token in a couple of seconds and a full digest
narrative in tens of seconds is normal, not a fault.

## What happens when the model is unavailable {#degradation}

Every path degrades on purpose rather than failing.

- Starting up (loading a model takes time) — the assistant says so and asks you to retry shortly.
- Failed or not installed — the assistant is unavailable and says why; the digest still runs.
- Crashed — the sidecar is restarted automatically. **Restart sidecar** forces it immediately.

Nothing that matters waits on inference. Alerts, rules, the feed and the digest's findings are all
computed without it, which is the property that lets you leave the model switched off.

## Who can change this {#permissions}

Installing, importing and pointing the control plane at an endpoint are **superadmin-only**,
regardless of the permission matrix. These actions can download gigabytes or aim the control plane
at an arbitrary network address, so they are not delegated.

Reading the digest and using the assistant are ordinary grants that a role can be given.
