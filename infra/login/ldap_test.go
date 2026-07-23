package login

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// SECURITY: filter metacharacters in the username must be escaped — otherwise a
// username like `*)(uid=*` rewrites the search filter (LDAP injection).
func TestBuildLdapUserFilter_EscapesUsername(t *testing.T) {
	got := BuildLdapUserFilter("(uid=%s)", `*)(uid=*`)
	if strings.ContainsAny(strings.TrimPrefix(strings.TrimSuffix(got, ")"), "(uid="), "*()") {
		t.Fatalf("unescaped filter metacharacters survived: %q", got)
	}
	if got != `(uid=\2a\29\28uid=\2a)` {
		t.Fatalf("unexpected escaped filter: %q", got)
	}
}

func TestBuildLdapUserFilter_SubstitutesEveryPlaceholder(t *testing.T) {
	got := BuildLdapUserFilter("(|(uid=%s)(mail=%s))", "alice")
	if got != "(|(uid=alice)(mail=alice))" {
		t.Fatalf("unexpected filter: %q", got)
	}
}

// SECURITY: an empty (or NUL-containing) password must be refused before any bind —
// many servers treat an empty password as a successful anonymous bind.
func TestLdapAuthenticate_RefusesEmptyPassword(t *testing.T) {
	settings := LdapSettings{Host: "ldap.example.test", BaseDn: "dc=example,dc=test"}
	for _, pwd := range []string{"", "   ", "with\x00nul"} {
		_, err := LdapAuthenticate(context.Background(), settings, "alice", pwd)
		if !errors.Is(err, ErrLdapInvalidCredential) {
			t.Errorf("password %q: err = %v, want ErrLdapInvalidCredential (no network I/O)", pwd, err)
		}
	}
}

func TestLdapSettings_Validate(t *testing.T) {
	if err := (LdapSettings{BaseDn: "dc=x"}).Validate(); err == nil {
		t.Error("missing host accepted")
	}
	if err := (LdapSettings{Host: "h"}).Validate(); err == nil {
		t.Error("missing base DN accepted")
	}
	if err := (LdapSettings{Host: "h", BaseDn: "dc=x", UserFilter: "(uid=alice)"}).Validate(); err == nil {
		t.Error("filter without a username placeholder accepted")
	}
	if err := (LdapSettings{Host: "h", BaseDn: "dc=x"}).Validate(); err != nil {
		t.Errorf("valid settings rejected: %v", err)
	}
}

func TestLdapSettings_Defaults(t *testing.T) {
	s := (LdapSettings{Host: "h", BaseDn: "dc=x"}).withDefaults()
	if s.Port != 636 || s.GroupAttr != "memberOf" || s.UserFilter == "" || s.Timeout <= 0 {
		t.Fatalf("unexpected defaults: %+v", s)
	}
	if st := (LdapSettings{Host: "h", BaseDn: "dc=x", UseStartTLS: true}).withDefaults(); st.Port != 389 {
		t.Fatalf("StartTLS default port = %d, want 389", st.Port)
	}
}

// objectGUID is stored mixed-endian (MS-DTYP): the first three groups are
// little-endian. The decoded text must match what AD tooling shows.
func TestDecodeObjectGUID_MixedEndian(t *testing.T) {
	raw := []byte{
		0x04, 0x03, 0x02, 0x01,
		0x06, 0x05,
		0x08, 0x07,
		0x09, 0x0a,
		0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10,
	}
	got := decodeObjectGUID(raw)
	want := "01020304-0506-0708-090a-0b0c0d0e0f10"
	if got != want {
		t.Fatalf("decodeObjectGUID = %q, want %q", got, want)
	}
	if decodeObjectGUID([]byte{1, 2, 3}) != "" {
		t.Fatal("short input must decode to empty, not panic or garbage")
	}
}
