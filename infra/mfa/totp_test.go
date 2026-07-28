package mfa

import (
	"testing"
	"time"
)

// rfc6238Key is the SHA-1 test key from RFC 6238 Appendix B: the ASCII string
// "12345678901234567890" (20 bytes).
var rfc6238Key = []byte("12345678901234567890")

// TestRFC6238Vectors checks generateCode against the published 8-digit SHA-1
// vectors in RFC 6238 Appendix B. If this passes, the HOTP truncation and the
// step counter are correct; the 6-digit production path is the same code with a
// smaller modulus.
func TestRFC6238Vectors(t *testing.T) {
	cases := []struct {
		unix int64
		want string
	}{
		{59, "94287082"},
		{1111111109, "07081804"},
		{1111111111, "14050471"},
		{1234567890, "89005924"},
		{2000000000, "69279037"},
		{20000000000, "65353130"},
	}
	for _, c := range cases {
		step := uint64(TimeStep(c.unix))
		got := generateCode(rfc6238Key, step, 8)
		if got != c.want {
			t.Errorf("unix=%d step=%d: got %s want %s", c.unix, step, got, c.want)
		}
	}
}

// enrollSecret is a fixed base32 secret used to exercise Validate deterministically.
const enrollSecret = "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ" // decodes to "12345678901234567890"

func TestValidateAcceptsCurrentStep(t *testing.T) {
	now := time.Unix(1234567890, 0)
	step := TimeStep(now.Unix())
	key, _ := b32.DecodeString(enrollSecret)
	code := generateCode(key, uint64(step), codeDigits)

	ok, accepted := Validate(enrollSecret, code, now, 0)
	if !ok {
		t.Fatalf("current-step code rejected")
	}
	if accepted != step {
		t.Fatalf("accepted step=%d want %d", accepted, step)
	}
}

func TestValidateSkewBoundary(t *testing.T) {
	now := time.Unix(1234567890, 0)
	center := TimeStep(now.Unix())
	key, _ := b32.DecodeString(enrollSecret)

	// ±1 step must be accepted.
	for _, d := range []int64{-1, 0, 1} {
		code := generateCode(key, uint64(center+d), codeDigits)
		if ok, _ := Validate(enrollSecret, code, now, 0); !ok {
			t.Errorf("delta=%d should be accepted", d)
		}
	}
	// ±2 steps must be rejected (window is exactly one).
	for _, d := range []int64{-2, 2} {
		code := generateCode(key, uint64(center+d), codeDigits)
		if ok, _ := Validate(enrollSecret, code, now, 0); ok {
			t.Errorf("delta=%d should be rejected", d)
		}
	}
}

func TestValidateReplayGuard(t *testing.T) {
	now := time.Unix(1234567890, 0)
	step := TimeStep(now.Unix())
	key, _ := b32.DecodeString(enrollSecret)
	code := generateCode(key, uint64(step), codeDigits)

	// First use accepts and yields the step.
	ok, accepted := Validate(enrollSecret, code, now, 0)
	if !ok {
		t.Fatal("first use should accept")
	}
	// Replaying the SAME code with lastStep advanced to the accepted step must fail.
	if ok, _ := Validate(enrollSecret, code, now, accepted); ok {
		t.Fatal("replayed code must be rejected once lastStep is recorded")
	}
	// A code from an earlier step than lastStep is also rejected even though its
	// HMAC would match within the window.
	prev := generateCode(key, uint64(step-1), codeDigits)
	if ok, _ := Validate(enrollSecret, prev, now, accepted); ok {
		t.Fatal("code older than lastStep must be rejected")
	}
}

func TestValidateRejectsGarbage(t *testing.T) {
	now := time.Unix(1234567890, 0)
	for _, bad := range []string{"", "123", "1234567", "abcdef", "12 45 6"} {
		if ok, _ := Validate(enrollSecret, bad, now, 0); ok {
			t.Errorf("garbage %q should be rejected", bad)
		}
	}
	// Wrong secret, right shape.
	if ok, _ := Validate("not-base32!!", "123456", now, 0); ok {
		t.Error("invalid secret should be rejected")
	}
}

func TestGenerateSecretRoundTrips(t *testing.T) {
	s, err := GenerateSecret()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b32.DecodeString(s); err != nil {
		t.Fatalf("secret is not valid base32: %v", err)
	}
	now := time.Now()
	key, _ := b32.DecodeString(s)
	code := generateCode(key, uint64(TimeStep(now.Unix())), codeDigits)
	if ok, _ := Validate(s, code, now, 0); !ok {
		t.Fatal("freshly generated secret failed to validate its own code")
	}
}

func TestRecoveryCodes(t *testing.T) {
	codes, err := GenerateRecoveryCodes()
	if err != nil {
		t.Fatal(err)
	}
	if len(codes) != RecoveryCodeCount {
		t.Fatalf("got %d codes want %d", len(codes), RecoveryCodeCount)
	}
	seen := map[string]bool{}
	for _, c := range codes {
		if seen[c] {
			t.Errorf("duplicate recovery code %q", c)
		}
		seen[c] = true
		// Normalisation of a lower-cased, space-substituted variant must round-trip.
		messy := ""
		for _, r := range c {
			switch {
			case r == '-':
				messy += " "
			case r >= 'A' && r <= 'Z':
				messy += string(r + 32) // lower-case letters only; digits have no case
			default:
				messy += string(r)
			}
		}
		if NormalizeRecoveryCode(messy) != c {
			t.Errorf("normalise(%q)=%q want %q", messy, NormalizeRecoveryCode(messy), c)
		}
	}
}

func TestOtpauthURI(t *testing.T) {
	uri := OtpauthURI("myidsan", "alice@corp.test", enrollSecret)
	if uri == "" || uri[:15] != "otpauth://totp/" {
		t.Fatalf("unexpected uri: %s", uri)
	}
	// The QR encoder must accept it.
	if _, err := QRPNG(uri, 128); err != nil {
		t.Fatalf("QR render failed: %v", err)
	}
}
