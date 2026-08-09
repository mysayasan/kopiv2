---
title: Teaching a camera a new skill
category: detection
categoryLabel: Detection & AI
summary: Teach a camera to recognise something the stock model does not know — no AI knowledge needed.
order: 340
---

# Teaching a camera a new skill

The stock model knows general things: people, vehicles, animals. It does not know *your* things —
your courier's uniform, your forklift, the difference between a good and a defective part on your
line.

Teach mode is how you show a camera those, without knowing anything about machine learning.

> [!NOTE]
> Teach mode ships in stages. Naming a skill, choosing its kind, picking the camera and area, and
> running teaching sessions to collect examples all work today. The **accuracy check** and
> **turn it on** steps arrive in a later update, and the wizard says so on those steps. You can
> set skills up and collect examples now; activating a taught skill comes with that update.

## Before you start {#prerequisites}

Teaching needs the AI runtime installed — the same one detection uses. If it is missing, the Teach
page says so and points at **Settings → AI**. See [Training custom models](training-models) for
what "the runtime" is.

## The three kinds of skill {#kinds}

The wizard asks what kind of skill this is, and the answer changes what happens behind the scenes:

**Recognise a new object.** Spot a thing anywhere in view — a courier uniform, a forklift, a
company truck. This is the common one.

**Tell good from bad.** Judge items appearing in the same place — good product versus defective on
a production line. It expects things to arrive in a consistent spot, which is what makes the
comparison meaningful.

**Spot anything unusual.** Learn what normal looks like and flag deviation. Use this when you
cannot enumerate what you are looking for — you know what *should* be there and want to know when
something else is.

## Naming it {#naming}

Describe it in your own words: *defective bottle cap*, *company van*, *courier uniform*. The name
becomes the label you will see in alerts and pick in rules, so make it something you would
recognise on a notification.

Names that collide with a built-in label are rejected — pick something more specific. That is
protecting you: a taught skill called `person` would be indistinguishable from the stock model's
own detections.

## Where it appears {#where}

Pick the camera that will learn the skill, then draw a box around where the object shows up. You
can leave the box empty to watch the whole view.

Draw the box when the thing genuinely appears in one place — a conveyor, a doorway, a bay. It
narrows what has to be learned and improves results markedly. Leave it empty when the thing could
be anywhere.

## Teaching sessions {#sessions}

Each skill has cards to collect examples for: the **target** (what should raise an alert) and
**normal / good** (what the ordinary case looks like). Press start on a card and show the camera
real examples; captures are labelled automatically as they arrive.

The coach tells you what it still needs, and it is worth doing what it says:

- **"Show me more X"** — it does not have enough of that class yet.
- **"The sets are uneven"** — one class has far more samples than the other. A lopsided teaching
  set produces a lopsided skill that answers with whichever class it saw more of.
- **"These captures look very similar"** — vary angle, position and lighting. Fifty photos of the
  same object in the same spot teach one appearance, and the skill fails on every other.

Review the filmstrip and flick away bad captures. A mislabelled example is worse than a missing
one.

## Skills and rules {#rules}

A taught skill, once active, becomes something a rule can detect like any other class. Rules
created for a taught skill are marked as such on the camera's AI Detection page, so you can see at
a glance which rules came from teaching rather than being written by hand.

## Managing the models behind it {#advanced}

Importing, activating and removing trained model files — and the object classes they bring — is in
**Settings → AI**. That is the same place a model trained outside this product is imported. See
[Training custom models](training-models).
