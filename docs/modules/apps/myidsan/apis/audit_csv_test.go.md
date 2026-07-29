# Module: apps/myidsan/apis/audit_csv_test.go

## Purpose

Validates `csvSafe` (`apis/audit.go.md`), the spreadsheet-formula-injection defuser applied
to every attacker-influenced column in the audit CSV export.

## Coverage

- `TestCsvSafeDefusesFormulaInjection` — a table of hostile values (`=cmd|'/c calc'!A1`,
  `+1+1`, `-1+1`, `@SUM(1:9)`, a leading tab, a leading CR) must all come back changed and
  prefixed with a leading apostrophe, so a spreadsheet renders them as literal text instead
  of evaluating them as a formula.
- `TestCsvSafeLeavesOrdinaryValuesAlone` — ordinary values (empty string, an email, an
  action name, an IP, a User-Agent string, a JSON blob, a string with an internal space)
  must pass through completely unchanged — quoting everything would corrupt the export for
  the humans reading it.
