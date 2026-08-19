# Module: apps/mymatasan/apis/evidence.go

## Purpose

The HTTP surface for evidence export: turning a span of footage into a verifiable `.zip` bundle that can be handed to somebody outside the product. See `services/evidence_export.go.md` for what a bundle contains and why (required reason, honest gap reporting, labelled digest strength, no re-encoding).

## Endpoints

Mounted under `/evidence` on the protected router, governed by the role permission matrix (`services/rbac.go` grants `/api/evidence` to operator and up; `services/pages.go`'s `PageRecordings` gained a second, non-admin-only level (`use`) that grants it):

| Method | Path | Description |
|---|---|---|
| `POST` | `/api/evidence/preview` | What an export **would** contain, gaps included. A separate call from `create` on purpose: an operator must be able to see the holes in a range before producing the bundle, not after. |
| `POST` | `/api/evidence/exports` | Start building a bundle. Body: `{cameraId, from, to, reason}`; `reason` is required — `Create` refuses an empty one. |
| `GET` | `/api/evidence/exports/{id}` | Job status, and the manifest once the build is ready. |
| `GET` | `/api/evidence/exports/{id}/download` | Streams the `.zip`. Sets `X-Evidence-Output-Sha256` and `X-Evidence-Gaps` from the manifest so a careful recipient can check the digest without unzipping. |

Building is asynchronous (`create` returns a job immediately) because a long range takes minutes to decrypt and join, and a request-scoped job would be abandoned the moment the operator's browser navigated away.

## Auditing

Both the request and the download are audited, deliberately as two separate entries (`services.ActionRecordingExport`, target `recording`, target id the camera id):

- **At `create`**, success or failure, because deciding to take a copy of footage out of the system is the auditable act, and a build that later fails must still leave a record that somebody asked for it. The stated reason and gap-warning flag are carried in the entry's metadata.
- **At `download`**, because requesting a bundle and collecting it can be minutes — or a shift — apart, and "downloaded evidence bundle X" is a distinct, later fact from "requested an export".

## Notes

- `NewEvidenceApi(router, serv services.IEvidenceExportService, audit *Auditor)` — the `Auditor` is the same value every other audited handler in this app holds (`apis/audit.go.md`); a nil one records nothing rather than panicking.
- `preview` and `status` are not audited — they read, they never commit to producing or releasing a copy of footage.
- `download` opens the bundle file directly and serves it with `http.ServeContent`; a bundle that has expired or was never built returns a `400` ("that export is not ready"/"is no longer available") rather than a `404`, since the id itself may still be valid.
- **Not yet live-benched.** See `docs/FLAGSHIP_BENCH_CHECKLIST.md` (W1-4).
