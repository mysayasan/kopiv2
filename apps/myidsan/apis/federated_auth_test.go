package apis

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/myidsan/services"
	"github.com/mysayasan/kopiv2/domain/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	"github.com/mysayasan/kopiv2/domain/utils/middlewares"
	"github.com/mysayasan/kopiv2/infra/cache"
	"github.com/mysayasan/kopiv2/infra/config"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
	"github.com/mysayasan/kopiv2/infra/login"
)

type fakeGenericRepo[T any] struct {
	rows []*T
	byID map[uint64]*T
}

func (f *fakeGenericRepo[T]) Get(ctx context.Context, datasrc string, limit uint64, offset uint64, filters []sqldataenums.Filter, sorter []sqldataenums.Sorter) ([]*T, uint64, error) {
	return f.rows, uint64(len(f.rows)), nil
}

func (f *fakeGenericRepo[T]) GetJoin(ctx context.Context, datasrc string, model any, limit uint64, offset uint64, filters []sqldataenums.Filter, sorter []sqldataenums.Sorter, joinsrc ...string) ([]map[string]any, uint64, error) {
	return nil, 0, nil
}

func (f *fakeGenericRepo[T]) GetJoinWithSpec(ctx context.Context, datasrc string, model any, limit uint64, offset uint64, filters []sqldataenums.Filter, sorter []sqldataenums.Sorter, joins ...dbsql.JoinSpec) ([]map[string]any, uint64, error) {
	return nil, 0, nil
}

func (f *fakeGenericRepo[T]) GetSingle(ctx context.Context, datasrc string, filters []sqldataenums.Filter) (*T, error) {
	if len(f.rows) == 0 {
		return nil, nil
	}
	return f.rows[0], nil
}

func (f *fakeGenericRepo[T]) GetById(ctx context.Context, datasrc string, id uint64) (*T, error) {
	return f.byID[id], nil
}

func (f *fakeGenericRepo[T]) GetByUnique(ctx context.Context, datasrc string, keyGroup string, uids ...any) (*T, error) {
	return f.GetSingle(ctx, datasrc, nil)
}

func (f *fakeGenericRepo[T]) GetByForeign(ctx context.Context, datasrc string, keyGroup string, fids ...any) ([]*T, error) {
	return f.rows, nil
}

func (f *fakeGenericRepo[T]) Create(ctx context.Context, datasrc string, model T) (uint64, error) {
	return 0, nil
}

func (f *fakeGenericRepo[T]) CreateMultiple(ctx context.Context, datasrc string, models []T) (uint64, error) {
	return 0, nil
}

func (f *fakeGenericRepo[T]) UpdateById(ctx context.Context, datasrc string, model T) (uint64, error) {
	return 0, nil
}

func (f *fakeGenericRepo[T]) UpdateByUnique(ctx context.Context, datasrc string, keyGroup string, model T) (uint64, error) {
	return 0, nil
}

func (f *fakeGenericRepo[T]) UpdateByForeign(ctx context.Context, datasrc string, keyGroup string, model T) (uint64, error) {
	return 0, nil
}

func (f *fakeGenericRepo[T]) Delete(ctx context.Context, datasrc string, filters []sqldataenums.Filter) (uint64, error) {
	return 0, nil
}

func (f *fakeGenericRepo[T]) DeleteById(ctx context.Context, datasrc string, id uint64) (uint64, error) {
	return 0, nil
}

func (f *fakeGenericRepo[T]) DeleteByUnique(ctx context.Context, datasrc string, keyGroup string, uids ...any) (uint64, error) {
	return 0, nil
}

func (f *fakeGenericRepo[T]) DeleteByForeign(ctx context.Context, datasrc string, keyGroup string, fids ...any) (uint64, error) {
	return 0, nil
}

