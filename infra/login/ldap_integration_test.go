package login

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

// LDAP integration tests against a real directory server.
//
// myidsan's README shipped saying LDAP login was "not yet live-tested against a real
// directory" — for a headline enterprise feature. These tests close that gap and are
// repeatable, so the claim stays true.
//
// Opt in (the bench is not part of the default suite, same convention as the Redis
// integration test in infra/cache):
//
//	docker run -d --name myidsan-ldap-bench --hostname localhost \
//	  -e LDAP_ORGANISATION="Kopiv2 Bench" -e LDAP_DOMAIN="bench.local" \
//	  -e LDAP_ADMIN_PASSWORD="benchadmin" -e LDAP_TLS_VERIFY_CLIENT="never" \
//	  -p 3389:389 -p 3636:636 osixia/openldap:1.5.0
//
//	RUN_LDAP_IT=1 LDAP_CA_PEM_FILE=/tmp/ldap-ca.pem go test ./infra/login -run Ldap -v
//
// Three things cost time when this bench was first built; all three are silent failures:
//
//  1. --hostname localhost. osixia derives the server certificate's CN/SAN from the
//     container hostname, and myidsan ALWAYS verifies it (there is deliberately no
//     insecure mode), so a cert issued for the container id fails hostname verification
//     from the host. The image's own bundled certificate may also simply be expired —
//     mint one instead and mount it over the certs directory (read-write; the entrypoint
//     chowns it), with LDAP_TLS_{CRT,KEY,CA_CRT}_FILENAME pointing at it:
//
//     openssl req -x509 -newkey rsa:2048 -days 3650 -nodes -keyout ca.key -out ca.crt \
//     -subj "/CN=Kopiv2 Bench CA"
//     openssl req -newkey rsa:2048 -nodes -keyout ldap.key -out ldap.csr -subj "/CN=localhost"
//     printf 'subjectAltName=DNS:localhost,IP:127.0.0.1\nextendedKeyUsage=serverAuth\n' > ext.cnf
//     openssl x509 -req -in ldap.csr -CA ca.crt -CAkey ca.key -CAcreateserial \
//     -out ldap.crt -days 3650 -extfile ext.cnf
//
//  2. Seed the group as groupOfUniqueNames/uniqueMember, NOT groupOfNames/member.
//     osixia configures the memberof overlay with olcMemberOfGroupOC: groupOfUniqueNames
//     and olcMemberOfMemberAD: uniqueMember, so a groupOfNames group is accepted happily
//     and simply never populates memberOf on its members — group -> role mapping then
//     resolves to nothing with no error anywhere.
//
//  3. Group membership is what the directory -> role mapping reads, so assert on it. A
//     directory that authenticates perfectly but returns no groups still leaves every
//     mapped user role-less, and LdapTest reports Ok in that state.
//
// Seed LDIF (pipe to `docker exec -i ... ldapadd -x -D cn=admin,dc=bench,dc=local -w benchadmin`):
//
//	dn: ou=people,dc=bench,dc=local
//	objectClass: organizationalUnit
//	ou: people
//
//	dn: uid=alice,ou=people,dc=bench,dc=local
//	objectClass: inetOrgPerson
//	uid: alice
//	cn: Alice Tan
//	sn: Tan
//	mail: alice@bench.local
//	userPassword: alicepass123
//
//	dn: cn=engineers,dc=bench,dc=local
//	objectClass: groupOfUniqueNames
//	cn: engineers
//	uniqueMember: uid=alice,ou=people,dc=bench,dc=local
func benchSettings(t *testing.T) LdapSettings {
	t.Helper()
	if os.Getenv("RUN_LDAP_IT") == "" {
		t.Skip("set RUN_LDAP_IT=1 (and start the LDAP bench) to run LDAP integration tests")
	}

	port := 3636
	if v := strings.TrimSpace(os.Getenv("LDAP_BENCH_PORT")); v != "" {
		parsed, err := strconv.Atoi(v)
		if err != nil {
			t.Fatalf("LDAP_BENCH_PORT: %v", err)
		}
		port = parsed
	}

	var caPem string
	if path := strings.TrimSpace(os.Getenv("LDAP_CA_PEM_FILE")); path != "" {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read LDAP_CA_PEM_FILE: %v", err)
		}
		caPem = string(raw)
	}

	return LdapSettings{
		Host:         "localhost",
		Port:         port,
		CaCertPem:    caPem,
		BindDn:       "cn=admin,dc=bench,dc=local",
		BindPassword: "benchadmin",
		BaseDn:       "dc=bench,dc=local",
		UserFilter:   "(&(objectClass=inetOrgPerson)(uid=%s))",
		GroupAttr:    "memberOf",
		Timeout:      10 * time.Second,
	}
}

