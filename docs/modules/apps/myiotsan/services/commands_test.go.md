# Module: apps/myiotsan/services/commands_test.go

## Purpose

Pins the pure, unit-testable half of actuation's safety gates — bounds validation and payload
rendering — down to the byte. The database-dependent half (rate limiting, audit rows, the twin,
`SweepUnconfirmed`) is exercised live instead; see `commands.go.md`'s "verified live" note.

## Responsibilities

- `TestCommand_SwitchTakesOnlyZeroOrOne` — a `switch` command accepts exactly `0`/`1`; `2`/`-1`
  are refused.
- `TestCommand_SetpointIsBoundedServerSide` — a `setpoint` inside `Min..Max` (inclusive of the
  boundary) is accepted; `200` against `5..30` is refused, not clamped, and the error names the
  range (`"5"`/`"30"` both appear) — the refusal has to be actionable: "outside the safe range
  5..30" tells an operator what to do, "bad request" does not.
- `TestCommand_SetpointWithNoDeclaredRangeRefusesEverything` — a `setpoint` with `Min == Max == 0`
  (no declared range) refuses every value tried, including `0` itself. An unbounded setpoint on a
  physical device is an omission, not permission.
- `TestCommand_PayloadTemplateSubstitutesTheValue` / `TestCommand_EmptyTemplateSendsTheBareValue`
  — `{value}` substitution, and the bare-value fallback for a device whose topic IS the
  instruction.
- `TestCommand_ValuesRenderCleanly` — `trimNum` never emits scientific notation or float noise
  (`1`, `0`, `21.5`, `100000`, `0.1`); a relay receiving `"1e+00"` instead of `"1"` does nothing,
  and the operator is left staring at a command that was "sent" and never took effect.

## Notes

- Exercises `validateValue`/`renderPayload`/`trimNum` directly against bare `entities.ProfileCommand`
  values — no `CommandService`, no database.
