# Module: infra/onvif/encoder.go

## Purpose

Reads and writes a camera's ONVIF video encoder configuration so the app can push a recording codec (H.264/H.265) and bitrate cap to the camera's own encoder — the zero-host-cost compression lever (the camera encodes smaller video and the recorder stream-copies it).

## Responsibilities

- `GetVideoEncoderConfig(ctx, StreamURIRequest)` resolves the media service URL + a profile token (reusing the streaming-path helpers) and returns the current `VideoEncoderConfig` (codec, resolution, quality, frame-rate/encoding-interval, bitrate, GOV length, H.264 profile) read out of `GetProfiles` (which embeds the full configuration).
- `ConfigureRecording(ctx, ConfigureRecordingRequest)` applies a codec (+ optional bitrate cap):
  - Reads the current config, then sets the target encoding/bitrate while preserving the rest.
  - **Applies via ONVIF Media2 (ver20) first**, falling back to Media1 (ver10). H.265 is only defined in the Media2 schema, so many cameras accept a Media1 set with HTTP 200 but silently keep H.264.
  - **Verifies by re-reading** from the camera and returns the actual config. A Media1 write that did not stick is a hard failure with a descriptive reason (the camera kept its old codec; it likely only allows codec changes from its own web UI, or — for H.265 — does not expose Media2). An accepted Media2 write is trusted even if the Media1 profile view differs (some cameras expose the two profile sets independently).
- `getMedia2ServiceURL` / `ParseMedia2ServiceXAddr` discover the ver20 media service XAddr via device `GetServices`.
- `setVideoEncoderConfigurationBody` (Media1, `trt`) and `setVideoEncoder2ConfigurationBody` (Media2, `tr2`) build the SOAP set bodies; the Media1 body emits the `H264` sub-element only for H.264 and sets `ForcePersistence`, the Media2 body carries `GovLength`/`Profile` as attributes (where H.265 is valid).
- `ParseVideoEncoderConfig` extracts a profile's `VideoEncoderConfiguration` from a `GetProfiles` response; `normalizeEncoding`/`canonicalEncoding` map codec spellings to ONVIF tokens / comparison labels.

## Notes

- Best-effort and camera-dependent: cameras that do not allow encoder changes over ONVIF surface the camera's own fault or the "did not apply" verification error, which the camera API forwards to the client.
- The shared `soapEnvelope` (client.go) declares the `tr2` (ver20 media) namespace used by the Media2 bodies.
- This is independent of the host-side at-rest codec (infra/recording/encode.go); the storage-codec lever is the camera-agnostic fallback when a camera can't be reconfigured.
