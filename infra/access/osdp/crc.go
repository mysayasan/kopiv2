// Package osdp implements the SIA OSDP v2.x wire protocol in CP (Control Panel) mode: mypintusan
// polls readers, readers reply. It is pure Go with no CGo and no external protocol dependency, which
// is what keeps the suite's single-static-binary promise intact.
//
// This file is framing arithmetic only — no I/O, no state. See frame.go for the packet layout and
// codes.go for the command/reply vocabulary.
package osdp

// OSDP frames end in a CRC-16-CCITT: polynomial 0x1021, MSB-first (NOT reflected), initial value
// 0x1D0F. That init value is the whole trap. It is *not* the 0xFFFF of the more common CCITT-FALSE
// variant, and it is a different algorithm entirely from the Modbus RTU CRC in
// infra/iot/modbus/crc.go, which is reflected (0xA001) with init 0xFFFF. Nothing is shared between
// the two; a "reuse the CRC we already have" shortcut here produces a bus where every frame NAKs and
// the symptom looks exactly like miswired A/B lines.
//
// Pinned against libosdp's osdp_phy.c, which computes crc16_itu_t(0x1D0F, buf, len). In CRC
// catalogue terms this is CRC-16/AUG-CCITT (a.k.a. CRC-16/SPI-FUJITSU); TestCRCCheckValue pins its
// published check value so a future "optimisation" cannot quietly change the algorithm.
const (
	crcPoly = 0x1021
	crcInit = 0x1D0F
)

// CRC16 computes the OSDP frame CRC over data.
func CRC16(data []byte) uint16 {
	crc := uint16(crcInit)
	for _, b := range data {
		crc ^= uint16(b) << 8
		for i := 0; i < 8; i++ {
			if crc&0x8000 != 0 {
				crc = (crc << 1) ^ crcPoly
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}

// AppendCRC returns frame with its CRC appended low-byte-first.
//
// Byte order follows the spec and libosdp (buf[len+0] = BYTE_0(crc), buf[len+1] = BYTE_1(crc)).
// MYPINTUSAN_OSDP_PLAN.md §2.1 lists this as unconfirmed against real hardware — it is the one
// framing fact in this file not yet seen on a wire, so if a bench reader NAKs every frame while the
// simulator is happy, swap these two bytes before suspecting anything else.
func AppendCRC(frame []byte) []byte {
	c := CRC16(frame)
	return append(frame, byte(c), byte(c>>8))
}

// CRCOK reports whether a complete frame (header + payload + trailing 2-byte CRC) checks out.
func CRCOK(frame []byte) bool {
	if len(frame) < 3 {
		return false
	}
	n := len(frame) - 2
	want := uint16(frame[n]) | uint16(frame[n+1])<<8
	return CRC16(frame[:n]) == want
}

// checksum8 computes the legacy 8-bit checksum used when the CTRL byte's CRC bit is clear: the
// two's complement of the sum of all preceding bytes. We never transmit in this mode (see
// frame.go), but decoding it is what lets us tell a legacy/misconfigured PD apart from line noise
// instead of reporting a CRC failure and sending the installer looking for a cabling fault.
func checksum8(data []byte) byte {
	var sum byte
	for _, b := range data {
		sum += b
	}
	return byte(-int8(sum))
}
