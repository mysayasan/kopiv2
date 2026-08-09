---
title: Training custom models
category: detection
categoryLabel: Detection & AI
summary: Build a dataset, label it, train or export it, and activate the result.
order: 360
---

# Training custom models

Training is the manual route to the same destination as [Teach mode](teach-mode): a model that
recognises something the stock model does not. Teach mode wraps this for people who do not want to
think about datasets. This page is for when you do.

## The runtime {#runtime}

Both detection and training need an AI runtime — Python plus the detection libraries — installed
from **Settings → AI**. Without it, nothing detects and nothing trains.

Training is far heavier than detection. On CPU it is measured in hours; on a decent GPU, minutes
to tens of minutes. If you plan to train regularly and have no GPU, plan to export the dataset and
train elsewhere instead.

## Datasets {#datasets}

A dataset is a set of images plus the boxes and labels drawn on them. Build one by:

- **Uploading images** directly, or
- **Importing an alert snapshot**, which arrives already carrying the detection's box and label.

The second is the underrated one. Your alert history is a free, perfectly on-distribution dataset:
real camera, real angle, real lighting, real weather. Images from those cameras beat better
photographs from anywhere else, because they match what the model will actually be given.

## Labelling {#labelling}

Draw a box around each instance and give it a label. Auto-label runs the active model over an
image and pre-fills what it finds, which you then correct — much faster than starting from an
empty frame.

What actually determines whether the result works:

- **Be consistent.** If you box the whole vehicle in some images and just the cab in others, you
  have taught it two different things and it will do neither well.
- **Label every instance in an image.** An unlabelled object in a training image is actively
  teaching the model that this thing is *not* the class — worse than leaving the image out.
- **Include negatives.** Images with none of your object, and images with things that look like it
  but are not. This is where most of the false-positive reduction comes from.
- **Vary everything.** Angle, distance, light, weather, time of day. A model trained only on
  midday images fails at dusk.

Rough scale: a few hundred varied instances gets something usable; a few dozen near-identical ones
does not.

## Labels {#labels}

Use a distinctive label rather than a general word. A model trained on `van` competing with the
stock model's own `truck` is a confusing setup; `acme-van` is not.

To make that label count as an existing category everywhere your rules already use it, add it to
that category rather than leaving it as a top-level class — see
[Object classes](object-classes#filing).

## Training or exporting {#training}

Train in place, or **export a YOLO dataset** — a zip with `data.yaml` and an images/labels
train/val split — and train it elsewhere on better hardware. The exported layout is the standard
one, so any YOLO tooling reads it.

Exporting is the right default for anything beyond a small dataset. Train on a machine with a GPU,
bring back the weights.

## Activating a model {#activating}

Import the `.pt` weights in **Settings → AI** and activate it. Its labels then appear in the object
class registry and can be used in rules.

Two things follow immediately:

- **Every active model inferences every frame.** Activating a second model roughly doubles
  detection cost. Deactivate models you are not using.
- **Model weights are stored as plain files**, not encrypted at rest like recordings, because the
  detection worker reads them directly. A model that is commercially sensitive should be treated
  as such.

## Judging whether it worked {#evaluating}

Do not judge a model on the images it was trained on — it has seen them.

Point it at a real camera, leave it for a day, and read the alert log. That is the only test that
predicts how it will behave, and it usually reveals the same two things: a false-positive source
nobody anticipated, and a lighting condition that was missing from the dataset. Both are fixed by
adding those images and retraining, which is why building a dataset from your own alert snapshots
compounds so well.
