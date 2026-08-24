# Module: apps/mymatasan/entities/case_item.go

## Purpose

Declares `CaseItem`: one piece of evidence inside a `CaseFile` (W3-3) — a span of footage, a
recorded sighting, an alert, or a written note.

## Fields

| Field          | Type   | Notes |
|----------------|--------|-------|
| `Id`           | int64  | Auto-increment primary key. |
| `CaseId`       | int64  | Owning case (`idx:"case_time"` with `StartedAt`). |
| `Kind`         | string | `footage` \| `sighting` \| `alert` \| `note`. |
| `CameraId`     | int64  | The camera the evidence is on, 0 for a note (`idx:"cam_time"` with `StartedAt`). |
| `StartedAt` / `EndedAt` | int64 | The span, unix seconds. Indexed for both the case read and the hold query. |
| `Label`        | string | Short caption — the object class, the rule name, whatever the operator typed. |
| `Note`         | string | The operator's annotation. |
| `SourceId`     | int64  | The observation/alert row this came from, or 0. Provenance only. |
| `SnapshotPath` | string | A still belonging to the evidence (an alert's snapshot), or empty. |
| `AddedBy` / `AddedName` / `AddedAt` | int64/string/int64 | Who added it. |
| `UpdatedAt`    | int64  | Last annotation. |

## Every kind is a span on a camera

A sighting, an alert and a hand-made bookmark differ in where they came from, not in what
they are: each names a camera and a moment, and each resolves to footage through the same
rule. One row shape means the case screen, the footage hold and the export bundle each handle
ONE thing instead of three, and a fourth source added later (a plate hit, a face match) is a
new `Kind` and nothing else.

A note is the exception that proves it — `CameraId` 0 and a zero span, carrying only text. It
is the same row because a note in the middle of a timeline of evidence has to sort WITH the
evidence, not live in a separate list beside it. The service zeroes a note's camera and span
rather than trusting the caller, or a note with a stray camera would pin footage nobody meant
to keep.

## The span is copied, not referenced

`StartedAt`/`EndedAt` live on the item even when `SourceId` names an observation or alert that
has its own timestamps. Retention deletes observations and alerts on their own schedule; if
the case had to read the source row to learn what footage to protect, the hold would silently
release the moment that index row expired — while the case still said the sighting was
evidence. The case is the record, so the case holds the facts.

## `HoldsFootage()`

Reports whether the item points at video that must survive retention while its case is open:
not a note, a real camera, and an end after its start. It is the single predicate every hold
path uses, so "what counts as held" is defined once.

## Related

- `apps/mymatasan/entities/case_file.go.md`
- `apps/mymatasan/services/case_hold.go.md`
