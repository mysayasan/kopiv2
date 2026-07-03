# Module: infra/talk/tapo.go

## Purpose

Implements the TP-Link Tapo/VIGI proprietary talk transport: a long-lived multipart HTTP session on port 8800, digest-authenticated, carrying G.711 A-law audio wrapped in MPEG-TS parts. These consumer cameras expose no ONVIF/RTSP audio backchannel, so this is their only two-way-audio path. Ported from go2rtc's `pkg/tapo` (MIT).

## Responsibilities

- `DialTapo(cfg TapoConfig)` — dials and digest-authenticates the port-8800 connection (`tapoAuthenticatedConn`), sends the JSON `{"talk":{"mode":"aec"}}` request part and reads back a `session_id` (`tapoSession.request`), then writes the one-off MPEG-TS PAT/PMT header (`tsMuxer.header`) the camera expects before any audio. Errors when the camera returns no session id (speaker unsupported or the password is wrong).
- `tapoSession.WritePCMA(payload, timestamp)` — muxes one G.711 A-law frame into a TS-packetized PES payload (`tsMuxer.payload`) and writes it as one `audio/mp2t` multipart part (`writePart`).
- `tapoSession.Close()` — idempotent; closes the underlying TCP connection.
- `tapoAuthenticatedConn(host, cfg)` — dials `host:8800`, sends an unauthenticated `POST /stream`, reads the `401` digest challenge, derives the username/password for the challenge (`tapoDigestCredentials`), retries with an `Authorization` header (`digestHeader`), and returns the raw connection on `200` without draining its body (it is the endless multipart device stream). A `401` on retry returns the sentinel error string `"talk-unauthorized: ..."` so callers (the API layer / UI) can distinguish "wrong password" from other failures.
- `tapoDigestCredentials(cfg, auth)` — three credential derivations selected by the challenge and `cfg.Brand`: the legacy `username="none"` exchange (fixed `tapoNonePassword`, CVE-2022-37255), the TP-Link cloud-account password hashed MD5 or SHA-256-uppercase-hex per `encrypt_type` (`BrandTapo`), or the camera admin password obfuscated with TP-Link's `securityEncode` scheme (`BrandVigi`).
- `digestHeader` / `md5Join` / `headerValue` / `randHex` — a minimal RFC 7616 MD5 digest (`qop=auth`) `Authorization` header builder for the TP-Link challenge.
- `normalizeTapoHost(host)` — appends `:8800` when `host` carries no port.
- `securityEncode(s)` — TP-Link's VIGI admin-password obfuscation (XOR against a fixed key table), ported verbatim from go2rtc.

## Key Types

- `TapoBrand` — `BrandTapo` (cloud-account password) or `BrandVigi` (obfuscated admin password); selects the credential derivation in `tapoDigestCredentials`.
- `TapoConfig` — `{Host, Brand, CloudPassword, Username, Password}`. `CloudPassword` is used for `BrandTapo`; `Username`/`Password` for `BrandVigi`.
- `tapoSession` — the `Session` implementation: a mutex-guarded connection, `tsMuxer` state, and the negotiated `session` id.

## Notes

- All of this transport's authentication and TS-muxing details (`tapoDialTimeout`/`tapoWriteTimeout`, the multipart boundaries, and the byte-for-byte MPEG-TS layout in `mpegts.go`) are exactly what Tapo/VIGI firmware requires — they are not general-purpose and must not be changed without re-verifying against real hardware.
- `Probe8800`/`isTPLinkStreamd` (`talk.go`) gate which cameras ever reach `DialTapo`: only a genuine TP-Link "Streamd" fingerprint match reports `Supported`, so unrelated devices with port 8800 open are never misdialed.
- `apps/mymatasan/services/talk.go` picks `BrandVigi` vs `BrandTapo` by sniffing `manufacturer`/`model`/`hardwareId`/ONVIF scopes for "vigi" (`isVigiCamera`); everything else defaults to `BrandTapo`.
