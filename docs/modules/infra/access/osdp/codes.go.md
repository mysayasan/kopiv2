# Module: infra/access/osdp/codes.go

## Purpose

The OSDP v2.2 command/reply/NAK vocabulary (per doc.osdp.dev), scoped to what P1 needs
(`MYPINTUSAN_OSDP_PLAN.md` §2.2). Unknown codes are not an error case the decoder rejects — `Frame`
carries a raw byte regardless, and `Command`/`Reply`/`NakCode`'s `String()` methods fall back to a
hex rendering, because a PD answering with something outside this list is a diagnostic worth
surfacing, not a frame worth dropping.

## Responsibilities

- `Command` (CP→PD) — `CmdPoll`, `CmdID`, `CmdCap`, `CmdLStat`, `CmdIStat`, `CmdOStat`, `CmdRStat`,
  `CmdOut`, `CmdLED`, `CmdBuz`, `CmdText`, `CmdComSet`, `CmdKeySet`, `CmdChlng`, `CmdSCrypt`.
- `Reply` (PD→CP) — `RplAck`, `RplNak`, `RplPDID`, `RplPDCap`, `RplLStatR`, `RplIStatR`,
  `RplOStatR`, `RplRStatR`, `RplRaw` (card data — unsolicited, arrives only as a `CmdPoll`
  reply), `RplKeypad`, `RplCom`, `RplCCrypt`, `RplRMacI`, `RplBusy` ("ask me again", not "I am
  broken").
- `NakCode` — the single data byte of a `RplNak`: `NakBadCheck`, `NakBadCommand`,
  `NakBadSequence`, `NakBadSCB`, `NakEncRequired`, `NakBIONotSup`. Distinguishing these (in
  `cp.go`/`bus.go`) is the difference between "the reader is faulty" and "we sent the wrong
  sequence number".
- `String()` on all three types, backed by name tables, for logging.

## Notes

- `RplBusy` and `NakBadSequence` each drive dedicated handling in `bus.go`'s `handleReply` — a
  `BUSY` reply must not count as a failed transaction, and a `NakBadSequence` forces a session
  reset (`resetSeq`).
- `securechannel.go` reuses `CmdChlng`/`CmdSCrypt`/`RplCCrypt`/`RplRMacI` as the handshake step
  markers.
