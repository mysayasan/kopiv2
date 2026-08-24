# Module: apps/mymatasan/services/case_file.go

## Purpose

`caseService` / `ICaseService`: the read and write side of case files (W3-3) — the
investigation container that footage, sightings and notes are collected into.

Most of this file is unremarkable CRUD. It exists for three things a folder of downloaded
clips cannot do:

1. **It holds its footage** (`case_hold.go.md`). Adding footage to an OPEN case takes it out
   of reach of retention, the per-camera "Purge now", and the disk-pressure sweeper.
2. **It exports as one bundle** (`case_export.go.md`) — every clip, one manifest, the
   digests, and the case's own audit trail as a chain of custody.
3. **It is assignable and closed with a stated outcome**, so "what came of that?" has an
   answer that outlives the person who was on shift.

## Reads that matter

- `List` returns cases newest-touched-first with item counts computed from the item table in
  ONE query for the whole page, not a count per row. The counts are never stored on the case:
  a denormalised counter eventually disagrees with the thing it counts, and this one would
  disagree on a screen an auditor is reading. Notes and footage are counted separately —
  "3 items" on a case of three notes would tell an operator there is evidence to export.
- `Get` resolves each item to a playable `(segment, seek)` through
  `ObservationService.ResolveFootageFor` → `pickCovering`, the SAME rule the Objects grid and
  appearance search use, so a clip opened from a case is the clip opened from anywhere else.
  It also sets `FootageMissing` on evidence whose video is gone — a fact the screen must show,
  because an item that silently refuses to play reads as a broken player rather than as
  missing evidence.
- `itemsForCases` uses a filter + `Get`, deliberately NOT `GetByForeign`: that returns a
  SINGLE row, which reads as "this case has one item" and is wrong in exactly the direction
  that drops evidence from an export.

## Rules the tests pin

| Rule | Why |
|------|-----|
| A case needs a title | An untitled case is one nobody else can pick up. |
| Closing requires an outcome | Closing is the act that releases every footage hold; "why" is the only part of that decision anybody can review later. |
| Reopening CLEARS the closure | Leaving `ClosedBy`/`ClosedAt` on an open case makes every later read ambiguous. The audit trail remembers it was once closed. |
| A closed case refuses new or edited evidence | Reopen first — an edit to a closed case is a change to a finished record. |
| Evidence needs a camera and a span that ends after it starts | A zero-length instant cannot be exported and cannot be held. |
| A note's camera and span are zeroed | Or a note created with a stray camera pins footage nobody meant to keep. |
| Duplicate evidence is refused | The likeliest mistake on a screen with an "add to case" button on every row, and a duplicated clip in a bundle is somebody else's job to explain. |
| An item is only reachable through its own case | Item ids are global; without the check, one case's URL edits another's evidence. |
| Deleting a case deletes its items FIRST | An orphaned item still answers the hold query — footage held forever by a case nobody can find or release. |

## Removing evidence, and why an operator may

`RemoveItem` releases the item's footage hold. It does not delete anything: the footage
returns to the retention policy it would have had if the case had never existed. The hold only
ever EXTENDS footage past its normal life, so declining to extend it is not destroying
evidence — which is what makes this safe to grant to the operator role, given that role's
defining rule (an operator who was present at an incident must never be able to destroy the
record of it). It is audited with the span it releases all the same, because the case that
matters is the one where the removal happened the day before the retention sweep.

## Caps

`caseMaxItems` (100) and `caseItemMaxSpanSeconds` (4h) are product limits, not storage ones:
an export of a hundred clips is a haystack rather than evidence, and an item covering a week
is a request to export a week.

## Related

- `apps/mymatasan/services/case_hold.go.md`
- `apps/mymatasan/services/case_export.go.md`
- `apps/mymatasan/apis/cases.go.md`
