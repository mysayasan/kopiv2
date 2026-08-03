package services

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	myidsanentities "github.com/mysayasan/kopiv2/apps/myidsan/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	"github.com/mysayasan/kopiv2/infra/cache"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
	wa "github.com/mysayasan/kopiv2/infra/webauthn"
)

// fakeWebAuthnRepo is an in-memory IGenericRepo for the credential table. It follows
// fakeAuditRepo's shape (embed the interface, implement only the calls under test) rather
// than fakeBackupRepo's, because this service's correctness DEPENDS on the UserLoginId
// filter: fakeBackupRepo ignores filters and would hand every user's rows to every caller,
// which is precisely the ownership bug these tests exist to catch.
type fakeWebAuthnRepo struct {
	dbsql.IGenericRepo[myidsanentities.UserWebauthnCredential]
	rows   []*myidsanentities.UserWebauthnCredential
	nextID int64

	// getErr simulates a database fault on read so the fail-closed behaviour can be checked.
	getErr error
	// notFoundErr simulates the repo's "no result found" miss, which the service treats as
	// an empty set rather than an error.
	notFoundErr bool

	updates int
	deletes int
}

func (f *fakeWebAuthnRepo) Get(_ context.Context, _ string, limit uint64, offset uint64, filters []sqldataenums.Filter, _ []sqldataenums.Sorter) ([]*myidsanentities.UserWebauthnCredential, uint64, error) {
	if f.notFoundErr {
		return nil, 0, errors.New("select failed: no result found")
	}
	if f.getErr != nil {
		return nil, 0, f.getErr
	}

	// Honour exactly the filter shape loadRows issues: UserLoginId Equal <int64>.
	var (
		userID    int64
		filtering bool
	)
	for _, filter := range filters {
		if filter.FieldName == "UserLoginId" && filter.Compare == sqldataenums.Equal {
			if v, ok := filter.Value.(int64); ok {
				userID = v
				filtering = true
			}
		}
	}

	matched := make([]*myidsanentities.UserWebauthnCredential, 0, len(f.rows))
	for _, row := range f.rows {
		if filtering && row.UserLoginId != userID {
			continue
		}
		cp := *row
		matched = append(matched, &cp)
	}

	total := uint64(len(matched))
	if offset > total {
		offset = total
	}
	page := matched[offset:]
	if limit > 0 && uint64(len(page)) > limit {
		page = page[:limit]
	}
	return page, total, nil
}

func (f *fakeWebAuthnRepo) Create(_ context.Context, _ string, model myidsanentities.UserWebauthnCredential) (uint64, error) {
	f.nextID++
	model.Id = f.nextID
	cp := model
	f.rows = append(f.rows, &cp)
	return uint64(f.nextID), nil
}

func (f *fakeWebAuthnRepo) UpdateById(_ context.Context, _ string, model myidsanentities.UserWebauthnCredential) (uint64, error) {
	f.updates++
	for i, row := range f.rows {
		if row.Id == model.Id {
			cp := model
			f.rows[i] = &cp
			return 1, nil
		}
	}
	return 0, nil
}

func (f *fakeWebAuthnRepo) DeleteById(_ context.Context, _ string, id uint64) (uint64, error) {
	f.deletes++
	for i, row := range f.rows {
		if uint64(row.Id) == id {
			f.rows = append(f.rows[:i], f.rows[i+1:]...)
			return 1, nil
		}
	}
	return 0, nil
}

// byId is a read helper for assertions; the service never exposes a row directly.
func (f *fakeWebAuthnRepo) byId(id int64) *myidsanentities.UserWebauthnCredential {
	for _, row := range f.rows {
		if row.Id == id {
			return row
		}
	}
	return nil
}

// seedCredential adds one enrolled key. The credential id and public key are base64 in the
// encodings the service uses (raw-url and std respectively) so buildUser can decode them —
// a row it cannot decode is silently skipped, which would mask a genuine failure.
func (f *fakeWebAuthnRepo) seedCredential(userID int64, label string) *myidsanentities.UserWebauthnCredential {
	f.nextID++
	row := &myidsanentities.UserWebauthnCredential{
		Id:           f.nextID,
		UserLoginId:  userID,
		CredentialId: base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf("cred-%d", f.nextID))),
		PublicKey:    base64.StdEncoding.EncodeToString([]byte("cose-public-key")),
		Aaguid:       base64.StdEncoding.EncodeToString(make([]byte, 16)),
		Label:        label,
		Transports:   "usb,nfc",
		SignCount:    7,
		CreatedAt:    1700000000,
		LastUsedAt:   1700000100,
	}
	f.rows = append(f.rows, row)
	return row
}

