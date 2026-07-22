package services

import (
	"context"
	"testing"

	myidsanentities "github.com/mysayasan/kopiv2/apps/myidsan/entities"
	"github.com/mysayasan/kopiv2/domain/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	"github.com/mysayasan/kopiv2/infra/atrest"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
	"github.com/mysayasan/kopiv2/infra/login"
)

// fakeMappingRepo serves canned FederatedGroupMapping rows for Get; everything
// else is unused by the directory service.
type fakeMappingRepo struct {
	dbsql.IGenericRepo[myidsanentities.FederatedGroupMapping]
	rows []*myidsanentities.FederatedGroupMapping
}

func (f *fakeMappingRepo) Get(_ context.Context, _ string, _ uint64, _ uint64, filters []sqldataenums.Filter, _ []sqldataenums.Sorter) ([]*myidsanentities.FederatedGroupMapping, uint64, error) {
	out := []*myidsanentities.FederatedGroupMapping{}
	for _, row := range f.rows {
		match := true
		for _, filter := range filters {
			if filter.FieldName == "Provider" {
				if s, _ := filter.Value.(string); s != row.Provider {
					match = false
				}
			}
		}
		if match {
			out = append(out, row)
		}
	}
	return out, uint64(len(out)), nil
}

// OIDC (external-IdP) logins: a groups-claim mapping seeds the role for a NEW
// (pending) account, but never overrides an existing manual assignment.
func TestAdmitExternalIdentity_SeedsPendingOnly(t *testing.T) {
	userRepo := newFakeUserLoginRepo()
	users := NewUserLoginService(userRepo, nil)
	mappings := &fakeMappingRepo{rows: []*myidsanentities.FederatedGroupMapping{
		{Id: 1, Provider: "oidc:kc", GroupName: "kopiv2-admins", RoleId: 4, Priority: 1},
	}}
	svc := NewDirectoryService(nil, mappings, users, nil)

	identity := login.Identity{Provider: "oidc:kc", Subject: "sub-9", Email: "bob@corp.local", Groups: []string{"KOPIV2-ADMINS"}}
	user, err := svc.AdmitExternalIdentity(context.Background(), identity)
	if err != nil {
		t.Fatalf("AdmitExternalIdentity: %v", err)
	}
	if user.UserRoleId != 4 {
		t.Fatalf("pending account role = %d, want 4 (seeded from mapping, case-insensitive)", user.UserRoleId)
	}

	// Existing account with a manually assigned role: mapping must NOT touch it.
	userRepo.usersByEmail["carol@corp.local"] = &entities.UserLogin{
		Id: 30, Email: "carol@corp.local", SsoProvider: "oidc:kc", SsoSubject: "sub-30",
		UserRoleId: 9, IsActive: true,
	}
	carol := login.Identity{Provider: "oidc:kc", Subject: "sub-30", Email: "carol@corp.local", Groups: []string{"kopiv2-admins"}}
	user, err = svc.AdmitExternalIdentity(context.Background(), carol)
	if err != nil {
		t.Fatalf("existing account: %v", err)
	}
	if user.UserRoleId != 9 {
		t.Fatalf("manual role overridden to %d, want 9 kept (non-authoritative)", user.UserRoleId)
	}

	// No groups → no mapping call → still pending.
	nogroups := login.Identity{Provider: "oidc:kc", Subject: "sub-11", Email: "dave@corp.local"}
	user, err = svc.AdmitExternalIdentity(context.Background(), nogroups)
	if err != nil || user.UserRoleId != 0 {
		t.Fatalf("groupless identity: role=%d err=%v, want pending", user.UserRoleId, err)
	}
}

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