// The headline path: a real user bind over LDAPS against a real directory.
func TestLdapAuthenticateAcceptsValidCredentials(t *testing.T) {
	identity, err := LdapAuthenticate(context.Background(), benchSettings(t), "alice", "alicepass123")
	if err != nil {
		t.Fatalf("LdapAuthenticate: %v", err)
	}
	if identity == nil {
		t.Fatal("nil identity on a successful bind")
	}
	if identity.Email != "alice@bench.local" {
		t.Errorf("Email = %q want alice@bench.local", identity.Email)
	}
	// Subject must be the stable directory id, never the email — rebinding an account by
	// email is the account-takeover pattern the federated path explicitly refuses.
	if strings.TrimSpace(identity.Subject) == "" {
		t.Error("Subject is empty; federated accounts would be unmatchable")
	}
	if identity.Subject == identity.Email {
		t.Error("Subject fell back to the email address instead of a stable directory id")
	}
	if identity.Provider != "ldap" {
		t.Errorf("Provider = %q want ldap", identity.Provider)
	}
}

func TestLdapAuthenticateRejectsWrongPassword(t *testing.T) {
	identity, err := LdapAuthenticate(context.Background(), benchSettings(t), "alice", "not-her-password")
	if err == nil {
		t.Fatal("a wrong password must not authenticate")
	}
	if identity != nil {
		t.Error("identity returned alongside an error")
	}
}

// An empty password is the LDAP unauthenticated-bind trap: RFC 4513 says a simple bind
// with an empty password succeeds as an ANONYMOUS bind rather than failing, so a server
// that is not careful will report "authenticated" for any username at all.
func TestLdapAuthenticateRejectsEmptyPassword(t *testing.T) {
	if _, err := LdapAuthenticate(context.Background(), benchSettings(t), "alice", ""); err == nil {
		t.Fatal("an empty password must be refused, not treated as an anonymous bind")
	}
}

func TestLdapAuthenticateRejectsUnknownUser(t *testing.T) {
	if _, err := LdapAuthenticate(context.Background(), benchSettings(t), "nosuchuser", "whatever"); err == nil {
		t.Fatal("an unknown user must not authenticate")
	}
}

// Group membership drives the directory -> role mapping, so an empty Groups slice here
// silently degrades every mapped user to no role.
func TestLdapAuthenticateReturnsGroupMemberships(t *testing.T) {
	identity, err := LdapAuthenticate(context.Background(), benchSettings(t), "alice", "alicepass123")
	if err != nil {
		t.Fatalf("LdapAuthenticate: %v", err)
	}
	var found bool
	for _, g := range identity.Groups {
		if strings.Contains(strings.ToLower(g), "engineers") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("engineers group missing from %v — directory role mapping would not resolve", identity.Groups)
	}
}

// LdapLookup backs Kerberos principal resolution: it must find the account WITHOUT a user
// bind, so a SPNEGO login lands on the same account a password login reaches.
func TestLdapLookupResolvesWithoutUserBind(t *testing.T) {
	identity, err := LdapLookup(context.Background(), benchSettings(t), "alice")
	if err != nil {
		t.Fatalf("LdapLookup: %v", err)
	}
	if identity == nil || identity.Email != "alice@bench.local" {
		t.Fatalf("lookup returned %+v want alice@bench.local", identity)
	}

	authed, err := LdapAuthenticate(context.Background(), benchSettings(t), "alice", "alicepass123")
	if err != nil {
		t.Fatalf("LdapAuthenticate: %v", err)
	}
	if identity.Subject != authed.Subject {
		t.Errorf("lookup subject %q != authenticate subject %q — Kerberos and password logins "+
			"would create two accounts for one person", identity.Subject, authed.Subject)
	}
}