// newWebAuthnHarness wires the service the way the composition root does: a real in-memory
// cache (the same one the session tests use) and an enabled Authority, so ceremony state
// genuinely round-trips through JSON as it would in Redis.
func newWebAuthnHarness(t *testing.T) (*webauthnService, *fakeWebAuthnRepo) {
	t.Helper()
	repo := &fakeWebAuthnRepo{}
	svc := NewWebAuthnService(
		repo,
		cache.NewMemoryStore(time.Minute, time.Minute),
		wa.New(wa.Settings{Enabled: true, RelyingPartyName: "MyIDSan test"}),
		60*time.Second,
	).(*webauthnService)
	return svc, repo
}

// webauthnRequest builds the request the ceremony's RP ID and origin are derived from.
func webauthnRequest() *http.Request {
	r := httptest.NewRequest(http.MethodPost, "http://localhost:3011/api/webauthn/enroll", nil)
	r.Host = "localhost:3011"
	return r
}

// Every one of the three collaborators is load-bearing, and a missing one must switch the
// feature OFF rather than half-work. Without a store there is nowhere to keep the challenge
// between the two legs, so an "assertion" would be verified against a challenge nobody
// issued — the one failure mode that must not be reachable.
func TestWebAuthnEnabledRequiresEveryCollaborator(t *testing.T) {
	store := cache.NewMemoryStore(time.Minute, time.Minute)
	authority := wa.New(wa.Settings{Enabled: true})

	cases := []struct {
		name string
		svc  IWebAuthnService
		want bool
	}{
		{"all present", NewWebAuthnService(&fakeWebAuthnRepo{}, store, authority, time.Minute), true},
		{"nil repo", NewWebAuthnService(nil, store, authority, time.Minute), false},
		{"nil store", NewWebAuthnService(&fakeWebAuthnRepo{}, nil, authority, time.Minute), false},
		{"disabled authority", NewWebAuthnService(&fakeWebAuthnRepo{}, store, wa.New(wa.Settings{}), time.Minute), false},
		// A nil Authority is what the composition root produces when the config block is
		// absent in a build that skips construction; Enabled() must not panic on it.
		{"nil authority", NewWebAuthnService(&fakeWebAuthnRepo{}, store, nil, time.Minute), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.svc.Enabled(); got != tc.want {
				t.Fatalf("Enabled() = %v, want %v", got, tc.want)
			}
		})
	}
}

// A disabled service must refuse every ceremony explicitly. Returning a zero value with no
// error would let the API layer report success for an enrolment that never happened.
func TestWebAuthnDisabledRefusesEveryCeremony(t *testing.T) {
	ctx := context.Background()
	svc := NewWebAuthnService(&fakeWebAuthnRepo{}, nil, wa.New(wa.Settings{}), time.Minute)
	r := webauthnRequest()

	if _, err := svc.BeginEnroll(ctx, r, "state", 1, "a@b.c", "A"); !errors.Is(err, ErrWebAuthnDisabled) {
		t.Errorf("BeginEnroll error = %v, want ErrWebAuthnDisabled", err)
	}
	if _, err := svc.FinishEnroll(ctx, r, "state", 1, "key", strings.NewReader("{}")); !errors.Is(err, ErrWebAuthnDisabled) {
		t.Errorf("FinishEnroll error = %v, want ErrWebAuthnDisabled", err)
	}
	if _, err := svc.BeginAssert(ctx, r, "state", 1, "a@b.c", "A"); !errors.Is(err, ErrWebAuthnDisabled) {
		t.Errorf("BeginAssert error = %v, want ErrWebAuthnDisabled", err)
	}
	if ok, _, err := svc.FinishAssert(ctx, r, "state", 1, "a@b.c", "A", strings.NewReader("{}")); ok || !errors.Is(err, ErrWebAuthnDisabled) {
		t.Errorf("FinishAssert = (%v, %v), want (false, ErrWebAuthnDisabled)", ok, err)
	}
}

