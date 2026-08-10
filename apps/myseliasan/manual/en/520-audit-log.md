---
title: The audit log
category: admin
categoryLabel: Administration
summary: An append-only record of the actions somebody might later have to explain.
order: 520
---

# The audit log

**Audit Log** is an append-only record of sensitive control-plane actions: who did what, to what,
and whether it worked.

Append-only is the point. It is not a feed you tidy up — it is the thing you consult when a node
vanished, a permission changed, or somebody asks what happened on Tuesday.

## What each row tells you {#columns}

| Column | Meaning |
|---|---|
| **Time** | When it happened. |
| **Actor** | The account that did it, or **System** for something no person triggered. |
| **Action** | What was attempted. |
| **Target** | What it was done to — a node, a user, a role. |
| **Outcome** | Success, denied, or error. |
| **Detail** | The specifics worth keeping. |

## Denied attempts are recorded too {#denied}

The outcome column has three values, and **denied** is the one people forget to look for.

A refused action is evidence. Somebody repeatedly attempting something they are not allowed to do
is a different situation from somebody doing it once successfully, and a log that recorded only
successes could not tell you which you were looking at.

## What gets recorded {#actions}

The fleet-shaped actions:

- **Node adopted**, **released**, **self-dropped**, and commands sent to a node.
- **Fleet key rotated** — worth noticing, because it changes which appliances can be discovered at
  all.

The access-shaped actions:

- **Role changed**, **user enabled or disabled**, **user elevated** to superadmin.
- **Node access granted** or **revoked**.

Plus the AI agent's own management actions — installing or importing a model, pointing the control
plane at an endpoint, generating a digest on demand.

The list is deliberately not "everything". A log of every read is a log nobody reads; these are the
actions that change what the system is or who can use it.

## System as an actor {#system}

Some rows have **System** as the actor because no person triggered them — a node dropping itself,
a scheduled job, an automatic recovery.

That is not an anonymised user. It means the action genuinely had no human behind it, which is
often exactly what you needed to establish.

## Using it {#using}

Read it when the fleet surprises you.

A node that disappeared is either **released** (an actor, on purpose), **self-dropped** (System,
from the node's own side) or simply offline with nothing logged at all — and those three point at
completely different next steps. The log turns "the node is gone" into a question with an answer.

The same applies to permissions: "I could see this yesterday" is answered by a role change with a
name and a timestamp against it.

A copy of the audit trail is included in the **Security & Access** [report](reports), which is the
form to hand to somebody who needs it on paper.
