package osdp

// Command and reply codes, per the OSDP v2.2 command/reply reference (doc.osdp.dev). Only the codes
// P1 needs are named; the decoder passes unknown codes through untouched rather than rejecting them,
// because a PD answering with something outside this list is a diagnostic worth surfacing, not a
// frame worth dropping.

// Command is a CP->PD command code, carried in the frame's code byte.
type Command byte

const (
	CmdPoll   Command = 0x60 // the heartbeat; every card read arrives as the REPLY to one of these
	CmdID     Command = 0x61 // request PDID  — vendor/model/serial/firmware
	CmdCap    Command = 0x62 // request PDCAP — what the reader can actually do
	CmdLStat  Command = 0x64 // local status: tamper, power
	CmdIStat  Command = 0x65 // input status
	CmdOStat  Command = 0x66 // output status
	CmdRStat  Command = 0x67 // reader status
	CmdOut    Command = 0x68 // drive an output
	CmdLED    Command = 0x69 // reader LED
	CmdBuz    Command = 0x6A // reader buzzer
	CmdText   Command = 0x6B // reader display text
	CmdComSet Command = 0x6E // set PD address + baud — the out-of-box "everyone is at address 0" fix
	CmdKeySet Command = 0x75 // install a site SCBK, replacing the well-known default
	CmdChlng  Command = 0x76 // Secure Channel: server random
	CmdSCrypt Command = 0x77 // Secure Channel: server cryptogram
)

// Reply is a PD->CP reply code.
type Reply byte

const (
	RplAck    Reply = 0x40
	RplNak    Reply = 0x41
	RplPDID   Reply = 0x45
	RplPDCap  Reply = 0x46
	RplLStatR Reply = 0x48
	RplIStatR Reply = 0x49
	RplOStatR Reply = 0x4A
	RplRStatR Reply = 0x4B
	// RplRaw carries card data. It is unsolicited in every sense that matters: the CP never asks
	// for a card, it polls, and the PD hands this back when a human badges. That is why the driver
	// API is a channel of events and not a ReadCard() call.
	RplRaw    Reply = 0x50
	RplKeypad Reply = 0x53
	RplCom    Reply = 0x54
	RplCCrypt Reply = 0x76 // Secure Channel: client id + random + cryptogram
	RplRMacI  Reply = 0x78 // Secure Channel: client cryptogram + initial R-MAC
	// RplBusy means "ask me again", not "I am broken". Treating it as a failure is how a CP
	// declares a healthy reader dead under load.
	RplBusy Reply = 0x79
)

// NAK error codes, from the single data byte of a NAK reply. Worth naming because these are the
// difference between "the reader is faulty" and "we sent the wrong sequence number" — see
// ErrSequence handling in cp.go.
type NakCode byte

const (
	NakBadCheck    NakCode = 0x01 // bad CRC/checksum
	NakBadCommand  NakCode = 0x02 // command not understood
	NakBadSequence NakCode = 0x03 // sequence number out of order
	NakBadSCB      NakCode = 0x04 // unsupported security block
	NakEncRequired NakCode = 0x05 // PD demands an encrypted session
	NakBIONotSup   NakCode = 0x06
)

var commandNames = map[Command]string{
	CmdPoll: "POLL", CmdID: "ID", CmdCap: "CAP", CmdLStat: "LSTAT", CmdIStat: "ISTAT",
	CmdOStat: "OSTAT", CmdRStat: "RSTAT", CmdOut: "OUT", CmdLED: "LED", CmdBuz: "BUZ",
	CmdText: "TEXT", CmdComSet: "COMSET", CmdKeySet: "KEYSET", CmdChlng: "CHLNG",
	CmdSCrypt: "SCRYPT",
}

var replyNames = map[Reply]string{
	RplAck: "ACK", RplNak: "NAK", RplPDID: "PDID", RplPDCap: "PDCAP", RplLStatR: "LSTATR",
	RplIStatR: "ISTATR", RplOStatR: "OSTATR", RplRStatR: "RSTATR", RplRaw: "RAW",
	RplKeypad: "KEYPAD", RplCom: "COM", RplCCrypt: "CCRYPT", RplRMacI: "RMAC_I", RplBusy: "BUSY",
}

var nakNames = map[NakCode]string{
	NakBadCheck: "bad-checksum", NakBadCommand: "bad-command", NakBadSequence: "bad-sequence",
	NakBadSCB: "bad-security-block", NakEncRequired: "encryption-required",
	NakBIONotSup: "biometry-unsupported",
}

func (c Command) String() string {
	if s, ok := commandNames[c]; ok {
		return s
	}
	return "CMD(" + hexByte(byte(c)) + ")"
}

func (r Reply) String() string {
	if s, ok := replyNames[r]; ok {
		return s
	}
	return "RPY(" + hexByte(byte(r)) + ")"
}

func (n NakCode) String() string {
	if s, ok := nakNames[n]; ok {
		return s
	}
	return "nak(" + hexByte(byte(n)) + ")"
}

const hexDigits = "0123456789abcdef"

func hexByte(b byte) string {
	return "0x" + string([]byte{hexDigits[b>>4], hexDigits[b&0x0F]})
}
