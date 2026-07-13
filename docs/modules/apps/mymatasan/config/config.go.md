# Module: apps/mymatasan/config/config.go

## Purpose

`mymatasan`'s **own** half of `config.json`: the `camera`, `decoder`, `stream`, `vision`,
`health`, and `recording` blocks. Introduced in Tier 2 phase C (the per-app config seam,
`docs/MYMATASAN_TIER2_PLAN.md`) to get these six blocks — dead weight for `myidsan` and
`myseliasan` — and their `ResolveWritablePath` normalization out of the shared
`infra/config.AppConfigModel` and `infra/apphost`, which is what blocked adding a fourth app
(`docs/MYIOTSAN_PLAN.md`).

Package name is `config`, imported as `mmconfig` everywhere it's consumed (`apps/mymatasan/app`)
to avoid colliding with `infra/config`.

## Responsibilities

- `type Config struct` — `Camera` (inline `FFmpegPath`), `Decoder DecoderConfigModel`,
  `Stream StreamConfigModel`, `Vision VisionConfigModel`, `Health HealthConfigModel`,
  `Recording RecordingConfigModel`.
- `Load(raw []byte) (*Config, error)` — decodes these blocks out of the same raw config
  document `infra/config.LoadAppConfiguration` already parsed (`AppConfigModel.Raw()`).
  Empty/nil input returns a zero-value `Config`, no error (a legitimate "no app config"
  case). A malformed document is an **error** — see Notes.
- `(*Config) Normalize(dataDir string)` — resolves this app's data-relative paths against
  the writable data dir:
  - `Vision.SnapshotDir` — the recordings/snapshots root. Empty defaults to `"recordings"`
    before resolving, so an unset value still lands under `dataDir` rather than the process
    working directory (a packaged Windows service runs with `CWD=C:\Windows\System32`).
  - `Vision.Training.DataDir` — the YOLO training root. Left empty (not defaulted) when
    unset, so callers can distinguish "not configured" from "configured to the data dir
    itself"; `apps/mymatasan/app/config_map.go`'s `trainingDataDir` supplies the actual
    default (a `training` sibling of the snapshot dir).
  - Both go through `apphost.ResolveWritablePath`, which is upgrade-safe: if the resolved
    `dataDir` target doesn't exist yet but a copy is found at the pre-packaging legacy
    (CWD-relative) location, the legacy path is returned instead, so an upgrade never
    orphans in-place recordings.
- Model types: `DecoderConfigModel` (`BrowseRoots`, `MJPEG`, `FFmpeg`), `StreamConfigModel`
  (`WebRTC`, `MJPEGFallback`), `WebRTCConfigModel` (embeds shared
  `sharedconfig.WebRTCICEServerModel` for `ICEServers`), `MJPEGFallbackConfigModel`,
  `HealthConfigModel`, `RecordingConfigModel` (`Shred`, `Storage`), `VisionConfigModel`
  (`Detector`, `Training`, retention/purge fields), `VisionTrainingConfigModel`,
  `VisionDetectorConfigModel`. All field-for-field identical (JSON tags included) to the
  types they replaced in `infra/config.config_models.go` — this is a pure relocation, not a
  schema change.

## Notes

- **No config file format change.** The blocks are NOT nested under an `"app"` key — they
  stay exactly where they were, at the top level of `config.json`. `Load` decodes them out
  of the identical bytes `infra/config.AppConfigModel` decodes its own blocks from
  (`AppConfigModel.Raw()`). No deployed `config.json` needs to change.
- **Bug fix carried over from `infra/config.LoadAppConfiguration`**: a malformed document is
  now a hard error from `Load`, and `(*module) DecodeAppConfig`
  (`apps/mymatasan/app/app.go.md`) propagates it to abort startup. Previously (via the
  now-fixed shared loader) a JSON syntax error silently produced an all-zero config: the app
  booted on defaults and gave no indication anything was wrong.
- **Where the seam line is** (a judgement call, not a mechanical rule): what moved here is
  only what nothing else in the codebase reads — confirmed by grep, and by the fact that
  when the 11 `*FromAppConfig` mappers in `apps/mymatasan/app/config_map.go` were reclassified,
  none needed both a shared-model field and an app-config field, a clean split. What stayed
  in `infra/config.AppConfigModel`: `Security` (encryption-at-rest — `myseliasan` also uses
  it), `Pairing`/`NodeStream` (the fleet), `Notification`, `LoginSecurity`, and every infra
  block. `WebRTCICEServerModel` specifically stayed shared (not moved here) because
  `NodeStreamConfigModel` — the fleet media relay's ICE server list, shared infra — also
  uses it.
- Called from `apps/mymatasan/app/app.go`'s `(*module) DecodeAppConfig`, which implements
  `apphost.AppConfigDecoder` (`infra/apphost/types.go.md`): `infra/apphost/run.go` calls it
  once, after the shared config is loaded and normalized and before any route is registered;
  an error there aborts startup.
- Depends on `infra/apphost` (for `ResolveWritablePath`) and `infra/config` (for
  `sharedconfig.WebRTCICEServerModel`) — the app config package is a consumer of both, never
  the other way around.

## Tests (`config_test.go`)

Six tests, all pure (no filesystem beyond `t.TempDir()` for `Normalize`):

- `TestLoad_DecodesTopLevelBlocksFromTheSharedDocument` — feeds a realistic slice of
  `config.json` including blocks owned by other owners (`server`, `db`, `security`) and
  asserts every mymatasan block decodes correctly and the other owners' blocks are ignored,
  not tripped over. This is the test that would fail if the seam were ever changed to a
  nested `"app"` key — a reminder that doing so breaks every deployed config file.
- `TestLoad_MalformedConfigIsAnError` — a syntactically broken document must fail `Load`,
  not silently zero out.
- `TestLoad_EmptyAndAbsentAreZeroValues` — `nil`, `{}`, and a document with only other
  owners' blocks all decode to a legitimate zero-value `Config`.
- `TestNormalize_ResolvesAppPathsAgainstDataDir` — both `Vision.SnapshotDir` and
  `Vision.Training.DataDir` resolve under a given `dataDir`.
- `TestNormalize_UnsetSnapshotDirDefaultsUnderDataDir` — an unset `snapshotDir` still lands
  under `dataDir`, not the process working directory.
- `TestNormalize_EmptyTrainingDirStaysEmpty` — an unset training dir stays empty rather than
  silently becoming the data dir itself.