// An existing install stores client secrets as unsalted SHA-256. Those rows must keep
// authenticating across the upgrade, and must be flagged for rewriting once the plaintext
// is in hand — otherwise every operator would have to re-enter every client secret.
func TestSecretMatchesAcceptsLegacySHA256AndAsksForRehash(t *testing.T) {
	legacy := "736c6859eceedb2db6b79b2f96d8e53a714ac644d83ee1dd3b52f89ae55cc274"

	ok, needsRehash := secretMatches(legacy, "dev-myseliasan-secret")
	if !ok {
		t.Fatal("a legacy SHA-256 hash must still authenticate")
	}
	if !needsRehash {
		t.Fatal("a legacy hash must be flagged for rewriting to bcrypt")
	}

	if ok, _ := secretMatches(legacy, "wrong-secret"); ok {
		t.Fatal("a wrong secret must not match the legacy hash")
	}
}

// The current form is bcrypt: salted and deliberately slow, so a database read no longer
// yields every operator-chosen client secret at GPU speed.
func TestSecretMatchesVerifiesBcryptAndDoesNotAskForRehash(t *testing.T) {
	hashed, err := hashClientSecret("a-real-client-secret")
	if err != nil {
		t.Fatalf("hashClientSecret: %v", err)
	}
	if !isBcryptClientSecret(hashed) {
		t.Fatalf("new hashes must be bcrypt, got %q", hashed)
	}

	ok, needsRehash := secretMatches(hashed, "a-real-client-secret")
	if !ok {
		t.Fatal("the correct secret must verify against its bcrypt hash")
	}
	if needsRehash {
		t.Fatal("a bcrypt hash must not be flagged for rehashing")
	}

	if ok, _ := secretMatches(hashed, "wrong-secret"); ok {
		t.Fatal("a wrong secret must not verify")
	}
}

// bcrypt salts, so the same secret hashed twice must differ — the property the old
// unsalted scheme lacked, which is what made a stolen table so cheap to crack in bulk.
func TestClientSecretHashesAreSalted(t *testing.T) {
	first, err := hashClientSecret("same-secret")
	if err != nil {
		t.Fatalf("hashClientSecret: %v", err)
	}
	second, err := hashClientSecret("same-secret")
	if err != nil {
		t.Fatalf("hashClientSecret: %v", err)
	}
	if first == second {
		t.Fatal("two hashes of the same secret are identical — the hash is not salted")
	}
	for _, h := range []string{first, second} {
		if ok, _ := secretMatches(h, "same-secret"); !ok {
			t.Fatal("both salted hashes must verify the same secret")
		}
	}
}

func TestSecretMatchesRejectsEmptyInputs(t *testing.T) {
	for _, tc := range []struct{ stored, presented string }{
		{"", "something"}, {"somehash", ""}, {"", ""}, {"   ", "  "},
	} {
		if ok, _ := secretMatches(tc.stored, tc.presented); ok {
			t.Errorf("empty input pair (%q,%q) must not match", tc.stored, tc.presented)
		}
	}
}

func TestCleanContinuePathRejectsExternalURL(t *testing.T) {
	if got := cleanContinuePath("https://evil.example/auth"); got != "/" {
		t.Fatalf("external continue path got %q", got)
	}
	if got := cleanContinuePath("//evil.example/auth"); got != "/" {
		t.Fatalf("network-path continue path got %q", got)
	}
	if got := cleanContinuePath("/api/auth/authorize?client_id=myseliasan"); got != "/api/auth/authorize?client_id=myseliasan" {
		t.Fatalf("relative continue path got %q", got)
	}
}

// Browsers normalise "\" to "/" in the authority position, so a backslash-prefixed value
// used to slip past both the IsAbs() and the "//" checks and then navigate off-origin as
// a protocol-relative URL. Every one of these must collapse to "/".
func TestCleanContinuePathRejectsBackslashBypass(t *testing.T) {
	bypasses := []string{
		`/\evil.example`,
		`/\/evil.example`,
		`\\evil.example`,
		`\/evil.example`,
		`/api/auth\@evil.example`,
	}
	for _, raw := range bypasses {
		if got := cleanContinuePath(raw); got != "/" {
			t.Errorf("backslash bypass %q survived as %q", raw, got)
		}
	}
}

