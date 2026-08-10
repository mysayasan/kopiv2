---
title: The fleet digest
category: agent
categoryLabel: AI agent
summary: What happened, what changed, what needs attention — computed, not guessed.
order: 310
---

# The fleet digest

**AI Insight** opens with the digest: a short account of what your fleet did, what changed, and
what deserves a look.

## The findings are computed, not written by a model {#findings}

This is the part worth understanding, because it decides how much you can trust the page.

The findings — nodes that went quiet, alert volumes against the expected range, a source behaving
unlike itself, certificates approaching expiry — are produced by ordinary code reading your own
tables. They are the same every time for the same data, and they appear **whether or not a language
model is enabled**.

A language model, when you have one, writes the readable narrative *over* those findings. It never
invents them, and it is never in a position to suppress one.

That ordering is deliberate: an alert you needed must not depend on a model being installed,
loaded, or having a good day.

## Daily and weekly {#cadence}

The **daily** digest runs on a schedule and covers the operational picture: the last day's events,
what is newly wrong, what is still wrong.

The **weekly** digest is the management cadence — seven days, trends rather than incidents.

**Generate now** produces one immediately without disturbing either schedule. If the scheduled
digest is switched off, the page says so rather than leaving you wondering why nothing arrives.

## Reading the activity chart {#chart}

The chart shows daily event counts against a **learned normal band**.

The band is what makes the chart worth reading. A hundred events is meaningless on its own; a
hundred events where you normally see twelve is a spike, and twelve where you normally see a
hundred is a **silence** — which is very often the more serious of the two. A camera that stopped
reporting produces no alerts at all, and a chart without a band makes that look like a quiet day.

## Suggested rules {#suggestions}

The agent may propose a fleet rule when it sees a pattern that keeps repeating — several nights of
after-hours activity at the same place, for instance.

A suggestion is **prefilled, never created**. It opens the rule editor with the fields populated so
you can look at it, change it, and decide. Nothing starts watching your site because software had a
hunch.

## What the digest deliberately ignores {#exclusions}

The digest does not count **its own** entries as fleet activity.

Without that rule the page eats itself: the digest publishes a critical finding, the next digest
sees a critical event in the feed, and every digest from then on is critical forever. The same law
applies here as to any rule engine — a conclusion is never evidence.

## When there is no digest yet {#empty}

A new control plane has nothing to summarise. The digest appears once the scheduled run happens, or
immediately if you generate one. If it stays empty after that, check that nodes are actually
adopted and reporting.
