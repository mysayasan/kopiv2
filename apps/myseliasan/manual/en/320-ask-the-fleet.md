---
title: Ask the fleet
category: agent
categoryLabel: AI agent
summary: A grounded assistant over your own data and the built-in manuals — and what it will not do.
order: 320
---

# Ask the fleet

The chat on the **AI Insight** page answers two kinds of question:

- **What is my fleet doing?** — "which node was offline this week?", "why did alerts spike last
  night?"
- **How does the product work?** — "how do I add a camera?", "what is a claim code?"

They are answered from two different sources, and the difference matters.

## Where answers come from {#grounding}

Fleet questions are answered from **this control plane's own tables**: adopted nodes and their
state, the event feed, the windowed statistics, the latest digest's findings.

Product questions are answered from the **built-in manuals** — this one, and MyMataSan's, which is
compiled into this control plane so that questions about your camera appliances can be answered
here.

Nothing else is consulted, and **nothing leaves your network**. There is no external service in the
path, which is what makes the assistant usable on an air-gapped site at all.

The assistant is told to keep the two apart: the manual describes how the software works in
general, and must never be reported as an observation about your installation. "Recordings are kept
for 14 days" is a sentence about the product, not about your disk.

## Sources {#sources}

When an answer draws on the manual, the sections it used appear underneath it.

**Sources** are the sections the answer actually cited. Anything else it was offered appears
separately as **further reading** — so the list never implies the answer rests on a page it
ignored.

A section from this manual opens in the help drawer when you click it. A section from another
product's manual names the product instead, because this control plane cannot display another
appliance's help — but you now know which product and which page to open.

## Asking about one node {#node-detail}

Name a node in your question and the assistant also fetches **that node's own recent events** over
the control channel.

One node, briefly. It does not fan out across the fleet: that would put every unreachable node's
timeout into the path of your answer. If the named node is offline, the answer says it was
unreachable rather than stalling or quietly leaving it out.

## It answers in your language {#language}

Replies follow the interface language — English, Malay, Chinese or Arabic.

Answer quality in a language depends on the model you run. A small model may drift back into
English on non-Latin scripts; a larger one holds the language and cites more reliably. If Arabic or
Chinese answers come back in English, the model is the thing to change.

## With no language model {#no-model}

A control plane ships with no model enabled, and the chat box says so.

The manual half still works. You can search the built-in manuals from the same card and get the
sections that answer your question, with working links — searching needs no model, no download and
no network. It is a worse answer than prose and a much better one than nothing.

## What it will not do {#limits}

- **It does not act.** It cannot adopt a node, change a rule, acknowledge an alert or restart
  anything. It reads and answers.
- **It does not replace an alert.** Alerts, rules and the digest run whether or not a model is
  installed. Nothing that matters waits on inference.
- **It only sees a window.** Fleet questions are answered over a recent period, not all history.
- **It can be wrong.** It is grounded and told to refuse when the data does not contain the answer,
  but a small model can still misread what it was given. Cited event ids and manual sections are
  there so you can check it — for anything consequential, check it.

## When it says it does not have that data {#no-answer}

That is usually correct behaviour rather than a fault. The assistant is instructed not to guess,
so a question outside its window — or about something the manual genuinely does not cover — gets a
refusal instead of an invention.

Rephrasing with the words the product uses helps more than asking again. For a question about a
particular node, name the node.
