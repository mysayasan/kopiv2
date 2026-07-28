package mfa

import (
	"crypto/rand"
	"strings"
)

// RecoveryCodeCount is how many single-use break-glass codes are minted per set.
const RecoveryCodeCount = 10

// recoveryGroups / recoveryGroupLen shape a code as groups of base32 chars joined
// by dashes ("xxxxx-xxxxx-xxxxx-xxxxx" = 20 chars = 100 bits of entropy), readable
// enough to write on paper and dense enough to be un-guessable.
const (
	recoveryGroups   = 4
	recoveryGroupLen = 5
)

// recoveryAlphabet excludes the visually ambiguous 0/O/1/I/L so a code copied by
// hand does not fail on a mistaken character.
const recoveryAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"

// GenerateRecoveryCodes returns RecoveryCodeCount fresh, high-entropy recovery
// codes in plaintext. The caller shows them to the user exactly once and persists
// only their bcrypt hashes.
func GenerateRecoveryCodes() ([]string, error) {
	codes := make([]string, 0, RecoveryCodeCount)
	for i := 0; i < RecoveryCodeCount; i++ {
		code, err := oneRecoveryCode()
		if err != nil {
			return nil, err
		}
		codes = append(codes, code)
	}
	return codes, nil
}

func oneRecoveryCode() (string, error) {
	total := recoveryGroups * recoveryGroupLen
	raw := make([]byte, total)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	var b strings.Builder
	for i, v := range raw {
		if i > 0 && i%recoveryGroupLen == 0 {
			b.WriteByte('-')
		}
		b.WriteByte(recoveryAlphabet[int(v)%len(recoveryAlphabet)])
	}
	return b.String(), nil
}

// NormalizeRecoveryCode canonicalises user input (case, spacing, dashes) so a code
// typed as "abcde fghij ..." still matches the stored hash of "ABCDE-FGHIJ-...".
func NormalizeRecoveryCode(in string) string {
	up := strings.ToUpper(strings.TrimSpace(in))
	var b strings.Builder
	for _, r := range up {
		if strings.ContainsRune(recoveryAlphabet, r) {
			b.WriteRune(r)
		}
	}
	// Re-insert the group dashes so the canonical form matches GenerateRecoveryCodes.
	compact := b.String()
	if len(compact) != recoveryGroups*recoveryGroupLen {
		return compact // let the hash comparison fail rather than mangle it further
	}
	var out strings.Builder
	for i := 0; i < len(compact); i += recoveryGroupLen {
		if i > 0 {
			out.WriteByte('-')
		}
		out.WriteString(compact[i : i+recoveryGroupLen])
	}
	return out.String()
}