// A value carrying an authority must be refused even when it is not scheme-absolute.
func TestCleanContinuePathRejectsEmbeddedHost(t *testing.T) {
	for _, raw := range []string{"//evil.example", "https://evil.example", "http://evil.example/x"} {
		if got := cleanContinuePath(raw); got != "/" {
			t.Errorf("host-bearing continue path %q survived as %q", raw, got)
		}
	}
}

func TestAuthorizeRedirectsRegisteredClientWithoutSessionToLogin(t *testing.T) {
	cfg := &config.AppConfigModel{}
	cfg.Jwt.Secret = "test-secret"
	cfg.SSO.Issuer = "myidsan"
	cfg.SSO.Audience = "myidsan,myseliasan"
	cfg.SSO.SessionTTLSeconds = 3600

	router := mux.NewRouter()
	api := router.PathPrefix("/api").Subrouter()
	auth := middlewares.NewAuthWithConfig(middlewares.AuthConfig{
		Secret:       cfg.Jwt.Secret,
		Issuer:       cfg.SSO.Issuer,
		Audience:     cfg.SSO.Audience,
		AppCode:      "myidsan",
		SessionCache: cache.NewMemoryStore(time.Minute, time.Minute),
		SessionTTL:   time.Hour,
	})

	app := &entities.AppRegistry{Id: 7, Code: "myseliasan", Audience: "myseliasan", IsActive: true}
	client := &entities.AppAuthConfig{Id: 11, AppRegistryId: app.Id, ClientId: "myseliasan", IsActive: true}
	redirect := &entities.AppRedirectUri{Id: 13, AppAuthConfigId: client.Id, RedirectUri: "http://localhost:3002/api/auth/callback", IsActive: true}

	NewFederatedAuthApi(
		api,
		cfg,
		auth,
		login.NewRegistry(),
		nil, // no directory service: LDAP login disabled in these tests
		"",  // no kerberos label: SSO button off in these tests
		nil, // no login guard: lockout is nil-safe and off in these tests
		services.NewUserLoginService(&fakeGenericRepo[entities.UserLogin]{}, cache.NewMemoryStore(time.Minute, time.Minute), config.PasswordPolicyConfigModel{}.Effective()),
		&fakeGenericRepo[entities.AppRegistry]{byID: map[uint64]*entities.AppRegistry{uint64(app.Id): app}},
		&fakeGenericRepo[entities.AppAuthConfig]{rows: []*entities.AppAuthConfig{client}},
		&fakeGenericRepo[entities.AppRedirectUri]{rows: []*entities.AppRedirectUri{redirect}},
		cache.NewMemoryStore(time.Minute, time.Minute),
		nil, // no MFA service: the challenge path is nil-safe and off in these tests
		nil, // no password-reset service: the forgot/reset paths are nil-safe in tests
		nil, // no metrics recorder: token-exchange counting is nil-safe in tests
		nil, // no security-key service: the challenge path is nil-safe and off in these tests
		nil, // no audit service: the federation trail is nil-safe in tests
		nil, // no session index: indexing is nil-safe, and a nil one leaves the session id
		//      unminted exactly as before this page indexed anything
		nil, // no trusted proxies: the client address is taken straight off the connection
	)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/auth/authorize?response_type=code&client_id=myseliasan&audience=myseliasan&redirect_uri=http%3A%2F%2Flocalhost%3A3002%2Fapi%2Fauth%2Fcallback&state=abc", nil)
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusFound {
		t.Fatalf("status got %d want %d body=%s", rr.Code, http.StatusFound, rr.Body.String())
	}
	location, err := url.Parse(rr.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse login redirect: %v", err)
	}
	if got, want := location.Path, "/api/auth/login"; got != want {
		t.Fatalf("login redirect path got %q want %q", got, want)
	}
	if continueTo := location.Query().Get("continue"); continueTo == "" {
		t.Fatalf("expected continue query in login redirect")
	}
}