// HasCredential is the login-gate predicate for this factor, asked on every password login.
// Its error behaviour is the security-relevant part: a database fault must PROPAGATE, because
// swallowing it into "false" would silently downgrade every account to password-only for the
// duration of the fault — an outage turning into an authentication bypass.
func TestWebAuthnHasCredentialFailsClosedOnRepoError(t *testing.T) {
	ctx := context.Background()

	t.Run("disabled reports no factor without touching the repo", func(t *testing.T) {
		// Off means off: an account cannot owe a factor the server will not verify.
		repo := &fakeWebAuthnRepo{}
		repo.seedCredential(1, "YubiKey")
		svc := NewWebAuthnService(repo, cache.NewMemoryStore(time.Minute, time.Minute), wa.New(wa.Settings{}), time.Minute)

		has, err := svc.HasCredential(ctx, 1)
		if err != nil {
			t.Fatalf("HasCredential: %v", err)
		}
		if has {
			t.Fatal("a disabled service must not claim the account owes a security-key factor")
		}
	})

	t.Run("true only for the owning user", func(t *testing.T) {
		svc, repo := newWebAuthnHarness(t)
		repo.seedCredential(1, "YubiKey")

		if has, err := svc.HasCredential(ctx, 1); err != nil || !has {
			t.Fatalf("HasCredential(owner) = (%v, %v), want (true, nil)", has, err)
		}
		// Another account's key must not answer for this one, or the gate would demand a
		// factor from a user who holds none (and could never satisfy it).
		if has, err := svc.HasCredential(ctx, 2); err != nil || has {
			t.Fatalf("HasCredential(other user) = (%v, %v), want (false, nil)", has, err)
		}
	})

	t.Run("repo error propagates", func(t *testing.T) {
		svc, repo := newWebAuthnHarness(t)
		repo.seedCredential(1, "YubiKey")
		repo.getErr = errors.New("connection refused")

		has, err := svc.HasCredential(ctx, 1)
		if err == nil {
			t.Fatal("a database fault must not read as \"this account has no second factor\"")
		}
		if has {
			t.Fatal("HasCredential returned true alongside an error")
		}
	})

	t.Run("a genuine miss is not an error", func(t *testing.T) {
		// The repo signals "nothing matched" with an error string, not an empty slice. That
		// must resolve to false/nil, or every user without a key would fail their login.
		svc, repo := newWebAuthnHarness(t)
		repo.notFoundErr = true

		if has, err := svc.HasCredential(ctx, 1); err != nil || has {
			t.Fatalf("HasCredential on a miss = (%v, %v), want (false, nil)", has, err)
		}
	})
}

// List is what the account's "security keys" screen renders. It must project the row without
// the public key (useless to a client, and only invites someone to try trusting it) and it
// must return an empty slice rather than nil so the JSON is [] and not null.
func TestWebAuthnListProjectsRows(t *testing.T) {
	ctx := context.Background()
	svc, repo := newWebAuthnHarness(t)

	t.Run("no rows yields an empty list", func(t *testing.T) {
		views, err := svc.List(ctx, 1)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(views) != 0 {
			t.Fatalf("List = %v, want empty", views)
		}
		if views == nil {
			t.Fatal("List returned nil; the JSON would be null rather than []")
		}
	})

	row := repo.seedCredential(1, "YubiKey 5 — desk drawer")
	row.BackedUp = true
	row.CloneWarning = true
	repo.seedCredential(2, "someone else's key")

	t.Run("rows map to the view", func(t *testing.T) {
		views, err := svc.List(ctx, 1)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(views) != 1 {
			t.Fatalf("List returned %d views, want only user 1's key", len(views))
		}
		got := views[0]
		if got.Id != row.Id || got.Label != row.Label || got.Transports != row.Transports ||
			got.Aaguid != row.Aaguid || got.SignCount != row.SignCount ||
			got.CreatedAt != row.CreatedAt || got.LastUsedAt != row.LastUsedAt {
			t.Fatalf("view = %+v, does not match row %+v", got, *row)
		}
		// Both flags drive operator-visible warnings ("this is a synced passkey", "this key
		// may have been cloned"), so losing them in the projection would hide the warning.
		if !got.BackedUp {
			t.Error("BackedUp was dropped in the projection")
		}
		if !got.CloneWarning {
			t.Error("CloneWarning was dropped in the projection — the clone signal would never surface")
		}
	})

	t.Run("a repo error is not swallowed into an empty list", func(t *testing.T) {
		repo.getErr = errors.New("connection refused")
		defer func() { repo.getErr = nil }()
		if _, err := svc.List(ctx, 1); err == nil {
			t.Fatal("List must surface a read failure rather than render \"you have no keys\"")
		}
	})
}

