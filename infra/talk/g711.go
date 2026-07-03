package talk

// G.711 A-law → µ-law transcoding for cameras whose backchannel wants PCMU
// while the browser sends PCMA. Both codecs carry one byte per 8 kHz sample, so
// conversion is a 256-entry table lookup built from the standard companding
// formulas at init.

var alawToUlawTable [256]byte

func init() {
	for i := 0; i < 256; i++ {
		alawToUlawTable[i] = linearToUlaw(alawToLinear(byte(i)))
	}
}

// alawToLinear decodes one A-law byte to a 16-bit PCM sample (ITU-T G.711).
func alawToLinear(a byte) int16 {
	a ^= 0x55
	t := int16(a&0x0F) << 4
	seg := (a & 0x70) >> 4
	switch seg {
	case 0:
		t += 8
	case 1:
		t += 0x108
	default:
		t += 0x108
		t <<= seg - 1
	}
	if a&0x80 != 0 {
		return t
	}
	return -t
}

// linearToUlaw encodes a 16-bit PCM sample as one µ-law byte (ITU-T G.711).
func linearToUlaw(pcm int16) byte {
	const bias = 0x84
	const clip = 32635
	var sign byte
	v := int32(pcm)
	if v < 0 {
		v = -v
		sign = 0x80
	}
	if v > clip {
		v = clip
	}
	v += bias
	exp := byte(7)
	for mask := int32(0x4000); v&mask == 0 && exp > 0; exp, mask = exp-1, mask>>1 {
	}
	mant := byte((v >> (exp + 3)) & 0x0F)
	return ^(sign | exp<<4 | mant)
}

// alawToUlaw converts a G.711 A-law frame to µ-law in a new slice.
func alawToUlaw(alaw []byte) []byte {
	out := make([]byte, len(alaw))
	for i, b := range alaw {
		out[i] = alawToUlawTable[b]
	}
	return out
}
