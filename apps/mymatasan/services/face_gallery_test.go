package services

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	appentities "github.com/mysayasan/kopiv2/apps/mymatasan/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// TestFaceVectorRoundTrip proves an embedding survives encode->store->decode unchanged (the base64
// TEXT storage that keeps the gallery portable across sqlite/MariaDB/Postgres), with no cipher.
func TestFaceVectorRoundTrip(t *testing.T) {
	s := &FaceGalleryService{}
	in := []float32{0.1, -0.2, 0.3333, 1.0, -1.0, 0}
	enc, err := s.encodeVector(in)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	out, err := s.decodeVector(enc)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out) != len(in) {
		t.Fatalf("len %d != %d", len(out), len(in))
	}
	for i := range in {
		if in[i] != out[i] {
			t.Errorf("vec[%d] = %v, want %v", i, out[i], in[i])
		}
	}
}

// TestPickEnrollFace proves enrollment fails LOUDLY on zero/multiple/low-quality/tiny faces — a bad
// enrollment silently poisons every future match, so these must error, not be stored.
func TestPickEnrollFace(t *testing.T) {
	cases := map[string]struct {
		faces   []DetectedFace
		wantErr error
	}{
		"no face":     {faces: nil, wantErr: ErrNoFace},
		"two faces":   {faces: []DetectedFace{{Quality: 0.9, Box: [4]float64{0, 0, 0.3, 0.3}}, {Quality: 0.9}}, wantErr: ErrMultipleFaces},
		"low quality": {faces: []DetectedFace{{Quality: 0.2, Box: [4]float64{0, 0, 0.3, 0.3}}}, wantErr: ErrNoFace},
		"too small":   {faces: []DetectedFace{{Quality: 0.9, Box: [4]float64{0, 0, 0.005, 0.005}}}, wantErr: ErrFaceTooSmall},
		"one good":    {faces: []DetectedFace{{Quality: 0.9, Box: [4]float64{0, 0, 0.3, 0.3}}}, wantErr: nil},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := pickEnrollFace(c.faces)
			if err != c.wantErr {
				t.Errorf("pickEnrollFace error = %v, want %v", err, c.wantErr)
			}
		})
	}
}

// --- Gallery export ---------------------------------------------------------------------------

// fakeFacePersonRepo / fakeFaceEmbeddingRepo are minimal in-memory repos for the two Get calls the
// gallery export makes. The embedded interface supplies the rest, so an unexpected call panics
// loudly rather than quietly returning a zero value.
type fakeFacePersonRepo struct {
	dbsql.IGenericRepo[appentities.FacePerson]
	rows []*appentities.FacePerson
}

func (f *fakeFacePersonRepo) Get(context.Context, string, uint64, uint64, []sqldataenums.Filter, []sqldataenums.Sorter) ([]*appentities.FacePerson, uint64, error) {
	return f.rows, uint64(len(f.rows)), nil
}

type fakeFaceEmbeddingRepo struct {
	dbsql.IGenericRepo[appentities.FaceEmbedding]
	rows []*appentities.FaceEmbedding
}

func (f *fakeFaceEmbeddingRepo) Get(_ context.Context, _ string, _ uint64, _ uint64, filters []sqldataenums.Filter, _ []sqldataenums.Sorter) ([]*appentities.FaceEmbedding, uint64, error) {
	var out []*appentities.FaceEmbedding
	for _, r := range f.rows {
		for _, flt := range filters {
			if flt.FieldName == "PersonId" {
				if id, ok := flt.Value.(int64); ok && id == r.PersonId {
					out = append(out, r)
				}
			}
		}
	}
	return out, uint64(len(out)), nil
}

type fakeFaceEmbedder struct{}

func (fakeFaceEmbedder) Embed(context.Context, []byte) ([]DetectedFace, error) { return nil, nil }
func (fakeFaceEmbedder) Model() string                                        { return "opencv-sface-128" }

// TestRebuildGallerySkipsMalformedVectors pins the rule that a person's exported faceprints are all
// the same length.
//
// THE FAILURE THIS PREVENTS IS NOT LOCAL. The worker builds one matrix per person with
// np.asarray(embeddings), which raises on a ragged list — and that call sits inside a try that
// wraps the WHOLE gallery load. So a single empty or odd-length row does not degrade one person: it
// aborts the load, leaves the cache empty, and NOBODY is recognized on ANY camera, with one line on
// the worker's stderr as the only symptom. An empty row is exactly what a truncated write or a
// hand-repaired database leaves behind, and an odd-length one is what a model change leaves behind.
func TestRebuildGallerySkipsMalformedVectors(t *testing.T) {
	dir := t.TempDir()
	galleryPath := filepath.Join(dir, "faces_gallery.json")

	s := &FaceGalleryService{}
	good1, err := s.encodeVector([]float32{0.1, 0.2, 0.3})
	if err != nil {
		t.Fatal(err)
	}
	good2, err := s.encodeVector([]float32{0.4, 0.5, 0.6})
	if err != nil {
		t.Fatal(err)
	}
	otherModel, err := s.encodeVector([]float32{0.7, 0.8})
	if err != nil {
		t.Fatal(err)
	}

	persons := &fakeFacePersonRepo{rows: []*appentities.FacePerson{
		{Id: 1, Name: "Kept", Enabled: true},
		{Id: 2, Name: "Paused", Enabled: false},
	}}
	embeddings := &fakeFaceEmbeddingRepo{rows: []*appentities.FaceEmbedding{
		{Id: 10, PersonId: 1, Vector: good1, Dim: 3},
		{Id: 11, PersonId: 1, Vector: "", Dim: 0},         // empty: a row that was never written
		{Id: 12, PersonId: 1, Vector: otherModel, Dim: 2}, // a different model's width
		{Id: 13, PersonId: 1, Vector: good2, Dim: 3},
		{Id: 14, PersonId: 2, Vector: good1, Dim: 3}, // person is disabled — excluded entirely
	}}

	svc := NewFaceGalleryService(persons, embeddings, nil, fakeFaceEmbedder{}, galleryPath, nil, nil)
	if err := svc.rebuildGallery(t.Context()); err != nil {
		t.Fatalf("rebuildGallery: %v", err)
	}

	data, err := os.ReadFile(galleryPath)
	if err != nil {
		t.Fatalf("gallery not written: %v", err)
	}
	var exp faceGalleryExport
	if err := json.Unmarshal(data, &exp); err != nil {
		t.Fatalf("gallery is not valid JSON: %v", err)
	}
	if len(exp.Persons) != 1 {
		t.Fatalf("exported %d people, want 1 (the disabled one must not be in the gallery)", len(exp.Persons))
	}
	got := exp.Persons[0]
	if got.Name != "Kept" {
		t.Errorf("exported %q, want Kept", got.Name)
	}
	if len(got.Embeddings) != 2 {
		t.Fatalf("exported %d faceprints, want 2 — the empty and the odd-length rows must be dropped", len(got.Embeddings))
	}
	for i, v := range got.Embeddings {
		if len(v) != 3 {
			t.Errorf("faceprint %d has %d dimensions, want 3: a ragged gallery breaks the worker's load for EVERY person", i, len(v))
		}
	}
}
