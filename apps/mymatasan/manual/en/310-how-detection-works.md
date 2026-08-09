---
title: How detection works
category: detection
categoryLabel: Detection & AI
summary: Models, labels, confidence and frames — the ideas you need before tuning anything.
order: 310
---

# How detection works

Understanding this article saves more time than any other in the manual, because almost every
"the AI is wrong" problem is really a question of which of these layers is at fault.

## The chain {#chain}

1. A **frame** is pulled from the camera's detection stream.
2. Every **active model** looks at it and reports the objects it recognises, each as a **label**
   (`person`, `car`, `truck`, …), a **box**, and a **confidence** between 0 and 1.
3. Those raw labels are mapped onto **object classes** you have named — *Person*, *Vehicle*,
   *Delivery*.
4. Each **rule** on that camera decides whether what was seen, where it was seen, when, and how
   confidently, is worth an **alert**.

The model produces facts. The rule applies judgement. Keep the two apart and diagnosis becomes
easy:

- **It never sees the thing at all** → model or confidence threshold.
- **It sees it but does not alert** → rule: zone, schedule, class selection, minimum frames.
- **It alerts on the wrong things** → rule, almost always: too broad a zone or too low a
  threshold.

## Models {#models}

The **stock (base) model** is always on. It recognises the general everyday classes — person,
vehicle, animal and so on. Its size is a straight speed/accuracy trade:

| Variant | Character |
|---|---|
| Nano | Fastest, least accurate. The default, and the right choice on CPU or a Raspberry Pi. |
| Small | A little slower, noticeably better. |
| Medium | Noticeably slower. A GPU is recommended. |
| Large | Slow on CPU. GPU recommended. |
| Extra-large | Slowest, most accurate. GPU strongly recommended. |

**Custom models** run alongside the stock model, not instead of it. That is what lets a model you
trained for one specific object coexist with general detection.

The cost is per model, per frame: every active model inferences every frame. Two active models is
roughly twice the work of one. Turning on a large stock model *and* two custom models on a modest
machine is the usual explanation for a system that has become sluggish.

Known stock variants are downloaded on first use and then cached, so that one download needs
internet access. Everything afterwards is local.

## Labels and object classes {#labels}

Models emit **raw labels** — lowercase words like `person`, `car`, `fire hydrant`. Those are the
model's vocabulary, and they are exact: a label that no active model produces will never match
anything, and a mistyped one matches nothing silently.

You do not write rules against raw labels. You write them against **object classes** you define,
which map a friendly name onto one or more raw labels — *Vehicle* = `car`, `truck`, `bus`,
`motorcycle`. See [Object classes and groups](object-classes).

## Confidence {#confidence}

Confidence is how sure the model is. A rule's **threshold** is the minimum it will accept.

There is no correct value, only a trade you are choosing:

- **Lower** catches more real events and more false ones.
- **Higher** is quieter and misses more.

Start around the default, watch the alert log for a day, and move it. Move it *because of what you
saw*, not on principle. A camera looking at a bright, close, unobstructed doorway can run a high
threshold; one looking down a long dark drive cannot, and forcing one on it just means it never
fires.

## Minimum frames {#min-frames}

How many consecutive frames must contain the object before the rule fires.

This is the single most effective control against flicker — a shadow, a moth, a compression
artefact — because noise rarely persists across frames while a real person always does. Raising it
from one to two or three eliminates most spurious alerts at the cost of a fraction of a second.

## Cooldown {#cooldown}

After a rule fires, it stays quiet for this many seconds.

Without it, one person standing in a doorway produces an alert per detection for as long as they
stand there. Cooldown converts "an event is happening" into "an event happened", which is what a
notification should mean.

## Where frames come from {#frames}

Detection reads the camera's **detection** stream, which you assign on the camera's
[Stream tab](camera-properties#stream). It does not have to be the stream you record.

Use a sub-stream. The detector does not need 4K to recognise a person, and the difference in cost
between a sub-stream and a main stream is often the difference between a machine that copes and
one that does not. The exception is [licence plates](fire-smoke-and-plates#lpr), which need every
pixel they can get.

## What it cannot do {#limits}

- **It does not understand intent.** It recognises a person; it cannot tell a delivery driver from
  an intruder. Zones, schedules and classes are how you supply that context.
- **It cannot see what the camera cannot.** No model recovers detail from a dark, wet, backlit or
  badly focused image. Camera placement beats model size, every time.
- **It is not a person.** Every threshold is a probability. Build your process on the assumption
  that some alerts are wrong and some events are missed, because both will happen.
