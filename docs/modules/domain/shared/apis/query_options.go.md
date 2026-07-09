# Module: domain/shared/apis/query_options.go

## Purpose

Parses reusable list endpoint query options into SQL repository filter and sorter enums.

## Behavior

- Reads `limit` and `offset` query parameters for paging.
- Reads `filters` or repeated `filter` query parameters as JSON filter object/array.
- Reads `sorters` or repeated `sorter` query parameters as JSON sorter object/array.
- Accepts public entity field names from `json`, `query`, `form`, lower-camel, snake-case, or Go struct names.
- Maps accepted field names back to Go struct field names before passing them to services.
- Validates compare enum values `1..7` (`7` = `In`, a multi-value membership filter) and sorter enum values `1..2`.
- Coerces filter values to the target entity field type before repository use, including `float32`/`float64` fields (accepts `json.Number`, numeric strings, or a raw float64).
- For `compare: 7` (`In`), `value` is a JSON array (a single scalar is treated as a one-element list); each element is normalized to the field type the same way a scalar filter value is (`normalizeQueryValueList`), blank/empty elements are dropped, and an empty resulting list drops the filter entirely rather than matching nothing.
- Exposes the parser for app-local APIs that follow the same shared list contract.

## Query Contract

Filter JSON shape:

```json
{"fieldName":"createdAt","compare":5,"value":1700000000}
```

Multi-value (`In`) filter shape:

```json
{"fieldName":"label","compare":7,"value":["person","car"]}
```

Sorter JSON shape:

```json
{"fieldName":"createdAt","sort":2}
```

Arrays use the same object shape. Multiple filters are combined by the SQL repository with `AND`; multiple sorters are applied in request order.