// The cap bounds the exclusion list sent to the browser and keeps an account's key set
// reviewable. It has to be enforced BEFORE a challenge is issued, otherwise the user touches
// their key and only then learns it was refused.
func TestWebAuthnBeginEnrollEnforcesTheKeyCap(t *testing.T) {
	ctx := context.Background()
	svc, repo := newWebAuthnHarness(t)

	for i := 0; i < webauthnMaxKeys-1; i++ {
		repo.seedCredential(1, fmt.Sprintf("key-%d", i))
	}

	// One slot left: the ceremony must still start, and must exclude the keys already held
	// so re-presenting one of them fails in the browser instead of creating a duplicate row.
	creation, err := svc.BeginEnroll(ctx, webauthnRequest(), "state-1", 1, "alice@corp.local", "Alice")
	if err != nil {
		t.Fatalf("BeginEnroll with a free slot: %v", err)
	}
	if len(creation.Response.CredentialExcludeList) != webauthnMaxKeys-1 {
		t.Errorf("exclude list has %d entries, want the %d keys already enrolled",
			len(creation.Response.CredentialExcludeList), webauthnMaxKeys-1)
	}

	repo.seedCredential(1, "the last one")
	if _, err := svc.BeginEnroll(ctx, webauthnRequest(), "state-2", 1, "alice@corp.local", "Alice"); err == nil {
		t.Fatalf("BeginEnroll must refuse a %dth key", webauthnMaxKeys+1)
	} else if !strings.Contains(err.Error(), "remove one first") {
		t.Errorf("the refusal should tell the user what to do, got: %v", err)
	}

	// Another account is unaffected by this user's full keyring.
	if _, err := svc.BeginEnroll(ctx, webauthnRequest(), "state-3", 2, "bob@corp.local", "Bob"); err != nil {
		t.Errorf("one user hitting the cap blocked another user's enrolment: %v", err)
	}
}

// The ceremony state is the challenge. It must be SINGLE-USE, or a response captured off the
// wire could be replayed against the same challenge — the replay defence for the whole
// feature. takeCeremony is internal, so this drives it through FinishEnroll: the first call
// consumes the state (and then fails on the deliberately-bogus attestation body, which is a
// DIFFERENT error), and the second must find no ceremony at all.
func TestWebAuthnCeremonyStateIsSingleUse(t *testing.T) {
	ctx := context.Background()
	svc, _ := newWebAuthnHarness(t)

	if _, err := svc.BeginEnroll(ctx, webauthnRequest(), "state-1", 1, "alice@corp.local", "Alice"); err != nil {
		t.Fatalf("BeginEnroll: %v", err)
	}

	// First redemption: the ceremony is found (so the failure is a parse/verify failure, not
	// a missing-ceremony one) and consumed.
	_, err := svc.FinishEnroll(ctx, webauthnRequest(), "state-1", 1, "YubiKey", strings.NewReader(`{"bogus":true}`))
	if err == nil {
		t.Fatal("a bogus attestation body must not be accepted")
	}
	if errors.Is(err, ErrWebAuthnNoCeremony) {
		t.Fatal("the first FinishEnroll did not find the ceremony BeginEnroll stored")
	}

	// Second redemption of the same challenge must find nothing.
	if _, err := svc.FinishEnroll(ctx, webauthnRequest(), "state-1", 1, "YubiKey", strings.NewReader(`{"bogus":true}`)); !errors.Is(err, ErrWebAuthnNoCeremony) {
		t.Fatalf("second FinishEnroll error = %v, want ErrWebAuthnNoCeremony — the challenge is replayable", err)
	}
}

