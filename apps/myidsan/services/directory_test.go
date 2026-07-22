package services

import (
	"testing"

	myidsanentities "github.com/mysayasan/kopiv2/apps/myidsan/entities"
	"github.com/mysayasan/kopiv2/infra/atrest"
)

func mapping(id int64, group string, roleId int64, priority int64) *myidsanentities.FederatedGroupMapping {
	return &myidsanentities.FederatedGroupMapping{Id: id, Provider: "ldap", GroupName: group, RoleId: roleId, Priority: priority}
}

func TestResolveMappedRole_HighestPriorityWins(t *testing.T) {
	mappings := []*myidsanentities.FederatedGroupMapping{
		mapping(1, "CN=Viewers,DC=corp,DC=local", 3, 10),
		mapping(2, "CN=Admins,DC=corp,DC=local", 1, 100),
	}
	groups := []string{"CN=Admins,DC=corp,DC=local", "CN=Viewers,DC=corp,DC=local"}

	roleId, matched := ResolveMappedRole(mappings, groups)
	if !matched || roleId != 1 {
		t.Fatalf("got (%d, %v), want (1, true)", roleId, matched)
	}
}

func TestResolveMappedRole_CaseInsensitiveAndTieBreak(t *testing.T) {
	mappings := []*myidsanentities.FederatedGroupMapping{
		mapping(5, "cn=ops,dc=corp,dc=local", 4, 50),
		mapping(2, "CN=OPS,DC=CORP,DC=LOCAL", 2, 50), // same priority, lower id -> wins
	}
	roleId, matched := ResolveMappedRole(mappings, []string{"Cn=Ops,Dc=Corp,Dc=Local"})
	if !matched || roleId != 2 {
		t.Fatalf("got (%d, %v), want (2, true)", roleId, matched)
	}
}

// No mapping match must report (0, false) — the caller must leave the account's
// role alone (pending clearance for new accounts), never zero an existing role.
func TestResolveMappedRole_NoMatch(t *testing.T) {
	mappings := []*myidsanentities.FederatedGroupMapping{mapping(1, "CN=Admins,DC=x", 1, 1)}
	if roleId, matched := ResolveMappedRole(mappings, []string{"CN=Other,DC=x"}); matched || roleId != 0 {
		t.Fatalf("got (%d, %v), want (0, false)", roleId, matched)
	}
	if _, matched := ResolveMappedRole(mappings, nil); matched {
		t.Fatal("empty group list must not match")
	}
	// RoleId <= 0 mappings are inert, not "match to no role".
	if _, matched := ResolveMappedRole([]*myidsanentities.FederatedGroupMapping{mapping(1, "CN=X", 0, 9)}, []string{"CN=X"}); matched {
		t.Fatal("mapping with roleId 0 must not match")
	}
}

func TestDirectorySecret_RoundTripAndPassthrough(t *testing.T) {
	key := make([]byte, atrest.KeySize)
	for i := range key {
		key[i] = byte(i)
	}
	cipher, err := atrest.NewCipher(key)
	if err != nil {
		t.Fatalf("NewCipher: %v", err)
	}

	enc, err := encodeDirectorySecret(cipher, "s3cret-bind-pw")
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if enc == "s3cret-bind-pw" {
		t.Fatal("secret stored in plaintext despite cipher present")
	}
	if got := decodeDirectorySecret(cipher, enc); got != "s3cret-bind-pw" {
		t.Fatalf("roundtrip = %q", got)
	}

	// Legacy plaintext (or encryption-disabled installs) must pass through on read.
	if got := decodeDirectorySecret(cipher, "legacy-plaintext"); got != "legacy-plaintext" {
		t.Fatalf("plaintext passthrough = %q", got)
	}
	// Nil cipher: store/read as-is.
	if enc, _ := encodeDirectorySecret(nil, "pw"); enc != "pw" {
		t.Fatalf("nil-cipher encode = %q", enc)
	}
}
