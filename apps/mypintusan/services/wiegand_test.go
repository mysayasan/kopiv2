package services

import (
	"strconv"
	"testing"

	"github.com/mysayasan/kopiv2/apps/mypintusan/entities"
)

// TestWiegand26RoundTrip walks the whole facility-code space and a spread of card numbers. Wiegand
// decoding is the easiest place in the app to produce a plausible WRONG answer — a misread number
// does not error, it identifies a different person — so this is exhaustive where it can afford to be.
func TestWiegand26RoundTrip(t *testing.T) {
	for facility := 0; facility < 256; facility++ {
		for _, number := range []int{0, 1, 1234, 32767, 65534, 65535} {
			data := EncodeWiegand26(facility, number)
			card, err := DecodeCard(26, data)
			if err != nil {
				t.Fatalf("facility %d number %d: %v", facility, number, err)
			}
			if card.FacilityCode != facility {
				t.Fatalf("facility = %d, want %d", card.FacilityCode, facility)
			}
			if card.Number != strconv.Itoa(number) {
				t.Fatalf("number = %s, want %d", card.Number, number)
			}
			if card.Format != entities.FormatWiegand26 {
				t.Fatalf("format = %s", card.Format)
			}
		}
	}
}

// TestWiegand26ParityCatchesBitFlips is the reason parity is checked rather than assumed. On a long
// or badly terminated run a flipped bit is otherwise indistinguishable from a DIFFERENT CARD, which
// is to say it opens the door for the wrong person.
func TestWiegand26ParityCatchesBitFlips(t *testing.T) {
	base := EncodeWiegand26(7, 1234)

	caught, missed := 0, 0
	for i := 0; i < 26; i++ {
		bad := append([]byte(nil), base...)
		bad[i/8] ^= 1 << (7 - uint(i%8))
		if _, err := DecodeCard(26, bad); err != nil {
			caught++
		} else {
			missed++
		}
	}
	// A single parity bit per half cannot catch every single-bit error in the other half, but it
	// must catch the overwhelming majority; zero would mean parity is not being evaluated at all.
	if caught == 0 {
		t.Fatal("parity caught no single-bit errors — it is not being checked")
	}
	if caught < 20 {
		t.Errorf("parity caught only %d of 26 single-bit flips (%d missed)", caught, missed)
	}
}

// TestFacilityCodeIsPartOfTheKey pins the collision that bites real sites: the same card number in
// two facility codes is two different people.
func TestFacilityCodeIsPartOfTheKey(t *testing.T) {
	a, _ := DecodeCard(26, EncodeWiegand26(7, 1234))
	b, _ := DecodeCard(26, EncodeWiegand26(8, 1234))

	if a.Number != b.Number {
		t.Fatal("test premise broken: the two cards should share a number")
	}
	fa, ca, na := a.Key()
	fb, cb, nb := b.Key()
	if fa == fb && ca == cb && na == nb {
		t.Error("two cards from different facility codes produced the same match key — " +
			"a site with two card batches would open doors for the wrong person")
	}
}

// TestDecodeUnknownLengthFallsBackToRawUID covers DESFire/Seos readers, which emit their own
// lengths. Refusing to read them would make those readers unusable; the mitigation for a cloneable
// UID is the door-class cap, not a refusal.
func TestDecodeUnknownLengthFallsBackToRawUID(t *testing.T) {
	card, err := DecodeCard(56, []byte{0x04, 0xA1, 0xB2, 0xC3, 0xD4, 0xE5, 0xF6})
	if err != nil {
		t.Fatalf("raw UID decode: %v", err)
	}
	if card.Format != entities.FormatRawUID {
		t.Errorf("format = %s, want raw-uid", card.Format)
	}
	if card.Number != "04a1b2c3d4e5f6" {
		t.Errorf("number = %s", card.Number)
	}
}

// TestDecodeRetainsRawOnFailure guards the most valuable row in the access log.
func TestDecodeRetainsRawOnFailure(t *testing.T) {
	bad := EncodeWiegand26(7, 1234)
	bad[1] ^= 0xFF // corrupt the facility byte, breaking parity

	card, err := DecodeCard(26, bad)
	if err == nil {
		t.Fatal("expected a parity failure")
	}
	if card.Raw == "" {
		t.Error("the raw value was discarded on a decode failure — there would be nothing to investigate")
	}
	if card.BitCount != 26 {
		t.Errorf("bit count = %d, want 26", card.BitCount)
	}
}

func TestDecodeRejectsTruncatedPayload(t *testing.T) {
	if _, err := DecodeCard(26, []byte{0x01}); err == nil {
		t.Error("a 26-bit card carried in 1 byte was accepted")
	}
	if _, err := DecodeCard(0, nil); err == nil {
		t.Error("a zero-bit card was accepted")
	}
}

// TestWiegand34 covers the wider format. 34-bit is not one standard — vendors split the payload
// differently — so this pins OUR split rather than claiming universality.
func TestWiegand34(t *testing.T) {
	// facility 0x1234, number 0x5678, with parity computed over the documented halves.
	data := make([]byte, 5)
	set := func(n, v int) {
		if v != 0 {
			data[n/8] |= 1 << (7 - uint(n%8))
		}
	}
	for i := 0; i < 16; i++ {
		set(1+i, (0x1234>>(15-i))&1)
	}
	for i := 0; i < 16; i++ {
		set(17+i, (0x5678>>(15-i))&1)
	}
	set(0, evenParity(data, 1, 16))
	set(33, oddParity(data, 17, 32))

	card, err := DecodeCard(34, data)
	if err != nil {
		t.Fatalf("wiegand34: %v", err)
	}
	if card.FacilityCode != 0x1234 {
		t.Errorf("facility = %d, want %d", card.FacilityCode, 0x1234)
	}
	if card.Number != strconv.Itoa(0x5678) {
		t.Errorf("number = %s, want %d", card.Number, 0x5678)
	}
	if card.Format != entities.FormatWiegand34 {
		t.Errorf("format = %s", card.Format)
	}
}
