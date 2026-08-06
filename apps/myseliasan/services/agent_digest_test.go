package services

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	appentities "github.com/mysayasan/kopiv2/apps/myseliasan/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	"github.com/mysayasan/kopiv2/domain/notification"
	"github.com/mysayasan/kopiv2/infra/config"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// in-memory fake of the AgentDigest repo.
type fakeDigestRepo struct {
	dbsql.IGenericRepo[appentities.AgentDigest]
	rows   []*appentities.AgentDigest
	nextID int64
}

func (f *fakeDigestRepo) Create(_ context.Context, _ string, m appentities.AgentDigest) (uint64, error) {
	f.nextID++
	m.Id = f.nextID
	cp := m
	f.rows = append(f.rows, &cp)
	return uint64(f.nextID), nil
}

func (f *fakeDigestRepo) Get(_ context.Context, _ string, limit, offset uint64, _ []sqldataenums.Filter, _ []sqldataenums.Sorter) ([]*appentities.AgentDigest, uint64, error) {
	// newest-first
	out := make([]*appentities.AgentDigest, 0, limit)
	for i := len(f.rows) - 1 - int(offset); i >= 0 && uint64(len(out)) < limit; i-- {
		out = append(out, f.rows[i])
	}
	return out, uint64(len(f.rows)), nil
}

func newTestDigestService(repo *fakeDigestRepo, llmMgr *LLMManager) *DigestService {
	return &DigestService{
		repo:      repo,
		notif:     &fakeNotifSource{stats: map[int64]*notification.Stats{}},
		publisher: nil, // feed publish is best-effort; exercised in the live bench
		fleet:     &fakeFleetSource{},
		audit:     &fakeDigestAudit{},
		llm:       llmMgr,
		cfg: func() config.AgentConfigModel {
			var c config.AgentConfigModel
			c.Digest.WindowHours = 24
			c.Digest.Language = "ms"
			return c
		},
		logf: func(string, ...any) {},
	}
}

// digestGenerate drives Generate against a digest service whose narrator input
// is the empty fake (yields all_quiet), so the LLM path is the variable.
func digestGenerate(t *testing.T, llmMgr *LLMManager) *appentities.AgentDigest {
	t.Helper()
	repo := &fakeDigestRepo{}
	d := newTestDigestService(repo, llmMgr)
	digest, err := d.Generate(context.Background(), "manual", 42)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(repo.rows) != 1 {
		t.Fatalf("digest not persisted, rows=%d", len(repo.rows))
	}
	return digest
}

func externalLLM(endpoint string) *LLMManager {
	cfg := config.AgentLLMConfigModel{Mode: "external", Endpoint: endpoint, Model: "test", TimeoutSeconds: 2}
	return NewLLMManager(cfg, nil)
}

func TestDigestGenerateWithHealthyLLM(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"choices":[{"message":{"content":"Semua tenang pagi ini."}}]}`)
	}))
	defer srv.Close()

	digest := digestGenerate(t, externalLLM(srv.URL))
	if digest.NarrativeSource != "llm" || digest.Narrative == "" {
		t.Fatalf("expected llm narrative, got %+v", digest)
	}
	if digest.NarrativeLang != "ms" || digest.Model != "test" {
		t.Fatalf("narrative metadata wrong: %+v", digest)
	}
	var findings []Finding
	if err := json.Unmarshal([]byte(digest.FindingsJson), &findings); err != nil || len(findings) == 0 {
		t.Fatalf("findings json broken: %v / %s", err, digest.FindingsJson)
	}
	if digest.Kind != "manual" || digest.GeneratedBy != 42 {
		t.Fatalf("attribution wrong: %+v", digest)
	}
}

func TestDigestGenerateSurvivesLLMTimeout(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release // hang past the client timeout
	}))
	defer srv.Close()
	defer close(release)

	digest := digestGenerate(t, externalLLM(srv.URL))
	if digest.NarrativeSource != "none" || digest.Narrative != "" {
		t.Fatalf("timeout must degrade to narrator-only, got %+v", digest)
	}
}

func TestDigestGenerateSurvivesLLMGarbage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `this is not json`)
	}))
	defer srv.Close()

	digest := digestGenerate(t, externalLLM(srv.URL))
	if digest.NarrativeSource != "none" {
		t.Fatalf("garbage must degrade to narrator-only, got %+v", digest)
	}
}

func TestDigestGenerateWithLLMOff(t *testing.T) {
	digest := digestGenerate(t, NewLLMManager(config.AgentLLMConfigModel{Mode: "off"}, nil))
	if digest.NarrativeSource != "none" || digest.Narrative != "" {
		t.Fatalf("mode off must yield narrator-only, got %+v", digest)
	}
	if digest.Severity == "" {
		t.Fatal("severity must be denormalized onto the digest row")
	}
}

func TestNextDigestRunMath(t *testing.T) {
	loc := time.Local
	morning := time.Date(2026, 8, 6, 5, 30, 0, 0, loc)

	// Before today's hour, not yet run today → today 07:00.
	next := nextDigestRun(morning, 7, "")
	if !next.Equal(time.Date(2026, 8, 6, 7, 0, 0, 0, loc)) {
		t.Fatalf("next = %s, want today 07:00", next)
	}
	// Before today's hour but ALREADY ran today (restart just after a run) → tomorrow.
	next = nextDigestRun(time.Date(2026, 8, 6, 6, 59, 0, 0, loc), 7, "2026-08-06")
	if !next.Equal(time.Date(2026, 8, 7, 7, 0, 0, 0, loc)) {
		t.Fatalf("next = %s, want tomorrow 07:00 (already ran)", next)
	}
	// After today's hour → tomorrow 07:00.
	next = nextDigestRun(time.Date(2026, 8, 6, 9, 0, 0, 0, loc), 7, "2026-08-06")
	if !next.Equal(time.Date(2026, 8, 7, 7, 0, 0, 0, loc)) {
		t.Fatalf("next = %s, want tomorrow 07:00", next)
	}
	// Month rollover.
	next = nextDigestRun(time.Date(2026, 8, 31, 23, 0, 0, 0, loc), 7, "")
	if !next.Equal(time.Date(2026, 9, 1, 7, 0, 0, 0, loc)) {
		t.Fatalf("next = %s, want Sep 1 07:00", next)
	}
}

func TestDigestListAndLatest(t *testing.T) {
	repo := &fakeDigestRepo{}
	d := newTestDigestService(repo, NewLLMManager(config.AgentLLMConfigModel{Mode: "off"}, nil))
	for i := 0; i < 3; i++ {
		if _, err := d.Generate(context.Background(), "daily", 0); err != nil {
			t.Fatalf("Generate #%d: %v", i, err)
		}
	}
	rows, total, err := d.List(context.Background(), 10, 0)
	if err != nil || total != 3 || len(rows) != 3 {
		t.Fatalf("List: %v total=%d n=%d", err, total, len(rows))
	}
	if rows[0].Id != 3 {
		t.Fatalf("List must be newest-first, got id %d", rows[0].Id)
	}
	latest, err := d.Latest(context.Background())
	if err != nil || latest == nil || latest.Id != 3 {
		t.Fatalf("Latest: %v %+v", err, latest)
	}
}
