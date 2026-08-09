---
title: Object classes and groups
category: detection
categoryLabel: Detection & AI
summary: Map the model's raw labels onto names you use, and bundle them into groups.
order: 330
---

# Object classes and groups

The registry under **Objects → Classes** is what turns a model's vocabulary into yours. It is the
list a rule's **Detect** picker offers.

## Categories {#categories}

A **category** maps a friendly name onto one or more raw model labels.

*Vehicle* might contain `car`, `truck`, `bus` and `motorcycle`. A rule that detects *Vehicle* then
matches any of them, and you never have to remember which word the model uses.

Two kinds exist: **object categories** (person, vehicle, …) and **hazard categories** (fire,
smoke), which are the same mechanism labelled for what they represent.

## Labels {#labels}

Labels are the model's exact output words, and exactness is the whole game:

- They are matched exactly. `fire hydrant`, not `fire_hydrant`.
- A label only detects anything if a model that produces it is **active**. The registry marks a
  category **model not active** when nothing running emits its labels.
- A mistyped label never matches and never complains. It just sits there detecting nothing.

Search and pick from the list rather than typing. The list is the set of labels your active models
actually produce, so choosing from it is the only way to be certain.

Labels from a model you trained appear here once that model is activated in **Settings → AI**.

## Groups {#groups}

A **group** bundles several categories. A rule targeting the group matches any of them.

*Traffic* = *Vehicle* + *Person* + *Bicycle*, say. Groups are for when several categories always
travel together in your rules — define the bundle once and the rules stay readable.

## Filing a trained label under an existing category {#filing}

This is the tip worth knowing.

When you train a model with your own label — say `papa` for a specific vehicle — it appears as its
own top-level class by default. Usually that is not what you want: you want it to count as a
*Vehicle* everywhere *Vehicle* is already used.

Edit the existing category and add the label there. It stops being a separate top-level class, and
every rule that already detects *Vehicle* picks it up with no further change.

## Practical shape {#practice}

Keep the registry small and meaningful. A category per thing you write rules about, not a category
per label the model can produce.

Sites that end up with thirty categories usually have three that are used and twenty-seven that
make the rule picker unreadable. The registry is a vocabulary for your rules — if you would not say
it on a radio, it probably does not need to be a category.
