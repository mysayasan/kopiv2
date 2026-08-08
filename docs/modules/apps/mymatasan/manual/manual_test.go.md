# Module: apps/mymatasan/manual/manual_test.go

## Purpose

Runs the suite-wide manual conformance suite against mymatasan's shipped articles.

## Behavior

`TestManual(t *testing.T)` calls `manualcheck.Library(t, manual.Library)`
(`domain/shared/manual/manualcheck/check.go.md`), which asserts: every article has the
frontmatter the index and print table of contents need, every language folder holds the
same set of articles as English, every internal cross-link resolves to a real article, and
every hand-written `{#anchor}` heading id is unique and identical across all four
translations.

## Notes

- Mirrors `apps/myiotsan/kb/kb_test.go` (`apps/myiotsan/kb/kb_test.go.md`), the
  single-language predecessor this suite's tests generalize.
- This is the test that actually enforces "every article exists in every language" for
  `apps/mymatasan/manual` — see the caution in `manual.go.md`.
