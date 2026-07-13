# Module: infra/config/app_config.go

## Purpose

`LoadAppConfiguration` reads `config.json`/`config.dev.json` into the shared
`AppConfigModel` and retains the raw document bytes, so an app that owns config blocks of
its own (see `config_models.go.md`'s "Per-app config seam") can decode them straight out of
the same file the host already read.

## Responsibilities

- `LoadAppConfiguration(file string) (*AppConfigModel, error)`:
  - Reads the whole file with `os.ReadFile` (previously streamed via `json.NewDecoder` over
    an open `*os.File`).
  - `json.Unmarshal`s it into a fresh `AppConfigModel`.
  - Stores the raw bytes on the model's unexported `raw` field.
  - Returns the decode error to the caller.
- `(*AppConfigModel) Raw() []byte` — returns the raw document the model was decoded from;
  `nil`-safe (a `nil` receiver returns `nil`) and `nil` for a model not built by
  `LoadAppConfiguration` (e.g. constructed directly in a test), in which case an app's own
  `Load` sees an empty document and decodes to its zero-value defaults.

## Notes

- **Bug fix: a malformed config is now an error.** This previously called
  `jsonParser.Decode(&config)` and discarded the returned error. A single trailing comma (or
  any other syntax error) in `config.json` silently produced an all-zero `AppConfigModel` —
  the app booted on defaults, ignored everything the operator had configured, and logged
  nothing about it. `LoadAppConfiguration` now returns the error, and `infra/apphost/run.go`
  (`loadConfig`) fails startup on it rather than continuing with defaults.
- `Raw()` is the seam the per-app config decode is built on: `infra/apphost/run.go` calls
  `appConfig.Raw()` and hands it to `app.(apphost.AppConfigDecoder).DecodeAppConfig(raw,
  dataDir)` after the shared config is loaded and normalized. See
  `infra/apphost/types.go.md` and `docs/modules/apps/mymatasan/config/config.go.md`.
- No config file format changed. The raw bytes are the exact same document
  `AppConfigModel`'s own JSON tags are unmarshalled from; a block an app owns and a block
  this model owns can sit side by side at the top level, and both are decoded from identical
  bytes.
