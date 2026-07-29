package services

import (
	"errors"
	"fmt"
	"strings"
	"unicode"

	"github.com/mysayasan/kopiv2/infra/config"
)

// Password policy enforcement.
//
// Before this, the ONLY rule anywhere was `len >= 8`, applied on exactly two of the four
// paths that set a password: ChangePassword and SetPasswordSelfService. Self-registration
// and admin account creation checked non-empty and nothing else, so an administrator could
// create an account with the password "a" on the server that authenticates the whole suite.
//
// The four paths are now funnelled through ValidatePassword. Server-GENERATED credentials
// are deliberately exempt (see PasswordPolicyConfigModel): they are CSPRNG output and
// already exceed anything expressible here.

// ErrPasswordPolicy is the sentinel for a rejected password. The wrapped message is
// user-facing and says what to fix — "password is too short" with no target length is a
// dead end for the person typing.
var ErrPasswordPolicy = errors.New("password does not meet the policy")

// policyError builds a user-facing rejection.
func policyError(reason string) error {
	return fmt.Errorf("%w: %s", ErrPasswordPolicy, reason)
}

// ValidatePassword checks a human-chosen password against the policy.
//
// identifier is the account's email/username when known. A password that merely repeats
// the username is rejected regardless of length or composition: it satisfies every
// character rule and is the first thing an attacker tries.
func ValidatePassword(policy config.EffectivePasswordPolicy, password, identifier string) error {
	// Deliberately NOT trimmed before the length check. A trailing space is a legitimate
	// character in a password, and silently trimming would mean the stored password
	// differed from what the user typed — which surfaces later as an inexplicable failed
	// login. Only the emptiness check looks at the trimmed form.
	if strings.TrimSpace(password) == "" {
		return policyError("a password is required")
	}

	if len([]rune(password)) < policy.MinLength {
		return policyError(fmt.Sprintf("must be at least %d characters", policy.MinLength))
	}

	var hasUpper, hasLower, hasDigit, hasSymbol bool
	for _, r := range password {
		switch {
		case unicode.IsUpper(r):
			hasUpper = true
		case unicode.IsLower(r):
			hasLower = true
		case unicode.IsDigit(r):
			hasDigit = true
		case unicode.IsPunct(r), unicode.IsSymbol(r):
			hasSymbol = true
		}
	}
	var missing []string
	if policy.RequireUpper && !hasUpper {
		missing = append(missing, "an uppercase letter")
	}
	if policy.RequireLower && !hasLower {
		missing = append(missing, "a lowercase letter")
	}
	if policy.RequireDigit && !hasDigit {
		missing = append(missing, "a digit")
	}
	if policy.RequireSymbol && !hasSymbol {
		missing = append(missing, "a symbol")
	}
	if len(missing) > 0 {
		return policyError("must contain " + strings.Join(missing, ", "))
	}

	// The username check is not optional: a password equal to the account name defeats
	// every other rule at once.
	if id := strings.TrimSpace(strings.ToLower(identifier)); id != "" {
		lower := strings.ToLower(password)
		if lower == id {
			return policyError("must not be the same as the username")
		}
		// Also catch the local part of an email address used verbatim.
		if at := strings.IndexByte(id, '@'); at > 0 && lower == id[:at] {
			return policyError("must not be the same as the username")
		}
	}

	if policy.BlockCommon && isCommonPassword(password) {
		return policyError("that password is among the most commonly used and is easy to guess")
	}

	return nil
}

// isCommonPassword tests the embedded denylist, case-insensitively.
//
// The comparison strips nothing else: "Password123" and "password123" are the same guess
// to an attacker running a wordlist with case rules, so folding case is right, but
// stripping digits or symbols would reject legitimate passphrases that merely contain a
// common word.
func isCommonPassword(password string) bool {
	_, found := commonPasswords[strings.ToLower(strings.TrimSpace(password))]
	return found
}

// commonPasswords is a deliberately small, high-value denylist: the passwords that
// dominate every leaked-credential corpus, plus the ones people reach for when a policy
// demands a symbol or a capital. It is NOT a substitute for a breach corpus — it is what
// can be shipped in an air-gapped binary, and it catches the guesses that actually get
// used. Entries are lowercase; matching folds case.
//
// Anything shorter than the default minimum length is included anyway, because the minimum
// is configurable downward and these must stay rejected if it is.
var commonPasswords = func() map[string]struct{} {
	list := []string{
		"123456", "123456789", "12345678", "1234567890", "12345", "1234567",
		"password", "password1", "password12", "password123", "password1234",
		"passw0rd", "p@ssw0rd", "p@ssword", "pa55word", "passsword",
		"qwerty", "qwerty123", "qwertyuiop", "1q2w3e4r", "1qaz2wsx", "zaq12wsx",
		"admin", "administrator", "admin123", "admin1234", "adminadmin",
		"root", "toor", "root123", "letmein", "letmein123",
		"welcome", "welcome1", "welcome123", "changeme", "changeme123",
		"iloveyou", "monkey", "dragon", "sunshine", "princess", "football",
		"baseball", "superman", "trustno1", "master", "shadow", "michael",
		"abc123", "abcd1234", "a1b2c3d4", "111111", "000000", "654321",
		"secret", "secret123", "default", "guest", "test", "test123", "testing",
		"temp", "temp123", "temporary", "user", "user123", "login", "pass",
		"qazwsx", "asdfgh", "zxcvbnm", "1234qwer", "qwe123", "asd123",
		"summer2024", "summer2025", "winter2024", "winter2025",
		"spring2024", "spring2025", "autumn2024", "autumn2025",
		"company123", "corporate1", "office123", "server123", "database123",
		"kopiv2", "myidsan", "myidsan123", "sso123", "identity123",
	}
	set := make(map[string]struct{}, len(list))
	for _, entry := range list {
		set[entry] = struct{}{}
	}
	return set
}()