// The admin "Test connection" probe must report success without binding as a sample user.
func TestLdapTestReportsReachableDirectory(t *testing.T) {
	result := LdapTest(context.Background(), benchSettings(t), "alice")
	if result == nil {
		t.Fatal("nil test result")
	}
	if !result.Ok {
		t.Fatalf("directory test failed: %s", result.Message)
	}
	if result.MatchedDn == "" {
		t.Error("sample username supplied but no entry matched")
	}
	if result.Email != "alice@bench.local" {
		t.Errorf("probe Email = %q want alice@bench.local", result.Email)
	}
	if result.GroupCount == 0 {
		t.Error("probe reported no groups; the role-mapping preview would look empty")
	}
}

// TLS is not optional in this code path — there is deliberately no insecure mode. Pinning
// a CA that did not sign the server certificate must fail closed.
func TestLdapAuthenticateRejectsUntrustedCA(t *testing.T) {
	settings := benchSettings(t)
	if strings.TrimSpace(settings.CaCertPem) == "" {
		t.Skip("set LDAP_CA_PEM_FILE to exercise CA pinning")
	}
	settings.CaCertPem = unrelatedCAPEM

	if _, err := LdapAuthenticate(context.Background(), settings, "alice", "alicepass123"); err == nil {
		t.Fatal("a server certificate signed by an unpinned CA must be refused")
	}
}

// A syntactically valid CA that signed nothing in this bench.
const unrelatedCAPEM = `-----BEGIN CERTIFICATE-----
MIIBhTCCASugAwIBAgIQIRi6zePL6mKjOipn+dNuaTAKBggqhkjOPQQDAjASMRAw
DgYDVQQKEwdBY21lIENvMB4XDTE3MTAyMDE5NDMwNloXDTE4MTAyMDE5NDMwNlow
EjEQMA4GA1UEChMHQWNtZSBDbzBZMBMGByqGSM49AgEGCCqGSM49AwEHA0IABD0d
7VNhbWvZLWPuj/RtHFjvtJBEwOkhbN/BnnE8rnZR8+sbwnc/KhCk3FhnpHZnQz7B
5aETbbIgmuvewdjvSBSjYzBhMA4GA1UdDwEB/wQEAwICpDATBgNVHSUEDDAKBggr
BgEFBQcDATAPBgNVHRMBAf8EBTADAQH/MCkGA1UdEQQiMCCCDmxvY2FsaG9zdDo1
NDUzgg4xMjcuMC4wLjE6NTQ1MzAKBggqhkjOPQQDAgNIADBFAiEA2zpJEPQyz6/l
Wf86aX6PepsntZv2GYlA5UpabfT2EZICICpJ5h/iI+i341gBmLiAFQOyTDT+/wQc
6MF9+Yw1Yy0t
-----END CERTIFICATE-----
`

// StartTLS on 389 is the other transport operators pick, and it is a genuinely different
// code path (plaintext connect, then upgrade) — a working LDAPS says nothing about it.
func TestLdapAuthenticateOverStartTLS(t *testing.T) {
	settings := benchSettings(t)
	settings.UseStartTLS = true
	settings.Port = 3389
	if v := strings.TrimSpace(os.Getenv("LDAP_BENCH_STARTTLS_PORT")); v != "" {
		parsed, err := strconv.Atoi(v)
		if err != nil {
			t.Fatalf("LDAP_BENCH_STARTTLS_PORT: %v", err)
		}
		settings.Port = parsed
	}

	identity, err := LdapAuthenticate(context.Background(), settings, "alice", "alicepass123")
	if err != nil {
		t.Fatalf("StartTLS bind failed: %v", err)
	}
	if identity.Email != "alice@bench.local" {
		t.Errorf("Email = %q want alice@bench.local", identity.Email)
	}
}
