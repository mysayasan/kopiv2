# Module: infra/vision/schedule.go

## Purpose

Evaluates rule-level schedule policies for reusable visual detection rules.

## Responsibilities

- Validate empty, weekly-window, and absolute date-range schedule policies.
- Support `allow` mode for rules that are active only inside matching windows or ranges.
- Support `deny` mode for rules that are active except inside matching windows or ranges.
- Resolve optional IANA timezone names, with empty or `local` falling back to the process local timezone.
- Match weekly windows by day and `HH:MM` start/end time.
- Support overnight weekly windows by checking the current and previous weekday as needed.
- Match RFC3339 date ranges.
- Return both active state and a reason string for diagnostics.

## Notes

- Empty `schedulePolicy` means the rule is always active.
- `preset` is optional UI metadata and does not affect backend evaluation.
- Valid day values include short and long weekday names such as `mon`, `monday`, `sat`, and `saturday`.
