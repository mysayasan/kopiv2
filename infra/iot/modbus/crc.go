package modbus

// Modbus RTU framing carries no length field and no TCP checksum, so each frame ends in a CRC-16
// (the classic reflected 0xA001 polynomial, init 0xFFFF) appended low-byte-first. TCP framing (MBAP)
// has neither — TCP guarantees delivery and the header carries the length — which is the whole
// reason the two transports need different framing code.

// crc16 computes the Modbus RTU CRC over data.
func crc16(data []byte) uint16 {
	crc := uint16(0xFFFF)
	for _, b := range data {
		crc ^= uint16(b)
		for i := 0; i < 8; i++ {
			if crc&1 != 0 {
				crc = (crc >> 1) ^ 0xA001
			} else {
				crc >>= 1
			}
		}
	}
	return crc
}

// appendCRC returns frame with its CRC-16 appended low-byte-first (the on-the-wire order).
func appendCRC(frame []byte) []byte {
	c := crc16(frame)
	return append(frame, byte(c), byte(c>>8))
}

// crcOK reports whether a full RTU frame (payload + trailing 2-byte CRC) checks out.
func crcOK(frame []byte) bool {
	if len(frame) < 4 {
		return false
	}
	n := len(frame) - 2
	want := uint16(frame[n]) | uint16(frame[n+1])<<8
	return crc16(frame[:n]) == want
}
