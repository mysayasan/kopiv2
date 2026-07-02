# Module: apps/mymatasan/services/talk.go

## Purpose

Implements the `ICameraService` talk-back (two-way audio) capability methods: `TalkCapability` and `OpenTalkSession`.

## Responsibilities

- `TalkCapability(ctx, id) TalkCapabilityResult` — returns the cached capability for a camera, re-probing (`resolveTalkCapability`) when the 10-minute TTL (`talkCapabilityCacheTTL`) has expired. Cache is keyed by camera id (`cameraService.talkCapById`, guarded by `talkCapMu`), mirroring the `LPRCapability` cache pattern.
- `resolveTalkCapability(ctx, id)` — loads the camera detail; if it has no stored RTSP URL, returns `{Transport: "none"}` without probing. Otherwise builds the RTSP URI with stored credentials and calls `infra/talk.HasBackchannel` (8s timeout) — `Supported: true, Transport: "onvif"` on a hit, else `{Transport: "none", Detail: "..."}`.
- `OpenTalkSession(ctx, id) (talk.Session, error)` — loads the camera detail, re-checks `TalkCapability` (cheap, cache-hit in the common case), and if supported dials `infra/talk.DialONVIF` with the camera's credentialed RTSP URI. Errors when the camera is not found or capability resolves unsupported.

## Key Types

- `TalkCapabilityResult` — `{Supported bool, Transport string, Detail string}`. `Transport` is `"onvif"` or `"none"`.
- `cachedTalkCapability` — `{cap TalkCapabilityResult, at int64}` internal cache entry.

## Notes

- Only the ONVIF audio backchannel is supported; it reuses the camera's already-stored ONVIF/RTSP credentials, so there is no extra password to configure for that path.
- `apps/mymatasan/entities/camera.go` carries `TalkTransport`/`TalkPassword` fields reserved for a future non-ONVIF transport (e.g. TP-Link's proprietary speaker protocol); they are not read or written by this file yet.
- `cameraService.talkCapById`/`talkCapMu` are initialized in `NewCameraService` (`services/camera.go`) alongside the existing `lprCapById` cache.
