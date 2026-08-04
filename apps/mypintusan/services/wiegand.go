package services

import (
	"encoding/hex"
	"fmt"
	"strconv"

	"github.com/mysayasan/kopiv2/apps/mypintusan/entities"
)

// Wiegand decoding: turning the bits a reader hands over into a facility code and a card number.
//
// This is small, boring, and the single easiest place in the app to produce a plausible wrong
// answer. A misread card number does not error — it silently identifies a DIFFERENT person, or
// nobody, and the symptom is "the system sometimes doesn't recognise my card". So the bit layouts
// are written out explicitly and the parity is actually checked.

// Card is a decoded credential presentation.
type Card struct {
	Format       string
	FacilityCode int
	Number       string
	// Raw is the presented value in a stable, loggable form. It is retained even when decoding
	// fails, because an unrecognised card at a perimeter door at 03:00 is worth investigating and
	// the raw bits are the only evidence there is.
	Raw string
	// BitCount is what the reader claimed. Kept because a reader emitting an unexpected length is
	// itself a diagnosis — usually a mismatched card format rather than a fault.
	BitCount int
}

// Key returns the match key used to find a credential: format, facility code and number together.
//
// Never the number alone. Card 1234 exists in every facility code ever issued, so a site that has
// bought two batches of cards will eventually open a door for the wrong person.
func (c Card) Key() (format string, facility int, number string) {
	return c.Format, c.FacilityCode, c.Number
}

// DecodeCard turns an OSDP RAW payload into a Card.
//
// bits is the bit count the reader reported and data is the payload, MSB first and left-aligned in
// the final byte — the reader packs from the top, so the last byte may carry padding that is not
// part of the credential.
func DecodeCard(bits int, data []byte) (Card, error) {
	raw := hex.EncodeToString(data)
	card := Card{Raw: raw, BitCount: bits}

	if bits <= 0 || len(data)*8 < bits {
		return card, fmt.Errorf("card claims %d bits but carries %d bytes", bits, len(data))
	}

	switch bits {
	case 26:
		return decodeWiegand26(card, data)
	case 34:
		return decodeWiegand34(card, data)
	default:
		// Anything else is passed through as a raw UID rather than rejected. A DESFire or Seos
		// reader emits its own lengths, and refusing to enrol a card just because we cannot split
		// it into facility/number would make those readers unusable. UID-only credentials are
		// cloneable, which is why the hardware plan caps such a reader at `interior` doors — the
		// cap is the mitigation, not a refusal to read.
		card.Format = entities.FormatRawUID
		card.Number = raw
		return card, nil
	}
}

// bit returns bit n counting from the MSB of the first byte.
func bit(data []byte, n int) int {
	return int(data[n/8]>>(7-uint(n%8))) & 1
}

// decodeWiegand26 decodes the classic H10301 layout:
//
//	bit  0      even parity over bits 1..12
//	bits 1..8   facility code   (8 bits)
//	bits 9..24  card number     (16 bits)
//	bit  25     odd parity over bits 13..24
func decodeWiegand26(card Card, data []byte) (Card, error) {
	card.Format = entities.FormatWiegand26

	facility, number := 0, 0
	for i := 1; i <= 8; i++ {
		facility = facility<<1 | bit(data, i)
	}
	for i := 9; i <= 24; i++ {
		number = number<<1 | bit(data, i)
	}
	card.FacilityCode = facility
	card.Number = strconv.Itoa(number)

	// Parity is checked, not assumed. A reader on a long or badly terminated run produces bit
	// errors, and a flipped bit in the card number is otherwise indistinguishable from a different
	// card — which is to say, it opens the door for the wrong person.
	if evenParity(data, 1, 12) != bit(data, 0) {
		return card, fmt.Errorf("wiegand26: leading even parity failed")
	}
	if oddParity(data, 13, 24) != bit(data, 25) {
		return card, fmt.Errorf("wiegand26: trailing odd parity failed")
	}
	return card, nil
}

// decodeWiegand34 decodes the common H10302/H10304-style 34-bit layout:
//
//	bit  0       even parity over bits 1..16
//	bits 1..16   facility code   (16 bits)
//	bits 17..32  card number     (16 bits)
//	bit  33      odd parity over bits 17..32
//
// 34-bit is NOT one standard — vendors split the 32 payload bits differently, and some use all 32
// as a single number with no facility code at all. This is the most common split; a site whose
// cards disagree needs a per-profile format rather than a change here.
func decodeWiegand34(card Card, data []byte) (Card, error) {
	card.Format = entities.FormatWiegand34

	facility, number := 0, 0
	for i := 1; i <= 16; i++ {
		facility = facility<<1 | bit(data, i)
	}
	for i := 17; i <= 32; i++ {
		number = number<<1 | bit(data, i)
	}
	card.FacilityCode = facility
	card.Number = strconv.Itoa(number)

	if evenParity(data, 1, 16) != bit(data, 0) {
		return card, fmt.Errorf("wiegand34: leading even parity failed")
	}
	if oddParity(data, 17, 32) != bit(data, 33) {
		return card, fmt.Errorf("wiegand34: trailing odd parity failed")
	}
	return card, nil
}

// evenParity returns the bit that makes bits [from,to] sum to even.
func evenParity(data []byte, from, to int) int {
	ones := 0
	for i := from; i <= to; i++ {
		ones += bit(data, i)
	}
	return ones % 2
}

// oddParity returns the bit that makes bits [from,to] sum to odd.
func oddParity(data []byte, from, to int) int {
	return 1 - evenParity(data, from, to)
}

// EncodeWiegand26 builds a 26-bit payload with correct parity. It exists for the simulator and for
// tests — being able to present a SPECIFIC card is what makes the decision path testable end to
// end without owning a card printer.
func EncodeWiegand26(facility, number int) []byte {
	data := make([]byte, 4) // 26 bits, left-aligned
	set := func(n, v int) {
		if v != 0 {
			data[n/8] |= 1 << (7 - uint(n%8))
		}
	}
	for i := 0; i < 8; i++ {
		set(1+i, (facility>>(7-i))&1)
	}
	for i := 0; i < 16; i++ {
		set(9+i, (number>>(15-i))&1)
	}
	set(0, evenParity(data, 1, 12))
	set(25, oddParity(data, 13, 24))
	return data
}