// A challenge issued to one account must not be redeemable by another. Without the UserId
// check, an attacker who could reach the finish endpoint with a victim's state key would
// enrol their own authenticator onto the victim's account (or satisfy the victim's second
// factor with their own key).
func TestWebAuthnCeremonyIsBoundToTheIssuingUser(t *testing.T) {
	ctx := context.Background()
	svc, _ := newWebAuthnHarness(t)

	if _, err := svc.BeginEnroll(ctx, webauthnRequest(), "shared-state", 1, "alice@corp.local", "Alice"); err != nil {
		t.Fatalf("BeginEnroll: %v", err)
	}

	// User 2 presenting user 1's state key must be refused as "no ceremony" — the same
	// answer as a missing one, so nothing is learned about whether a ceremony exists.
	if _, err := svc.FinishEnroll(ctx, webauthnRequest(), "shared-state", 2, "attacker key", strings.NewReader(`{"bogus":true}`)); !errors.Is(err, ErrWebAuthnNoCeremony) {
		t.Fatalf("a ceremony issued to user 1 was redeemable by user 2: err = %v", err)
	}

	// Same for the assertion leg.
	if _, err := svc.BeginEnroll(ctx, webauthnRequest(), "assert-state", 1, "alice@corp.local", "Alice"); err != nil {
		t.Fatalf("BeginEnroll: %v", err)
	}
	if ok, _, err := svc.FinishAssert(ctx, webauthnRequest(), "assert-state", 2, "bob@corp.local", "Bob", strings.NewReader(`{"bogus":true}`)); ok || !errors.Is(err, ErrWebAuthnNoCeremony) {
		t.Fatalf("FinishAssert across users = (%v, %v), want (false, ErrWebAuthnNoCeremony)", ok, err)
	}
}

// A finish call with no matching state key at all must be refused rather than falling through
// to verification against a zero SessionData (whose empty challenge could match a crafted
// response).
func TestWebAuthnFinishWithoutABeginIsRefused(t *testing.T) {
	ctx := context.Background()
	svc, repo := newWebAuthnHarness(t)
	repo.seedCredential(1, "YubiKey")

	if _, err := svc.FinishEnroll(ctx, webauthnRequest(), "never-issued", 1, "key", strings.NewReader(`{}`)); !errors.Is(err, ErrWebAuthnNoCeremony) {
		t.Errorf("FinishEnroll error = %v, want ErrWebAuthnNoCeremony", err)
	}
	if ok, _, err := svc.FinishAssert(ctx, webauthnRequest(), "never-issued", 1, "a@b.c", "A", strings.NewReader(`{}`)); ok || !errors.Is(err, ErrWebAuthnNoCeremony) {
		t.Errorf("FinishAssert = (%v, %v), want (false, ErrWebAuthnNoCeremony)", ok, err)
	}
}

// An assertion can only be attempted against a key the account actually holds; otherwise the
// challenge would allow any credential to answer.
func TestWebAuthnBeginAssertRequiresAnEnrolledKey(t *testing.T) {
	ctx := context.Background()
	svc, repo := newWebAuthnHarness(t)

	if _, err := svc.BeginAssert(ctx, webauthnRequest(), "state", 1, "alice@corp.local", "Alice"); !errors.Is(err, ErrWebAuthnNoKeys) {
		t.Fatalf("BeginAssert with no keys = %v, want ErrWebAuthnNoKeys", err)
	}

	repo.seedCredential(1, "YubiKey")
	assertion, err := svc.BeginAssert(ctx, webauthnRequest(), "state", 1, "alice@corp.local", "Alice")
	if err != nil {
		t.Fatalf("BeginAssert: %v", err)
	}
	if len(assertion.Response.AllowedCredentials) != 1 {
		t.Fatalf("allowed credentials = %d, want the account's single key", len(assertion.Response.AllowedCredentials))
	}
	if assertion.Response.RelyingPartyID != "localhost" {
		t.Errorf("assertion RP ID = %q, want the derived localhost", assertion.Response.RelyingPartyID)
	}
}

// One unreadable row must not lock a user out of the other keys they hold — losing access to
// every key because a single column got corrupted is a far worse outcome than ignoring the
// bad row. A user whose ONLY row is undecodable, though, genuinely has no usable key and must
// be told so rather than handed an assertion no credential can answer.
func TestWebAuthnUndecodableRowsAreSkippedNotFatal(t *testing.T) {
	ctx := context.Background()
	svc, repo := newWebAuthnHarness(t)

	corrupt := repo.seedCredential(1, "corrupt")
	corrupt.CredentialId = "!!! not base64 !!!"
	repo.seedCredential(1, "good key")

	assertion, err := svc.BeginAssert(ctx, webauthnRequest(), "state", 1, "alice@corp.local", "Alice")
	if err != nil {
		t.Fatalf("one corrupt row broke the whole ceremony: %v", err)
	}
	if len(assertion.Response.AllowedCredentials) != 1 {
		t.Fatalf("allowed credentials = %d, want just the decodable key", len(assertion.Response.AllowedCredentials))
	}

	onlyCorrupt := &fakeWebAuthnRepo{}
	bad := onlyCorrupt.seedCredential(1, "corrupt")
	bad.PublicKey = "!!! not base64 !!!"
	svcOnlyCorrupt := NewWebAuthnService(onlyCorrupt, cache.NewMemoryStore(time.Minute, time.Minute),
		wa.New(wa.Settings{Enabled: true}), time.Minute)
	if _, err := svcOnlyCorrupt.BeginAssert(ctx, webauthnRequest(), "state", 1, "a@b.c", "A"); !errors.Is(err, ErrWebAuthnNoKeys) {
		t.Fatalf("BeginAssert with only an undecodable key = %v, want ErrWebAuthnNoKeys", err)
	}
}

