# Module: domain/shared/manual/manualcheck/diagrams.go

## Purpose

The build guard for the manual's drawn figures (`domain/shared/manual/diagram`,
`diagram/diagram.go.md`). Wired into `Library` (`check.go`, `check.go.md`) as its fifth
subtest, `Diagrams`. A wrong diagram is worse than a missing one — it reads as authority,
and it is the part of a page nobody proofreads in a language they do not speak — so this
catches the three ways a figure rots silently in production: a fence the renderer cannot
draw, a translation that lost an edge, and a node linking to an article that no longer
exists.

## Key Function: DiagramProblems

```go
func DiagramProblems(lib *manual.Library, langs []string) (problems []string, checked int)
```

The guard as a plain function rather than one welded to `*testing.T`, so it is itself
testable (`diagrams_test.go`) without deliberately failing a test to exercise it — a drift
guard nobody has ever watched catch anything is indistinguishable from one that quietly
stopped working. `checked` is the count of figures parsed in English; a check that reports
"0 figures" is failing at its own job the same way a `0` from `Size()` would for
`retrieval.Corpus`.

For every English article, parses each fence with `diagram.Parse` and resolves every
`=> slug#anchor` link against the article set and hand-written anchors (`known`), exactly
as a contextual `?` button is resolved. For every other configured language, does the same
and then asserts the language's figures have the **same fingerprint**, in the **same
order**, as the English originals (`compare`) — a count mismatch means a diagram was added
or dropped in translation; a fingerprint mismatch means one changed shape.

`diagrams(t *testing.T, lib, langs)` is the `testing.T`-bound wrapper `Library` actually
calls; it reports each problem with `t.Error` and logs the figure/language counts.

## Key Function: SpecValues

```go
func SpecValues(t *testing.T, lib *manual.Library, want map[string]string)
```

Separate from `Library` because it needs values only the calling **app** can name from its
own source — it is opt-in, called from an app's own manual test alongside
`manualcheck.Library`:

```go
manualcheck.SpecValues(t, manual.Library, map[string]string{
    "fleet-ports/discovery": fmt.Sprintf("%d/udp", pairing.DefaultPort),
    "fleet-ports/control":   fmt.Sprintf("%d/tcp", fleetnode.DefaultMTLSPort),
})
```

The key is `"<article slug>/<spec row id>"`. Only English `` ```spec `` rows are read (the
other languages are already held to English by `Diagrams`'s fingerprint check, which treats
a spec value as structure). A key that resolves to no row fails loudly rather than silently
covering nothing — the same "a check that quietly covers nothing is worse than no check"
principle `TestManualUIReferences` (`uirefs.go.md`) applies to a zero-reference walk. A
value mismatch names the row, what the manual says, and what the software actually uses.

This is the check that turns a `` ```spec `` reference table from documentation into an
assertion: without it, a manual stating a wrong port costs an integrator an afternoon,
where no reference section at all would have cost them a support call instead — i.e.
shipping a reference section with no `SpecValues` behind it makes the product worse than
shipping none.

## Notes

- `checkLinks` reuses the same target-resolution rule `check.go`'s `Links`/`Anchors` checks
  apply to markdown links, so a figure's `=> slug#anchor` and a contextual `?` button can
  never disagree about what counts as a valid target.
- `structureDiff`/`nodeKeys`/`edgeKeys` turn a fingerprint mismatch into a readable
  missing/unexpected node-or-edge list rather than two opaque hashes — the translator
  reading the failure is the person least equipped to work out what "the fingerprints
  differ" means.
- As of this writing no app calls `SpecValues` yet; the manual ships no `` ```spec `` content
  yet either. Both land in a follow-up once an integrator-facing reference section is
  authored.
- Author-facing guide: `docs/MANUAL_DIAGRAMS.md`.
