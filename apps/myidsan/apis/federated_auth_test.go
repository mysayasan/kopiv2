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

func TestSecretMatchesSHA256Hash(t *testing.T) {
	hash := "736c6859eceedb2db6b79b2f96d8e53a714ac644d83ee1dd3b52f89ae55cc274"
	if !secretMatches(hash, "dev-myseliasan-secret") {
		t.Fatalf("expected dev secret to match hash")
	}
	if secretMatches(hash, "wrong-secret") {
		t.Fatalf("expected wrong secret to fail")
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
		services.NewUserLoginService(&fakeGenericRepo[entities.UserLogin]{}, cache.NewMemoryStore(time.Minute, time.Minute)),
		&fakeGenericRepo[entities.AppRegistry]{byID: map[uint64]*entities.AppRegistry{uint64(app.Id): app}},
		&fakeGenericRepo[entities.AppAuthConfig]{rows: []*entities.AppAuthConfig{client}},
		&fakeGenericRepo[entities.AppRedirectUri]{rows: []*entities.AppRedirectUri{redirect}},
		cache.NewMemoryStore(time.Minute, time.Minute),
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