// Rename is the only field a user can edit, and it matters more than it looks: the revoke
// button is useless if every row reads "Security key". Hence the empty-label refusal.
func TestWebAuthnRename(t *testing.T) {
	ctx := context.Background()

	t.Run("empty label is refused", func(t *testing.T) {
		svc, repo := newWebAuthnHarness(t)
		row := repo.seedCredential(1, "original")

		for _, label := range []string{"", "   ", "\t\n"} {
			if err := svc.Rename(ctx, 1, row.Id, label); err == nil {
				t.Errorf("Rename accepted the blank label %q", label)
			}
		}
		if repo.byId(row.Id).Label != "original" {
			t.Error("a refused rename still wrote to the row")
		}
	})

	t.Run("label is trimmed and persisted", func(t *testing.T) {
		svc, repo := newWebAuthnHarness(t)
		row := repo.seedCredential(1, "original")

		if err := svc.Rename(ctx, 1, row.Id, "  YubiKey 5 — safe  "); err != nil {
			t.Fatalf("Rename: %v", err)
		}
		if got := repo.byId(row.Id).Label; got != "YubiKey 5 — safe" {
			t.Fatalf("label = %q, want it trimmed", got)
		}
		if repo.byId(row.Id).UpdatedBy != 1 {
			t.Error("UpdatedBy must record who renamed the key")
		}
	})

	t.Run("an over-long label is truncated, not refused", func(t *testing.T) {
		// The column is bounded, so an over-long label has to be dealt with here rather than
		// as a database error the user cannot act on.
		svc, repo := newWebAuthnHarness(t)
		row := repo.seedCredential(1, "original")

		if err := svc.Rename(ctx, 1, row.Id, strings.Repeat("x", 500)); err != nil {
			t.Fatalf("Rename: %v", err)
		}
		if got := len(repo.byId(row.Id).Label); got != 120 {
			t.Fatalf("label length = %d, want it truncated to 120", got)
		}
	})

	t.Run("another user's key cannot be renamed", func(t *testing.T) {
		// Ownership is enforced in the service, not trusted from the route, so a caller
		// cannot retitle (or, below, revoke) someone else's key by guessing a row id.
		svc, repo := newWebAuthnHarness(t)
		victim := repo.seedCredential(1, "alice's key")

		if err := svc.Rename(ctx, 2, victim.Id, "pwned"); !errors.Is(err, ErrWebAuthnNotFound) {
			t.Fatalf("cross-user Rename = %v, want ErrWebAuthnNotFound", err)
		}
		if repo.byId(victim.Id).Label != "alice's key" {
			t.Fatal("a cross-user rename modified the victim's row")
		}
		if repo.updates != 0 {
			t.Fatal("a cross-user rename reached the database")
		}
	})

	t.Run("a missing row is not found", func(t *testing.T) {
		svc, repo := newWebAuthnHarness(t)
		repo.seedCredential(1, "alice's key")
		if err := svc.Rename(ctx, 1, 9999, "nope"); !errors.Is(err, ErrWebAuthnNotFound) {
			t.Fatalf("Rename of a missing row = %v, want ErrWebAuthnNotFound", err)
		}
	})
}

