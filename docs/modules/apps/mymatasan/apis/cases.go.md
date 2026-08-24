# Module: apps/mymatasan/apis/cases.go

## Purpose

The case-file HTTP surface (W3-3), mounted under `/api/cases`.

## Routes

| Method | Path | Notes |
|--------|------|-------|
| GET | `/api/cases` | list; `status`, `assignedTo`, `limit`, `offset` |
| POST | `/api/cases` | open a case |
| GET | `/api/cases/{id}` | the case, its evidence, and what it is holding |
| POST | `/api/cases/{id}` | retitle / re-summarize / reassign |
| DELETE | `/api/cases/{id}` | remove it entirely — administrator only, by verb |
| POST | `/api/cases/{id}/close` | close it; an outcome is required |
| POST | `/api/cases/{id}/reopen` | reopen it |
| POST | `/api/cases/{id}/items` | add evidence |
| POST | `/api/cases/{id}/items/{itemId}` | annotate evidence |
| POST | `/api/cases/{id}/items/{itemId}/remove` | take evidence back out |
| POST | `/api/cases/{id}/export` | build the case bundle; a reason is required |
| GET | `/api/cases/exports/{exportId}` | bundle status |
| GET | `/api/cases/exports/{exportId}/download` | the `.zip` |
| GET | `/api/cases/assignees` | ids and display names a case can be handed to |

## Every mutation is a POST

Including the ones REST would spell PUT or DELETE. The appliance role model grants verbs, and
its grantable "use" rung is GET+POST precisely so that DELETE stays an administrator's verb
everywhere. Spelling "remove this item" as DELETE would make it ungrantable to the operator
who does the work; widening the rung to include DELETE would hand out the verb that destroys
footage. The one genuine DELETE is deleting the case itself, which is exactly the act that
should need an administrator.

## Why the export routes live here

Under `/api/cases`, not `/api/evidence`, so the Cases page grant alone is enough to work a
case end to end. A role granted Cases but not Recordings would otherwise be able to assemble a
bundle and not collect it. `exportStatus` and `exportDownload` both refuse a job whose
`CaseId` is 0: that check is authorization, not tidiness — without it this route would serve
any single-clip bundle whose id was known.

## Why `/api/cases/assignees` exists

Assignment is part of working a case, and `/api/settings/users` is administrator-only: an
operator who could not read it could open a case and never hand it to anybody. This answers
with ids and display names ONLY — none of the account surface that makes the settings route
administrative.

## Auditing

Every mutation is audited against `TargetCase`. Two choices worth knowing:

- **Reassignment is its own action** (`case.assign`, not `case.update`). "Who was this handed
  to, and when" is a question asked on its own; a trail that buries it inside a generic update
  cannot answer it.
- **Exporting records `recording.export`, not a case-specific action.** "What footage left this
  appliance" has to be answerable by filtering on one action; a separate `case.export` would
  put half the evidence handling outside the filter every auditor uses. It is recorded twice —
  at request time (deciding to take a copy out is the auditable act, and a build that later
  fails must still leave a record that somebody asked) and again on download (when the footage
  actually leaves).
- **Closing records what it released**, read BEFORE the close: afterwards the answer is zero,
  which is the least useful possible thing to write down about that decision.

## Related

- `apps/mymatasan/services/case_file.go.md`
- `apps/mymatasan/services/case_export.go.md`
- `apps/mymatasan/services/pages.go.md`
