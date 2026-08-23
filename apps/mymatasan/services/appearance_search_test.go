package services

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"testing"

	"github.com/mysayasan/kopiv2/apps/mymatasan/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// appearanceRepo is an in-memory stand-in that HONOURS the filters the search builds.
//
// It evaluates them rather than returning a canned slice, because the assertions that
// matter most here are about what the query EXCLUDES — a row from another model, another
// label, another camera, outside the window. A fake that ignores filters passes every one
// of those tests while the real query returns nonsense.
type appearanceRepo struct {
	dbsql.IGenericRepo[entities.ObjectAppearance]
	rows    []*entities.ObjectAppearance
	nextId  int64
	deleted []int64
}

func (f *appearanceRepo) Get(_ context.Context, _ string, limit, offset uint64, filters []sqldataenums.Filter, sorters []sqldataenums.Sorter) ([]*entities.ObjectAppearance, uint64, error) {
	var matched []*entities.ObjectAppearance
	for _, row := range f.rows {
		if appearanceMatches(row, filters) {
			matched = append(matched, row)
		}
	}
	desc := false
	for _, s := range sorters {
		if s.FieldName == "SeenAt" && s.Sort == sqldataenums.DESC {
			desc = true
		}
	}
	sort.SliceStable(matched, func(i, j int) bool {
		if desc {
			return matched[i].SeenAt > matched[j].SeenAt
		}
		return matched[i].SeenAt < matched[j].SeenAt
	})
	total := uint64(len(matched))
	if offset >= total {
		return nil, total, nil
	}
	page := matched[offset:]
	if limit > 0 && uint64(len(page)) > limit {
		page = page[:limit]
	}
	return page, total, nil
}

func (f *appearanceRepo) Create(_ context.Context, _ string, model entities.ObjectAppearance) (uint64, error) {
	f.nextId++
	model.Id = f.nextId
	f.rows = append(f.rows, &model)
	return uint64(f.nextId), nil
}

func (f *appearanceRepo) Delete(_ context.Context, _ string, filters []sqldataenums.Filter) (uint64, error) {
	kept := make([]*entities.ObjectAppearance, 0, len(f.rows))
	var n uint64
	for _, row := range f.rows {
		if appearanceMatches(row, filters) {
			n++
			f.deleted = append(f.deleted, row.ObservationId)
			continue
		}
		kept = append(kept, row)
	}
	f.rows = kept
	return n, nil
}

func appearanceMatches(row *entities.ObjectAppearance, filters []sqldataenums.Filter) bool {
	for _, f := range filters {
		switch f.FieldName {
		case "Label", "Model":
			want, _ := f.Value.(string)
			have := row.Label
			if f.FieldName == "Model" {
				have = row.Model
			}
			if f.Compare != sqldataenums.Equal || have != want {
				return false
			}
		case "SeenAt", "CameraId", "ObservationId":
			have := row.SeenAt
			if f.FieldName == "CameraId" {
				have = row.CameraId
			}
			if f.FieldName == "ObservationId" {
				have = row.ObservationId
			}
			switch f.Compare {
			case sqldataenums.Equal:
				want, _ := f.Value.(int64)
				if have != want {
					return false
				}
			case sqldataenums.GreaterThanOrEqualTo:
				want, _ := f.Value.(int64)
				if have < want {
					return false
				}
			case sqldataenums.LessThan:
				want, _ := f.Value.(int64)
				if have >= want {
					return false
				}
			case sqldataenums.In:
				list, _ := f.Value.([]any)
				hit := false
				for _, v := range list {
					if id, ok := v.(int64); ok && id == have {
						hit = true
					}
				}
				if !hit {
					return false
				}
			default:
				panic(fmt.Sprintf("appearanceRepo: unhandled compare %d on %s", f.Compare, f.FieldName))
			}
		default:
			panic(fmt.Sprintf("appearanceRepo: unhandled filter field %q", f.FieldName))
		}
	}
	return true
}

const apBase = int64(1755612000)
const apModel = "resnet18-hsv-560"

