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
`manualcheck.Library`. `mymatasan` was the first caller, in
`apps/mymatasan/manual/manual_test.go`'s `TestManualSpecValues`, asserting the
`control-plane` article's port table against the four exported constants in
`infra/pairing/packet.go` (`infra/pairing/packet.go.md`). `myseliasan` calls it the same
way, from the other side of the same handshake — see below.

```go
manualcheck.SpecValues(t, manual.Library, map[string]string{
    "control-plane/discovery": fmt.Sprintf("%d/udp", pairing.DefaultDiscoveryPort),
    "control-plane/mtls":      fmt.Sprintf("%d/tcp", pairing.DefaultMTLSPort),
    "control-plane/control":   fmt.Sprintf("%d/tcp", pairing.DefaultControlPort),
    "control-plane/media":     fmt.Sprintf("%d/tcp", pairing.DefaultMediaPort),
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
- `mymatasan`'s manual now ships four figures using this grammar: two `` ```flow `` (the
  detection chain in `310-how-detection-works.md`, the "no alerts" checklist in
  `910-troubleshooting.md`) and one `` ```seq `` (the adoption handshake) plus the one
  `` ```spec `` (the fleet port table) in `570-control-plane.md`, byte-identical in
  structure across `en`/`ms`/`zh`/`ar`. `Diagrams` reports "checked 4 figures across 4
  languages" for that app's suite.
- `myseliasan`'s manual now ships ten figures: five `` ```flow `` (the must-change/no-role
  gates after sign-in in `20-first-sign-in.md`, the two-permission split in
  `150-node-pages.md` (`{#authorization}` — control plane decides *reach*, the node decides
  *do*, and a node refusal means nothing was sent), the match-through-fire wait loop in
  `140-fleet-rules.md`, the staged-camera takeover in `160-failover.md` (`{#takeover}`), and
  the nav-rail/API split in `510-users-and-roles.md`), three `` ```arch `` (what the control
  plane talks to in `10-welcome.md`, the event-row-stays-here/clip-stays-on-the-node split in
  `130-notifications.md`, and where an answer comes from in `320-ask-the-fleet.md`), one
  `` ```seq `` (the fleet-key and claim-code handshake in `110-adopting-nodes.md`), and the
  one `` ```spec `` (the same four fleet ports, described from the control plane's side) in
  that same article (`{#ports}`), byte-identical in structure across `en`/`ms`/`zh`/`ar`.
  `Diagrams` reports "checked 10 figures across 4 languages" for that app's suite. Both
  apps' `SpecValues` calls assert the **same** `pairing.Default*Port` constants, so the two
  manuals' port tables cannot drift from each other either.
- Author-facing guide: `docs/MANUAL_DIAGRAMS.md`.
