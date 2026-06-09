# Module: apps/mymatasan/entities/runtime_setting.go

## Purpose

Defines the persisted runtime settings entity for `mymatasan`.

## Responsibilities

- Store one keyed JSON settings payload.
- Allow bootstrap to create and migrate the `runtime_setting` table.
- Provide a unique `key` index so the app can keep a single `runtime` settings row.

## Notes

- The current payload contains Decoder and Live Stream settings.
- Config JSON is used as a startup/default seed, while this table is the runtime source of truth.