// unit builds a deterministic unit vector pointing mostly along one axis, with `tilt`
// mixed in from the next axis — so two vectors' similarity can be reasoned about directly.
func unit(dim, axis int, tilt float64) []float32 {
	v := make([]float32, dim)
	v[axis%dim] = float32(math.Cos(tilt))
	v[(axis+1)%dim] = float32(math.Sin(tilt))
	return v
}

func newAppearanceSvc(rows ...*entities.ObjectAppearance) (*AppearanceService, *appearanceRepo) {
	repo := &appearanceRepo{}
	for _, r := range rows {
		repo.nextId++
		r.Id = repo.nextId
		repo.rows = append(repo.rows, r)
	}
	// No cipher: these tests are about ranking and filtering, and the codec has its own.
	return NewAppearanceService(repo, nil), repo
}

func apRow(obsId, cam, seenAt int64, label string, vec []float32, model string) *entities.ObjectAppearance {
	enc, _ := encodeVectorAtRest(nil, vec)
	return &entities.ObjectAppearance{
		ObservationId: obsId, CameraId: cam, SeenAt: seenAt, Label: label,
		Vector: enc, Dim: len(vec), Model: model, Confidence: 0.9,
	}
}

func TestAppearanceRanksTheMostSimilarFirst(t *testing.T) {
	query := unit(8, 0, 0)
	svc, _ := newAppearanceSvc(
		apRow(1, 1, apBase, "person", unit(8, 0, 0.9), apModel),  // least similar
		apRow(2, 1, apBase+1, "person", unit(8, 0, 0.1), apModel), // most similar
		apRow(3, 1, apBase+2, "person", unit(8, 0, 0.5), apModel),
	)
	res, err := svc.Search(context.Background(), AppearanceQuery{
		Vector: query, Model: apModel, Label: "person",
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(res.Hits) != 3 {
		t.Fatalf("hits = %d, want 3", len(res.Hits))
	}
	if res.Hits[0].ObservationId != 2 || res.Hits[2].ObservationId != 1 {
		t.Fatalf("ranking = %d,%d,%d, want 2,3,1",
			res.Hits[0].ObservationId, res.Hits[1].ObservationId, res.Hits[2].ObservationId)
	}
	for i := 1; i < len(res.Hits); i++ {
		if res.Hits[i].Similarity > res.Hits[i-1].Similarity {
			t.Fatalf("hits are not sorted by similarity: %+v", res.Hits)
		}
	}
}

func TestAppearanceNeverRanksAcrossModels(t *testing.T) {
	// A vector from another network is not a worse match — it is a meaningless one.
	// Cosine similarity between unrelated feature spaces returns ordinary-looking numbers
	// with nothing behind them, so a model swap must degrade to "no older matches" rather
	// than to a confident ranking of noise.
	query := unit(8, 0, 0)
	svc, _ := newAppearanceSvc(
		apRow(1, 1, apBase, "person", unit(8, 0, 0.05), "some-other-model"),
		apRow(2, 1, apBase+1, "person", unit(8, 0, 0.8), apModel),
	)
	res, err := svc.Search(context.Background(), AppearanceQuery{
		Vector: query, Model: apModel, Label: "person",
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(res.Hits) != 1 || res.Hits[0].ObservationId != 2 {
		t.Fatalf("hits = %+v, want only the same-model row", res.Hits)
	}
}

func TestAppearanceNeverRanksAcrossDimensions(t *testing.T) {
	// Same model name, different vector length: something wrote a row it should not have.
	// Scoring the overlap would launder that bug into a ranking an operator would act on.
	query := unit(8, 0, 0)
	svc, _ := newAppearanceSvc(
		apRow(1, 1, apBase, "person", unit(16, 0, 0.01), apModel),
		apRow(2, 1, apBase+1, "person", unit(8, 0, 0.8), apModel),
	)
	res, err := svc.Search(context.Background(), AppearanceQuery{
		Vector: query, Model: apModel, Label: "person",
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(res.Hits) != 1 || res.Hits[0].ObservationId != 2 {
		t.Fatalf("hits = %+v, want only the matching-dimension row", res.Hits)
	}
	if res.Scanned != 1 {
		t.Fatalf("scanned = %d, want 1 — the mismatched row must not count as compared", res.Scanned)
	}
}

func TestAppearanceScopesToOneLabel(t *testing.T) {
	// A person and a car are both points in the same feature space, so a car WILL return a
	// similarity for a person query. That is a confident answer to a question nobody asked.
	query := unit(8, 0, 0)
	svc, _ := newAppearanceSvc(
		apRow(1, 1, apBase, "car", unit(8, 0, 0.0), apModel), // identical vector, wrong class
		apRow(2, 1, apBase+1, "person", unit(8, 0, 0.7), apModel),
	)
	res, err := svc.Search(context.Background(), AppearanceQuery{
		Vector: query, Model: apModel, Label: "person",
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(res.Hits) != 1 || res.Hits[0].ObservationId != 2 {
		t.Fatalf("hits = %+v, want only the person", res.Hits)
	}
}

func TestAppearanceExcludesTheSightingBeingSearchedFor(t *testing.T) {
	// Otherwise the top hit is always the query itself at 1.00, which reads as a perfect
	// match and pushes the real answer to second place.
	query := unit(8, 0, 0)
	svc, _ := newAppearanceSvc(
		apRow(7, 1, apBase, "person", query, apModel),
		apRow(8, 1, apBase+1, "person", unit(8, 0, 0.3), apModel),
	)
	res, err := svc.Search(context.Background(), AppearanceQuery{
		Vector: query, Model: apModel, Label: "person",
		ExcludeObservationId: 7,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(res.Hits) != 1 || res.Hits[0].ObservationId != 8 {
		t.Fatalf("hits = %+v, want the query sighting excluded", res.Hits)
	}
}

// crowd builds `n` mutually-similar candidates — the everyday population of a busy site,
// where every crop of a person resembles every other one.
func crowd(n int, startId int64) []*entities.ObjectAppearance {
	rows := make([]*entities.ObjectAppearance, 0, n)
	for i := 0; i < n; i++ {
		// Small, varied tilts: alike, but not identical to each other.
		tilt := 0.55 + float64(i%7)*0.01
		rows = append(rows, apRow(startId+int64(i), 1, apBase+int64(i), "person", unit(8, 0, tilt), apModel))
	}
	return rows
}

func TestAppearanceScoresHowFarASightingStandsOutFromTheCrowd(t *testing.T) {
	// THE MEASUREMENT THAT FORCED THIS DESIGN. On the real model, two crops of the same
	// subject score 0.9825 and two crops of obviously different subjects score 0.9498. An
	// absolute cosine floor therefore filters nothing and an absolute band marks everything
	// "strong". What is meaningful is standing out from the others that were compared.
	query := unit(8, 0, 0)
	rows := crowd(30, 100)
	// One genuine match, much closer to the query than the crowd is.
	rows = append(rows, apRow(999, 2, apBase+500, "person", unit(8, 0, 0.05), apModel))
	svc, _ := newAppearanceSvc(rows...)

	res, err := svc.Search(context.Background(), AppearanceQuery{
		Vector: query, Model: apModel, Label: "person",
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if !res.Calibrated {
		t.Fatalf("a %d-candidate search should be calibrated", res.Scanned)
	}
	if len(res.Hits) != 1 || res.Hits[0].ObservationId != 999 {
		t.Fatalf("hits = %+v, want only the sighting that stands out", res.Hits)
	}
	if res.Hits[0].Standout < 2.0 {
		t.Fatalf("standout = %v, want at least the default floor", res.Hits[0].Standout)
	}
	// The crowd IS similar to the query in absolute terms — which is the whole point. If
	// this stops being true the scene no longer reproduces the condition being defended
	// against, and the test would pass for the wrong reason.
	if res.Median < 0.8 {
		t.Fatalf("median similarity = %v; the crowd is meant to be absolutely similar", res.Median)
	}
}

func TestAppearanceSaysWhenThereWasTooLittleToCalibrateAgainst(t *testing.T) {
	// "Stands out from the crowd" describes nothing when there is no crowd. Three results
	// dressed up as a statistical finding is a confident answer built on nothing, so the
	// search ranks them by raw similarity and reports that it could not calibrate.
	query := unit(8, 0, 0)
	svc, _ := newAppearanceSvc(
		apRow(1, 1, apBase, "person", unit(8, 0, 0.9), apModel),
		apRow(2, 1, apBase+1, "person", unit(8, 0, 0.1), apModel),
	)
	res, err := svc.Search(context.Background(), AppearanceQuery{
		Vector: query, Model: apModel, Label: "person",
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if res.Calibrated {
		t.Fatalf("two candidates must not be reported as calibrated")
	}
	// Still ranked, still ordered, nothing dropped — an uncalibrated answer is worth less
	// than a calibrated one but far more than none.
	if len(res.Hits) != 2 || res.Hits[0].ObservationId != 2 {
		t.Fatalf("hits = %+v, want both, best first", res.Hits)
	}
	if res.Hits[0].Standout != 0 {
		t.Fatalf("standout = %v, want 0 when uncalibrated rather than a made-up figure", res.Hits[0].Standout)
	}
}

func TestAppearanceCalibrationIsNotDraggedUpByNearDuplicates(t *testing.T) {
	// THE REASON IT IS MEDIAN AND MAD RATHER THAN MEAN AND STANDARD DEVIATION. If somebody
	// walked past six cameras, six near-duplicates of the query are in the candidate set. A
	// mean would climb towards them and a standard deviation would widen, so the very
	// matches the search exists to surface would be the ones it flattened out of the
	// results.
	query := unit(8, 0, 0)
	// Twelve of them, against a crowd of thirty. That ratio is what makes this test
	// discriminate: with only a handful of duplicates a mean and a standard deviation are
	// dragged but not far enough to matter, and the test would pass against the very
	// statistics it exists to reject. Twelve is also the realistic number — somebody
	// crossing a site is picked up again and again, which is exactly when this must work.
	rows := crowd(30, 100)
	for i := 0; i < 12; i++ {
		rows = append(rows, apRow(900+int64(i), 2, apBase+600+int64(i), "person", unit(8, 0, 0.05), apModel))
	}
	svc, _ := newAppearanceSvc(rows...)
	res, err := svc.Search(context.Background(), AppearanceQuery{
		Vector: query, Model: apModel, Label: "person",
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(res.Hits) != 12 {
		t.Fatalf("hits = %d, want all twelve near-duplicates surfaced, not flattened", len(res.Hits))
	}
	for _, h := range res.Hits {
		if h.ObservationId < 900 || h.ObservationId > 911 {
			t.Fatalf("unexpected hit %+v", h)
		}
	}
}

func TestAppearanceMedianIgnoresTheCallersOrdering(t *testing.T) {
	// medianOf sorts a COPY. Sorting the caller's slice in place would reorder the
	// candidates by similarity before the confidence and recency tie-breaks are applied,
	// so two searches over the same data could return the same rows in a different order.
	values := []float64{0.9, 0.1, 0.5}
	got := medianOf(values)
	if got != 0.5 {
		t.Fatalf("median = %v, want 0.5", got)
	}
	if values[0] != 0.9 || values[1] != 0.1 || values[2] != 0.5 {
		t.Fatalf("medianOf reordered its input: %+v", values)
	}
}

func TestAppearanceHonoursTheWindowAndCameraFilters(t *testing.T) {
	query := unit(8, 0, 0)
	svc, _ := newAppearanceSvc(
		apRow(1, 1, apBase-500, "person", query, apModel), // before the window
		apRow(2, 1, apBase+50, "person", query, apModel),  // in
		apRow(3, 2, apBase+60, "person", query, apModel),  // in, other camera
		apRow(4, 1, apBase+9000, "person", query, apModel), // after the window
	)
	res, err := svc.Search(context.Background(), AppearanceQuery{
		Vector: query, Model: apModel, Label: "person",
		From: apBase, To: apBase + 3600, CameraIds: []int64{1},
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(res.Hits) != 1 || res.Hits[0].ObservationId != 2 {
		t.Fatalf("hits = %+v, want only the in-window row on camera 1", res.Hits)
	}
}

func TestAppearanceRefusesRatherThanRankingATruncatedSet(t *testing.T) {
	// Rows are read newest-first, so a capped scan drops the OLDEST candidates — and in an
	// investigation the match being looked for is usually an older one. The result would
	// be a confident shortlist with the answer missing and nothing on screen to say so.
	query := unit(8, 0, 0)
	rows := make([]*entities.ObjectAppearance, 0, appearanceMaxScan+1)
	for i := 0; i <= appearanceMaxScan; i++ {
		rows = append(rows, apRow(int64(i+1), 1, apBase+int64(i), "person", query, apModel))
	}
	svc, _ := newAppearanceSvc(rows...)
	_, err := svc.Search(context.Background(), AppearanceQuery{
		Vector: query, Model: apModel, Label: "person",
	})
	if !errors.Is(err, ErrAppearanceRangeTooWide) {
		t.Fatalf("err = %v, want ErrAppearanceRangeTooWide", err)
	}
}

// shortPageRepo reports a large total but hands back a small page — the shape of a repo
// with an internal page cap of its own.
type shortPageRepo struct {
	dbsql.IGenericRepo[entities.ObjectAppearance]
	page  []*entities.ObjectAppearance
	total uint64
}

func (f *shortPageRepo) Get(_ context.Context, _ string, _, _ uint64, _ []sqldataenums.Filter, _ []sqldataenums.Sorter) ([]*entities.ObjectAppearance, uint64, error) {
	return f.page, f.total, nil
}

func TestAppearanceRefusesWhenTheREPORTEDTotalIsTooLargeEvenIfThePageIsShort(t *testing.T) {
	// The refusal has two independent clauses and this pins the one the other cannot
	// cover. If a repo caps its own page below the limit asked for, the returned slice is
	// small and looks perfectly rankable — only the reported total reveals that most of
	// the candidates were never seen. Without this clause the search would confidently
	// rank a handful of rows out of tens of thousands and say nothing about the rest.
	query := unit(8, 0, 0)
	repo := &shortPageRepo{
		page:  []*entities.ObjectAppearance{apRow(1, 1, apBase, "person", query, apModel)},
		total: uint64(appearanceMaxScan) + 1,
	}
	svc := NewAppearanceService(repo, nil)
	_, err := svc.Search(context.Background(), AppearanceQuery{
		Vector: query, Model: apModel, Label: "person",
	})
	if !errors.Is(err, ErrAppearanceRangeTooWide) {
		t.Fatalf("err = %v, want ErrAppearanceRangeTooWide", err)
	}
}

func TestAppearanceStoreSkipsSightingsWithNothingToDescribe(t *testing.T) {
	// The worker skips crops too small or too uncertain to describe, so most observations
	// legitimately carry no vector. Treating that as an error would fill the log with
	// reports of the system working correctly.
	svc, repo := newAppearanceSvc()
	for _, rec := range []AppearanceRecord{
		{ObservationId: 1, Vector: nil, Model: apModel},
		{ObservationId: 2, Vector: unit(8, 0, 0), Model: "  "},
		{ObservationId: 0, Vector: unit(8, 0, 0), Model: apModel},
	} {
		if err := svc.Store(context.Background(), rec); err != nil {
			t.Fatalf("Store(%+v) errored: %v", rec, err)
		}
	}
	if len(repo.rows) != 0 {
		t.Fatalf("stored %d rows, want none", len(repo.rows))
	}
}

func TestAppearanceStoreThenSearchRoundTrips(t *testing.T) {
	svc, _ := newAppearanceSvc()
	vec := unit(8, 3, 0.2)
	if err := svc.Store(context.Background(), AppearanceRecord{
		ObservationId: 42, CameraId: 5, SeenAt: apBase, Label: "Person",
		Confidence: 0.8, Vector: vec, Model: apModel,
	}); err != nil {
		t.Fatalf("Store: %v", err)
	}
	got, model, label, err := svc.VectorFor(context.Background(), 42)
	if err != nil {
		t.Fatalf("VectorFor: %v", err)
	}
	if model != apModel || label != "person" {
		t.Fatalf("model=%q label=%q, want %q/person (label must be normalised on write)", model, label, apModel)
	}
	if len(got) != len(vec) {
		t.Fatalf("round trip changed the dimension: %d -> %d", len(vec), len(got))
	}
	if sim := cosineSimilarity(vec, got); sim < 0.9999 {
		t.Fatalf("round trip changed the vector: similarity %v", sim)
	}
}

func TestAppearancePurgeRemovesDescriptorsWithTheirSightings(t *testing.T) {
	// A descriptor that outlives its sighting is a searchable record of somebody the
	// retention policy says has been forgotten.
	svc, repo := newAppearanceSvc(
		apRow(1, 1, apBase, "person", unit(8, 0, 0), apModel),
		apRow(2, 1, apBase+1, "person", unit(8, 0, 0), apModel),
		apRow(3, 2, apBase+2, "person", unit(8, 0, 0), apModel),
	)
	n, err := svc.DeleteForObservations(context.Background(), []int64{1, 3})
	if err != nil {
		t.Fatalf("DeleteForObservations: %v", err)
	}
	if n != 2 || len(repo.rows) != 1 || repo.rows[0].ObservationId != 2 {
		t.Fatalf("deleted %d, left %+v", n, repo.rows)
	}
}

func TestAppearancePurgeByCameraRemovesEveryDescriptorForIt(t *testing.T) {
	svc, repo := newAppearanceSvc(
		apRow(1, 1, apBase, "person", unit(8, 0, 0), apModel),
		apRow(2, 2, apBase+1, "person", unit(8, 0, 0), apModel),
	)
	n, err := svc.DeleteForCamera(context.Background(), 1)
	if err != nil {
		t.Fatalf("DeleteForCamera: %v", err)
	}
	if n != 1 || len(repo.rows) != 1 || repo.rows[0].CameraId != 2 {
		t.Fatalf("deleted %d, left %+v", n, repo.rows)
	}
}

func TestAppearanceVectorParamSurvivesAURL(t *testing.T) {
	// The federated call carries the query vector in a query string. Unpadded base64url is
	// used because "+", "/" and "=" are one mis-escaped hop from arriving as different
	// bytes — and a vector that decodes to slightly wrong numbers does not fail, it
	// silently ranks the wrong things.
	vec := make([]float32, 512)
	for i := range vec {
		vec[i] = float32(math.Sin(float64(i))) // spans negative values and odd byte patterns
	}
	enc := EncodeAppearanceVectorParam(vec)
	for _, bad := range []string{"+", "/", "="} {
		if strings.Contains(enc, bad) {
			t.Fatalf("encoded vector contains %q, which is not URL-safe: %s", bad, enc[:32])
		}
	}
	got, err := DecodeAppearanceVectorParam(enc)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(got) != len(vec) {
		t.Fatalf("length %d -> %d", len(vec), len(got))
	}
	for i := range vec {
		if got[i] != vec[i] {
			t.Fatalf("element %d changed: %v -> %v", i, vec[i], got[i])
		}
	}
}

func TestAppearanceVectorParamRejectsARaggedPayload(t *testing.T) {
	// A truncated vector must fail loudly. Decoding it to a shorter one would then be
	// dropped by the dimension check further in and look like "no matches".
	if _, err := DecodeAppearanceVectorParam(EncodeAppearanceVectorParam([]float32{1, 2, 3})[:5]); err == nil {
		t.Fatal("a truncated vector decoded without error")
	}
	if _, err := DecodeAppearanceVectorParam("not base64!!"); err == nil {
		t.Fatal("invalid base64 decoded without error")
	}
}

func TestCosineRefusesMismatchedLengths(t *testing.T) {
	if got := cosineSimilarity([]float32{1, 0}, []float32{1, 0, 0}); got != 0 {
		t.Fatalf("cosine of mismatched lengths = %v, want 0", got)
	}
	if got := cosineSimilarity(nil, nil); got != 0 {
		t.Fatalf("cosine of empty = %v, want 0", got)
	}
}

func TestCosineNormalisesRatherThanAssumingUnitLength(t *testing.T) {
	// The producers normalise, but a vector that has been through storage, a model swap or
	// a hand-written test is not guaranteed to. An unnormalised pair scored by raw dot
	// product returns a similarity above 1, which then sorts above every honest match.
	a := []float32{3, 0, 0}
	b := []float32{5, 0, 0}
	got := cosineSimilarity(a, b)
	if math.Abs(got-1.0) > 1e-9 {
		t.Fatalf("cosine of parallel unnormalised vectors = %v, want 1", got)
	}
}
