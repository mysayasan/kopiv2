# Module: infra/talk/mpegts.go

## Purpose

Minimal single-track MPEG-TS muxer for the TP-Link talk backchannel: packetizes G.711 A-law audio (TP-Link's private stream type `0x90`) into PES packets preceded by a one-off PAT+PMT header. The bit layout — including the little-endian CRC32 with a byte-reversed polynomial table and zero-byte stuffing — is ported verbatim from the go2rtc mpegts muxer because that is the exact framing Tapo/VIGI firmware accepts; it is not a general-purpose MPEG-TS writer.

## Responsibilities

- `tsMuxer.header()` — returns the two 188-byte PAT + PMT packets (`patSection`/`pmtSection` wrapped by `appendPSIPacket`) that must be sent once before any audio, declaring program 1 → PMT PID `0x1000` → one PCMA-Tapo (`0x90`) elementary stream on PID `0x100`.
- `tsMuxer.payload(timestamp, audio)` — wraps one audio frame in a PES packet (start code + stream id `0xC0` + PTS-only optional header via `writePTS`) and splits it across as many 188-byte TS packets as needed, incrementing the per-PID continuity counter and zero-stuffing the final packet's adaptation field. PTS accumulates the RTP timestamp deltas between successive calls (uint32 wrap-safe arithmetic), so the caller can pass raw 8 kHz RTP timestamps.
- `appendPSIPacket(out, pid, section)` — wraps one PSI section in a single TS packet (pointer field + zero stuffing to 188 bytes).
- `appendCRC(section)` / `tsChecksum(data)` / `tsCRCTable` — MPEG-TS section CRC32 in the little-endian, byte-reversed-table form TP-Link's TS parser expects (not the standard CRC32/MPEG-2 byte order).

## Key Types

- `tsMuxer` — per-session mux state: accumulated `pts`, `lastTS` (for delta computation), `started` (first-frame guard), and `counter` (TS continuity counter for the audio PID). Zero value is ready to use (`header()` needs no prior state).

## Notes

- Verified by `talk_test.go` (`TestMuxerHeaderAlignment`, `TestMuxerPayloadAlignment`, `TestMuxerPTSAdvances`) which check packet-size alignment, sync-byte placement, and PTS delta accumulation — not full protocol conformance, since that can only be confirmed against real hardware.
- Consumed exclusively by `tapo.go`'s `tapoSession` (`DialTapo`/`WritePCMA`); no other transport in this package uses MPEG-TS.
