package services

import (
	"context"
	"errors"
	"testing"

	"github.com/mysayasan/kopiv2/domain/entities"
	"github.com/mysayasan/kopiv2/infra/login"
)

func googleIdentity() login.Identity {
	return login.Identity{
		Provider:   "google",
		Subject:    "sub-123",
		Email:      "alice@example.com",
		Name:       "Alice Doe",
		GivenName:  "Alice",
		FamilyName: "Doe",
		Picture:    "https://example.com/alice.png",
	}
}

// A full miss creates a pending-clearance account bound to the identity.
func TestUpsertFederated_CreatesPendingAccount(t *testing.T) {
	repo := newFakeUserLoginRepo()
	svc := NewUserLoginService(repo, nil)

	user, err := svc.UpsertFederated(context.Background(), googleIdentity())
	if err != nil {
		t.Fatalf("UpsertFederated: %v", err)
	}
	if user.UserRoleId != 0 {
		t.Errorf("new federated user got role %d, want 0 (pending clearance)", user.UserRoleId)
	}
	if user.SsoProvider != "google" || user.SsoSubject != "sub-123" {
		t.Errorf("identity not bound: provider=%q subject=%q", user.SsoProvider, user.SsoSubject)
	}
	if !user.IsActive || user.Id == 0 {
		t.Errorf("created user not active or missing id: %+v", user)
	}
	if repo.createCount != 1 {
		t.Errorf("createCount = %d, want 1", repo.createCount)
	}
}

// A repeat login matches on (provider, subject) and does not create a second account,
// even when the email at the provider has changed since.
func TestUpsertFederated_MatchesBySubjectNotEmail(t *testing.T) {
	repo := newFakeUserLoginRepo()
	svc := NewUserLoginService(repo, nil)

	first, err := svc.UpsertFederated(context.Background(), googleIdentity())
	if err != nil {
		t.Fatalf("first login: %v", err)
	}

	changed := googleIdentity()
	changed.Email = "alice.new@example.com"
	second, err := svc.UpsertFederated(context.Background(), changed)
	if err != nil {
		t.Fatalf("second login: %v", err)
	}
	if second.Id != first.Id {
		t.Errorf("same identity resolved to a different account: %d then %d", first.Id, second.Id)
	}
	if repo.createCount != 1 {
		t.Errorf("createCount = %d, want 1 (no duplicate account)", repo.createCount)
	}
	if second.Email != "alice.new@example.com" {
		t.Errorf("email not refreshed from provider: %q", second.Email)
	}
}

// A legacy account (same email, no bound identity — pre-upgrade social users) claims
// the identity on first login after the upgrade.
func TestUpsertFederated_LegacyEmailAccountClaimsIdentity(t *testing.T) {
	repo := newFakeUserLoginRepo()
	repo.usersByEmail["alice@example.com"] = &entities.UserLogin{
		Id:         7,
		Email:      "alice@example.com",
		UserRoleId: 3,
		IsActive:   true,
	}
	svc := NewUserLoginService(repo, nil)

	user, err := svc.UpsertFederated(context.Background(), googleIdentity())
	if err != nil {
		t.Fatalf("UpsertFederated: %v", err)
	}
	if user.Id != 7 {
		t.Errorf("resolved to account %d, want legacy account 7", user.Id)
	}
	if user.SsoProvider != "google" || user.SsoSubject != "sub-123" {
		t.Errorf("legacy account did not claim identity: provider=%q subject=%q", user.SsoProvider, user.SsoSubject)
	}
	if user.UserRoleId != 3 {
		t.Errorf("legacy account role changed to %d, want 3 kept", user.UserRoleId)
	}
	if repo.createCount != 0 {
		t.Errorf("createCount = %d, want 0", repo.createCount)
	}
}

// SECURITY: an email match already bound to a DIFFERENT identity is refused — merging
// would let a same-email account at another provider take over this one.
func TestUpsertFederated_RefusesEmailTakeover(t *testing.T) {
	repo := newFakeUserLoginRepo()
	repo.usersByEmail["alice@example.com"] = &entities.UserLogin{
		Id:          7,
		Email:       "alice@example.com",
		SsoProvider: "github",
		SsoSubject:  "999",
		UserRoleId:  5,
		IsActive:    true,
	}
	svc := NewUserLoginService(repo, nil)

	_, err := svc.UpsertFederated(context.Background(), googleIdentity())
	if !errors.Is(err, ErrFederatedIdentityConflict) {
		t.Fatalf("err = %v, want ErrFederatedIdentityConflict", err)
	}
	if repo.createCount != 0 || repo.updateCount != 0 {
		t.Errorf("takeover attempt mutated store: creates=%d updates=%d", repo.createCount, repo.updateCount)
	}
}

// Inactive accounts are refused even when the identity matches.
func TestUpsertFederated_RefusesInactiveAccount(t *testing.T) {
	repo := newFakeUserLoginRepo()
	repo.usersByEmail["alice@example.com"] = &entities.UserLogin{
		Id:          7,
		Email:       "alice@example.com",
		SsoProvider: "google",
		SsoSubject:  "sub-123",
		IsActive:    false,
	}
	svc := NewUserLoginService(repo, nil)

	_, err := svc.UpsertFederated(context.Background(), googleIdentity())
	if !errors.Is(err, ErrInactiveAccount) {
		t.Fatalf("err = %v, want ErrInactiveAccount", err)
	}
}

// An identity without a stable subject (or email) is refused outright.
func TestUpsertFederated_RefusesIncompleteIdentity(t *testing.T) {
	svc := NewUserLoginService(newFakeUserLoginRepo(), nil)

	for _, id := range []login.Identity{
		{Provider: "google", Email: "a@b.c"},           // no subject
		{Provider: "google", Subject: "sub-1"},         // no email
		{Subject: "sub-1", Email: "a@b.c"},             // no provider
		{Provider: " ", Subject: "  ", Email: "a@b.c"}, // whitespace only
	} {
		if _, err := svc.UpsertFederated(context.Background(), id); !errors.Is(err, ErrFederatedIdentityInvalid) {
			t.Errorf("identity %+v: err = %v, want ErrFederatedIdentityInvalid", id, err)
		}
	}
}
