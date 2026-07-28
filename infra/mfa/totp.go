// Package mfa implements the second-factor primitives myidsan uses for its own
// credentials: RFC 6238 TOTP (the algorithm every authenticator app speaks) and
// single-use recovery codes.
//
// The algorithm is protocol-frozen and tiny, so it is hand-rolled on the standard
// library (crypto/hmac + crypto/sha1) with no third-party dependency — the only
// external package in the whole feature is the QR encoder (qr.go), and even that
// runs entirely on-box so an air-gapped install never reaches a CDN.
//
// Verification deliberately allows only a ±1 step window and enforces a monotonic
// replay guard: a code captured in a proxy log or shoulder-surfed must not be
// reusable for the next 30–90 seconds. See Validate.
package mfa

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"
)

const (
	// stepSeconds is the TOTP time-step. 30s is the universal authenticator default.
	stepSeconds = 30
	// codeDigits is the number of digits in a code. 6 is the universal default.
	codeDigits = 6
	// skewSteps is how many steps on EACH side of "now" are accepted. Exactly one:
	// a wider window multiplies the online brute-force surface for no usability gain.
	skewSteps = 1
	// secretBytes is the entropy of a freshly-generated shared secret (160 bits,
	// matching the SHA-1 block the HMAC keys).
	secretBytes = 20
)

// b32 is RFC 4648 base32 with padding stripped — what authenticator apps expect in
// an otpauth:// secret and in the manual-entry fallback.
var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// GenerateSecret returns a new random shared secret as an un-padded base32 string,
// ready to seal at rest and to hand to the enrolling authenticator app.
func GenerateSecret() (string, error) {
	buf := make([]byte, secretBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return b32.EncodeToString(buf), nil
}

// TimeStep is the RFC 6238 step counter for a unix timestamp.
func TimeStep(unix int64) int64 { return unix / stepSeconds }

// generateCode computes the RFC 4226 HOTP / RFC 6238 TOTP value for a raw key and
// step counter, truncated to `digits`. Unexported and digit-parameterised so the
// test suite can drive it with the published RFC 6238 vectors (which use 8 digits).
func generateCode(key []byte, step uint64, digits int) string {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], step)

	mac := hmac.New(sha1.New, key)
	mac.Write(msg[:])
	sum := mac.Sum(nil)

	// Dynamic truncation (RFC 4226 §5.3).
	offset := sum[len(sum)-1] & 0x0f
	bin := (uint32(sum[offset])&0x7f)<<24 |
		uint32(sum[offset+1])<<16 |
		uint32(sum[offset+2])<<8 |
		uint32(sum[offset+3])

	mod := uint32(1)
	for i := 0; i < digits; i++ {
		mod *= 10
	}
	return fmt.Sprintf("%0*d", digits, bin%mod)
}

// Validate checks a user-supplied code against a base32 secret within ±1 step of
// `now`, enforcing the replay guard. It returns whether the code is valid and, on
// success, the accepted time-step — which the caller MUST persist as the new
// LastStep so the same (or any earlier) step can never be replayed.
//
// lastStep is the previously-accepted step for this factor (0 for a factor that
// has never completed a login). Any candidate step ≤ lastStep is refused even if
// the HMAC matches.
func Validate(secretB32, code string, now time.Time, lastStep int64) (ok bool, acceptedStep int64) {
	secretB32 = strings.TrimSpace(strings.ToUpper(secretB32))
	code = strings.TrimSpace(code)
	if secretB32 == "" || len(code) != codeDigits {
		return false, 0
	}
	key, err := b32.DecodeString(secretB32)
	if err != nil || len(key) == 0 {
		return false, 0
	}

	center := TimeStep(now.Unix())
	// Check the earliest step first so that, given clock skew, we still advance
	// LastStep to the newest matching step and keep the replay guard monotonic.
	for delta := int64(-skewSteps); delta <= skewSteps; delta++ {
		step := center + delta
		if step <= lastStep {
			// Already consumed (replay) — or clock so far behind it predates the
			// last success. Never accept.
			continue
		}
		want := generateCode(key, uint64(step), codeDigits)
		if subtle.ConstantTimeCompare([]byte(want), []byte(code)) == 1 {
			return true, step
		}
	}
	return false, 0
}

// OtpauthURI builds the standard provisioning URI an authenticator app consumes,
// e.g. otpauth://totp/myidsan:alice@corp?secret=...&issuer=myidsan. Both label
// components are percent-encoded so an email account or an issuer with spaces
// produces a valid, scannable URI.
func OtpauthURI(issuer, account, secretB32 string) string {
	issuer = strings.TrimSpace(issuer)
	if issuer == "" {
		issuer = "myidsan"
	}
	label := url.PathEscape(issuer + ":" + strings.TrimSpace(account))
	q := url.Values{}
	q.Set("secret", strings.TrimSpace(secretB32))
	q.Set("issuer", issuer)
	q.Set("algorithm", "SHA1")
	q.Set("digits", fmt.Sprintf("%d", codeDigits))
	q.Set("period", fmt.Sprintf("%d", stepSeconds))
	return "otpauth://totp/" + label + "?" + q.Encode()
}
