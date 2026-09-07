# Module: apps/myseliasan/manual/manual_test.go

## Purpose

Runs the suite-wide manual conformance suite against myseliasan's shipped articles —
the same test shape `apps/mymatasan/manual/manual_test.go`
(`apps/mymatasan/manual/manual_test.go.md`) runs for its own, larger book.

## Behavior

`TestManual(t *testing.T)` calls `manualcheck.Library(t, manual.Library)`
(`domain/shared/manual/manualcheck/check.go.md`), which asserts: every article has the
frontmatter the index and print table of contents need, every language folder holds the
same set of articles as English, every internal cross-link resolves to a real article,
and every hand-written `{#anchor}` heading id is unique and identical across all four
translations.

`TestManualUIReferences(t *testing.T)` calls
`manualcheck.UIReferences(t, manual.Library, "../views/react-webpack/src/views")`
(`domain/shared/manual/manualcheck/uirefs.go.md`) — the opposite direction from
`TestManual`. It walks every `.js` file under the SPA's `views` tree and confirms each
contextual `HelpButton`/`help:`/`TAB_HELP`/`SETUP_STEP_HELP` target actually resolves
against the manual shipped above, currently checking 19 targets. A renamed article or a
dropped `{#anchor}` breaks nothing at build or run time — the "?" button just opens the
wrong page — so this is the only thing that catches it.

`TestManualSpecValues(t *testing.T)` calls `manualcheck.SpecValues` (`domain/shared/manual/
manualcheck/diagrams.go.md`) with a map naming the `adopting-nodes` article's four `` ```spec ``
port rows (`adopting-nodes/discovery`, `adopting-nodes/mtls`, `adopting-nodes/control`,
`adopting-nodes/media`) against `infra/pairing.Default{Discovery,MTLS,Control,Media}Port`
(`infra/pairing/packet.go.md`) — the same four constants `apps/mymatasan/manual/
manual_test.go`'s `TestManualSpecValues` asserts, described here from the control plane's
side of the same handshake rather than the node's. Because both manuals assert against the
same source constants, the two port tables cannot drift from each other, not just from the
software.

## Notes

- Mirrors `apps/mymatasan/manual/manual_test.go` (`apps/mymatasan/manual/manual_test.go.md`),
  the four-language predecessor this suite's tests generalize; both run the identical
  `manualcheck` package against different-sized libraries, and both now call the opt-in
  `SpecValues` helper against the same `infra/pairing` constants.
- This is the test that actually enforces "every article exists in every language" for
  `apps/myseliasan/manual` — see the caution in `manual.go.md`.
- `TestManualUIReferences` is the only automated guard on myseliasan's contextual-help
  wiring; every new `HelpButton` call, `TAB_HELP` entry, or `SETUP_STEP_HELP` anchor list
  update is checked here, not by hand.
- `TestManual`'s call into `manualcheck.Library` also runs the `Diagrams` subtest
  (`domain/shared/manual/manualcheck/diagrams.go.md`) over this app's shipped figures — five
  `` ```flow `` figures (the must-change/no-role gates after sign-in in
  `20-first-sign-in.md`, the two-permission split in `150-node-pages.md`, the
  match-through-fire wait loop in `140-fleet-rules.md`, the staged-camera takeover in
  `160-failover.md`, and the nav-rail/API split in `510-users-and-roles.md`), three
  `` ```arch `` figures (what the control plane talks to in `10-welcome.md`, the
  event-row-stays-here/clip-stays-on-the-node split in `130-notifications.md`, and where an
  answer comes from in `320-ask-the-fleet.md`), and one `` ```seq `` figure (the fleet-key
  and claim-code handshake in `110-adopting-nodes.md`), alongside the `` ```spec `` fleet
  port table in that same article that `TestManualSpecValues` checks the values of —
  reporting "checked 10 figures across 4 languages".
