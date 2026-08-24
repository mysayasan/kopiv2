# Module: apps/mymatasan/services/case_export.go

## Purpose

Exporting a whole case file as one verifiable bundle (W3-3). `CreateCase` on
`IEvidenceExportService`, plus the case manifest and the verification note.

The single-clip export (`evidence_export.go.md`) answers "give me camera 3 from 14:05 to
14:40". A case answers the question the single clip cannot — "give me what happened" — which
is four clips off three cameras, the operator's notes about why each one matters, and the
record of who assembled it. Handing that over as four separate downloads plus a verbal
explanation is exactly the handover this replaces.

## It reuses the single-clip machinery whole

Each item goes through the same `plan()` (segment resolution, merged-interval coverage maths,
the mandatory gap list, the refusal on a digest mismatch) and the same
`materialize`/`concat`. A second implementation of "what footage covers this range" would
eventually disagree with the first, and the two answers would be about evidence.

## Bundle layout

```
clips/001_camera3_20260824T140500Z-141000Z.mp4   one file per piece of footage
manifest.json                                     CaseManifest
chain-of-custody.csv                              the case's audit entries, in order
VERIFY.txt                                        how to check all of it
```

`CaseManifest` carries the case (title, status, who opened/closed it, the outcome), one
`CaseClipEntry` per item — each with its own full single-clip `ExportManifest` under
`evidence`, so per-source digests, `hashOrigin` and gaps survive into the case bundle — the
notes, the custody list, and totals. `Clips` and `Notes` are never omitted and encode as `[]`,
the same rule the single-clip manifest's gap list follows: a missing field reads as "did not
look".

## A missing clip does not fail the bundle

If an item's footage is gone (added after the fact, destroyed before the case existed) the
bundle is still produced and that clip is recorded as **missing, with the reason**, in the
manifest and in `VERIFY.txt`. Refusing would hand back nothing for an investigation that is
mostly intact; omitting it silently would produce a bundle that looks complete. Saying so is
the only honest option.

The line is drawn at whether the problem is about the FOOTAGE or about the APPLIANCE: an
unreadable source or a failed integrity check marks that clip missing, while a `concat`
failure (ffmpeg absent, say) fails the whole job — it would fail every clip identically, and
a bundle full of "missing" entries would blame the footage for a configuration problem.

## Chain of custody

Assembled by the API layer (`apis/cases.go.md`) from the case's own audit entries, oldest
first, and shipped as both JSON and CSV — CSV because the chain of custody is the part
somebody opens in a spreadsheet, prints and attaches to a report. When the trail could not be
read, or was longer than one bundle carries, `CustodyNote` says so: an empty custody list must
never be presented as "nothing happened".

## Housekeeping

Clips are zipped in sorted name order, so two exports of the same case do not differ for no
reason. The loose decrypted clips are removed the moment the bundle exists — leaving them
beside the encrypted recordings is what the at-rest encryption exists to prevent — and the
bundle itself is cleaned up by the existing `scheduleCleanup` after `exportRetention`.

## Related

- `apps/mymatasan/services/evidence_export.go.md`
- `apps/mymatasan/apis/cases.go.md`
