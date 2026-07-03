# Module: apps/mymatasan/services/talk.go

## Purpose

Implements the `ICameraService` talk-back (two-way audio) capability methods: `TalkCapability`, `SaveTalkPassword`, and `OpenTalkSession`, resolving between the ONVIF RTSP backchannel and the TP-Link Tapo/VIGI port-8800 transport.

## Responsibilities

- `TalkCapability(ctx, id) TalkCapabilityResult` — returns the cached capability for a camera, re-probing (`resolveTalkCapability`) when the 10-minute TTL (`talkCapabilityCacheTTL`) has expired. Cache is keyed by camera id (`cameraService.talkCapById`, guarded by `talkCapMu`), mirroring the `LPRCapability` cache pattern. `HasPassword` is always recomputed live off the stored camera row (`talkPasswordStored`), even on a cache hit, so a just-saved password shows immediately without waiting out the TTL.
- `resolveTalkCapability(ctx, id)` — loads the camera detail. Prefers the ONVIF backchannel: if the camera has a stored RTSP URL, probes it with `infra/talk.HasBackchannel` (8s timeout) and returns `{Supported: true, Transport: "onvif"}` on a hit (no extra password — reuses stored RTSP credentials). Otherwise falls back to `infra/talk.Probe8800` (8s timeout) against the camera's host; on a genuine TP-Link "Streamd" fingerprint match it returns `{Supported: true, Transport: "tapo"|"vigi", NeedsPassword: !probe.NoneAuth}` (`"vigi"` when `isVigiCamera` detects a VIGI model, else `"tapo"`). Returns `{Transport: "none", Detail: "..."}` when neither probe succeeds.
- `isVigiCamera(detail)` — sniffs manufacturer/model/hardware-id/ONVIF scopes for "vigi" (case-insensitive) to pick the VIGI credential path (admin password) over the default Tapo path (cloud password).
- `talkPasswordStored(ctx, id)` — reports whether a non-empty `TalkPassword` is currently saved for the camera, read straight from the repo (not cached).
- `SaveTalkPassword(ctx, id, password)` — trims and stores the speaker/cloud password on the camera's detail (`Camera.TalkPassword`) and invalidates the cached capability for that id so the next `TalkCapability` call re-resolves `HasPassword`. Errors when the camera is not found.
- `OpenTalkSession(ctx, id) (talk.Session, error)` — loads the camera detail, re-checks `TalkCapability` (cheap, cache-hit in the common case), and dials the resolved transport: `"onvif"` → `infra/talk.DialONVIF` with the credentialed RTSP URI; `"tapo"` → `infra/talk.DialTapo` with `BrandTapo` and the stored `TalkPassword` (errors if empty, pointing the operator at the Access tab); `"vigi"` → `DialTapo` with `BrandVigi`, falling back to the camera's ONVIF admin password when no dedicated `TalkPassword` is stored. Errors when the camera is not found, capability is unsupported, or the resolved transport is unknown.

## Key Types

- `TalkCapabilityResult` — `{Supported bool, Transport string, NeedsPassword bool, HasPassword bool, Detail string}`. `Transport` is `"onvif"`, `"tapo"`, `"vigi"`, or `"none"`. `NeedsPassword` also gates the TP-Link speaker-password UI — it is only ever true for a camera that genuinely fingerprinted as a TP-Link talk service.
- `cachedTalkCapability` — `{cap TalkCapabilityResult, at int64}` internal cache entry.

## Notes

- ONVIF reuses the camera's already-stored ONVIF/RTSP credentials, so there is no extra password to configure for that path. TP-Link Tapo/VIGI cameras need a separately stored `TalkPassword` (except the legacy `username="none"` exchange, `NeedsPassword: false`), set via `SaveTalkPassword`/`POST /api/cameras/{id}/talk/password`.
- `apps/mymatasan/entities/camera.go` carries the `TalkTransport` (cache hint, not authoritative — `resolveTalkCapability` re-probes) and `TalkPassword` (`json:"-"`, never serialized to the client) fields this file reads/writes.
- `cameraService.talkCapById`/`talkCapMu` are initialized in `NewCameraService` (`services/camera.go`) alongside the existing `lprCapById` cache.
