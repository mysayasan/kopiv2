# Module: domain/shared/apis/query_options.go

## Purpose

Parses reusable list endpoint query options into SQL repository filter and sorter enums.

## Behavior

- Reads `limit` and `offset` query parameters for paging.
- Reads `filters` or repeated `filter` query parameters as JSON filter object/array.
- Reads `sorters` or repeated `sorter` query parameters as JSON sorter object/array.
- Accepts public entity field names from `json`, `query`, `form`, lower-camel, snake-case, or Go struct names.
- Maps accepted field names back to Go struct field names before passing them to services.
- Validates compare enum values `1..6` and sorter enum values `1..2`.
- Coerces filter values to the target entity field type before repository use.
- Exposes the parser for app-local APIs that follow the same shared list contract.

## Query Contract

Filter JSON shape:

```json
{"fieldName":"createdAt","compare":5,"value":1700000000}
```

Sorter JSON shape:

```json
{"fieldName":"createdAt","sort":2}
```

Arrays use the same object shape. Multiple filters are combined by the SQL repository with `AND`; multiple sorters are applied in request order.
