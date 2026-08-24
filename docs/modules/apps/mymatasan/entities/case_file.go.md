# Module: apps/mymatasan/entities/case_file.go

## Purpose

Declares `CaseFile`: one investigation (W3-3). A titled, assignable folder that footage,
sightings and notes are collected into, worked on, and eventually closed with a stated
outcome.

Everything else this appliance records is organised by CAMERA and by TIME, which is how the
appliance thinks and not how an incident does. An incident is one person crossing four
cameras over eleven minutes, an alert that fired in the middle of it, and two things somebody
noticed afterwards. Before this the only container for that was an operator's memory and a
folder of downloaded `.mp4` files — which is also where the chain of custody ends, because a
file on somebody's desktop carries no record of where it came from or who took it.

## Fields

| Field          | Type   | Notes |
|----------------|--------|-------|
| `Id`           | int64  | Auto-increment primary key. |
| `Title`        | string | Required. An untitled case is one nobody else can pick up. |
| `Summary`      | string | Free text description of the incident. |
| `Status`       | string | `open` or `closed` (`idx:"status_updated"` with `UpdatedAt`). Two states and no more: an "in progress" state nothing behaves differently for is a field people forget to move, and the assignment already carries "somebody is on this". |
| `AssignedTo`   | int64  | Local user id, 0 = unassigned. |
| `AssignedName` | string | Denormalised display name — see below. |
| `OpenedBy` / `OpenedName` / `OpenedAt` | int64/string/int64 | Who opened it and when. |
| `Outcome`      | string | What the case concluded. Required at closure. |
| `ClosedBy` / `ClosedName` / `ClosedAt` | int64/string/int64 | Cleared again on reopen, so an open case never carries a stale closure. |
| `UpdatedAt`    | int64  | Touched by any edit, including adding or annotating evidence (`idx:"status_updated"`); the default listing is "open cases, most recently touched first". |

## Why the table is `case_file` and not `case`

The bootstrapper derives the table name from the struct name (`strcase.ToSnake`), and `CASE`
is a reserved word in SQL on every engine this suite runs on. A struct called `Case` produces
DDL that fails to parse on sqlite and MariaDB and does something surprising on Postgres.
Renaming here is free; discovering it in a customer's migration is not. (The same trap bit
mypintusan's `grant` table.)

## Why the actor names are denormalised

`AssignedName`, `OpenedName` and `ClosedName` are copied onto the row rather than joined at
read time. The record of who was handling an investigation has to survive that person's
account being deleted — a join that renders "user 7" after an offboarding is not a record of
anything. Same reasoning as the audit trail's `ActorEmail`.

## Related

- `apps/mymatasan/entities/case_item.go.md` — the evidence inside a case.
- `apps/mymatasan/services/case_hold.go.md` — why an OPEN case keeps its footage alive.
- `apps/mymatasan/services/case_file.go.md` — the service.