// Delete is credential revocation. Deleting the wrong user's key is a denial of service
// against their account, so the same ownership check applies as for Rename.
func TestWebAuthnDelete(t *testing.T) {
	ctx := context.Background()

	t.Run("the owner can revoke", func(t *testing.T) {
		svc, repo := newWebAuthnHarness(t)
		row := repo.seedCredential(1, "YubiKey")

		if err := svc.Delete(ctx, 1, row.Id); err != nil {
			t.Fatalf("Delete: %v", err)
		}
		if repo.byId(row.Id) != nil {
			t.Fatal("the row survived a successful Delete")
		}
	})

	t.Run("another user's key cannot be revoked", func(t *testing.T) {
		svc, repo := newWebAuthnHarness(t)
		victim := repo.seedCredential(1, "alice's key")

		if err := svc.Delete(ctx, 2, victim.Id); !errors.Is(err, ErrWebAuthnNotFound) {
			t.Fatalf("cross-user Delete = %v, want ErrWebAuthnNotFound", err)
		}
		if repo.byId(victim.Id) == nil {
			t.Fatal("a cross-user Delete revoked the victim's key")
		}
		if repo.deletes != 0 {
			t.Fatal("a cross-user Delete reached the database")
		}
	})
}

// DeleteAllForUser is the superadmin lost-device reset and the account-deletion path. It must
// clear the target account completely — a key left behind still satisfies the factor — and
// touch nobody else, since this runs on an operator's behalf against an account they named.
func TestWebAuthnDeleteAllForUser(t *testing.T) {
	ctx := context.Background()
	svc, repo := newWebAuthnHarness(t)

	repo.seedCredential(1, "alice A")
	repo.seedCredential(1, "alice B")
	repo.seedCredential(1, "alice C")
	bobKey := repo.seedCredential(2, "bob A")

	if err := svc.DeleteAllForUser(ctx, 1); err != nil {
		t.Fatalf("DeleteAllForUser: %v", err)
	}
	if has, err := svc.HasCredential(ctx, 1); err != nil || has {
		t.Fatalf("user 1 still owes a security-key factor after a full reset: (%v, %v)", has, err)
	}
	if repo.byId(bobKey.Id) == nil {
		t.Fatal("another account's key was deleted by a reset targeting user 1")
	}

	// Idempotent: the lost-device path may well be run twice.
	if err := svc.DeleteAllForUser(ctx, 1); err != nil {
		t.Fatalf("second DeleteAllForUser: %v", err)
	}

	// With no repo at all there is nothing to clear, and the account-deletion path must not
	// fail because the feature is not wired.
	if err := NewWebAuthnService(nil, nil, nil, time.Minute).DeleteAllForUser(ctx, 1); err != nil {
		t.Fatalf("DeleteAllForUser with no repo = %v, want nil", err)
	}
}

// A read failure during a reset must abort rather than report success: "we removed all your
// keys" when nothing was removed would leave an operator believing a lost authenticator had
// been revoked.
func TestWebAuthnDeleteAllForUserSurfacesReadFailure(t *testing.T) {
	svc, repo := newWebAuthnHarness(t)
	repo.seedCredential(1, "YubiKey")
	repo.getErr = errors.New("connection refused")

	if err := svc.DeleteAllForUser(context.Background(), 1); err == nil {
		t.Fatal("DeleteAllForUser reported success while it could not even read the rows")
	}
}

// The user handle is baked into every credential the account creates, so it must be STABLE
// for the account's lifetime: change the derivation and every enrolled key stops matching its
// owner. It must also differ between accounts, or one user's key would resolve to another.
func TestWebAuthnUserHandleIsStableAndPerAccount(t *testing.T) {
	first := (&webauthnUser{id: 42}).WebAuthnID()
	second := (&webauthnUser{id: 42}).WebAuthnID()
	if string(first) != string(second) {
		t.Fatal("the user handle is not stable for the same account")
	}
	if len(first) != 8 {
		t.Fatalf("user handle is %d bytes; the WebAuthn spec bounds it to 1..64", len(first))
	}
	if string((&webauthnUser{id: 43}).WebAuthnID()) == string(first) {
		t.Fatal("two accounts derive the same user handle")
	}

	// The display name falls back to the email so an authenticator prompt never shows a
	// blank account.
	if got := (&webauthnUser{email: "alice@corp.local"}).WebAuthnDisplayName(); got != "alice@corp.local" {
		t.Errorf("display name = %q, want the email fallback", got)
	}
	if got := (&webauthnUser{email: "alice@corp.local", display: "  "}).WebAuthnDisplayName(); got != "alice@corp.local" {
		t.Errorf("a blank display name resolved to %q, want the email fallback", got)
	}
	if got := (&webauthnUser{email: "alice@corp.local", display: "Alice"}).WebAuthnDisplayName(); got != "Alice" {
		t.Errorf("display name = %q, want the supplied value", got)
	}
}
