package services

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	appentities "github.com/mysayasan/kopiv2/apps/mymatasan/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	"github.com/mysayasan/kopiv2/infra/atrest"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// FaceGalleryService owns the GLOBAL face gallery: the enrolled people and their faceprints, plus
// the file the live face worker reads to recognize them. It is the face analog of the anomaly bank,
// but LABELLED (one gallery entry per person) and GLOBAL rather than per-camera — enroll once,
// recognized on every camera that has face recognition switched on.
//
// Matching does NOT happen here or in SQL: this service persists encrypted embeddings and exports a
// plaintext gallery file that the Python worker loads into memory and compares with cosine
// similarity. The database's only job is durable, portable storage (base64 TEXT columns, identical
// on sqlite/MariaDB/Postgres). See face_embedding.go for the encryption/portability rationale.
type FaceGalleryService struct {
	persons    dbsql.IGenericRepo[appentities.FacePerson]
	embeddings dbsql.IGenericRepo[appentities.FaceEmbedding]
	// cipher applies encryption-at-rest to the stored vector bytes (nil = plaintext, e.g. tests).
	cipher   *atrest.Cipher
	embedder FaceEmbedder
	// galleryPath is the worker-readable export file; reload restarts the detector so it re-reads it.
	galleryPath string
	reload      func()
	logf        func(string, ...any)
	now         func() time.Time
}

// FaceEmbedder turns an image into the faces it contains + their embeddings. Production runs the
// YuNet+SFace worker; tests substitute a fake. It is the one model-dependent seam in this service.
type FaceEmbedder interface {
	Embed(ctx context.Context, imageJPEG []byte) ([]DetectedFace, error)
	// Model reports the embedder identity (for the Dim/Model stamp on stored embeddings).
	Model() string
}

// DetectedFace is one face found in an enrollment image.
type DetectedFace struct {
	Vector    []float32  `json:"vector"`
	Box       [4]float64 `json:"box"` // x,y,w,h normalized 0..1
	Quality   float64    `json:"quality"`
	ThumbJPEG []byte     `json:"-"` // a small cropped face, for the person thumbnail (optional)
}

var (
	// ErrNoFace / ErrMultipleFaces make enrollment fail LOUDLY — a bad enrollment silently poisons
	// every future match, so we refuse an image that is not exactly one clear face.
	ErrNoFace        = errors.New("no face found in the image — use a clear, front-facing photo with the whole head visible")
	ErrMultipleFaces = errors.New("more than one face in the image — enroll one person per photo")
	ErrFaceTooSmall  = errors.New("the face is too small — use a closer, higher-resolution photo")
)

const (
	faceMinEnrollQuality = 0.6  // reject low-confidence face detections at enrollment
	faceMinBoxFraction   = 0.02 // reject faces smaller than ~2% of the frame width at enrollment
)

// NewFaceGalleryService builds the service. galleryPath is where the worker-readable gallery is
// written; reload restarts the live worker to pick it up (nil-safe).
func NewFaceGalleryService(
	persons dbsql.IGenericRepo[appentities.FacePerson],
	embeddings dbsql.IGenericRepo[appentities.FaceEmbedding],
	cipher *atrest.Cipher, embedder FaceEmbedder, galleryPath string, reload func(), logf func(string, ...any),
) *FaceGalleryService {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	if reload == nil {
		reload = func() {}
	}
	return &FaceGalleryService{
		persons: persons, embeddings: embeddings, cipher: cipher, embedder: embedder,
		galleryPath: galleryPath, reload: reload, logf: logf, now: time.Now,
	}
}

// --- People -----------------------------------------------------------------------------------

func (s *FaceGalleryService) ListPeople(ctx context.Context) ([]*appentities.FacePerson, error) {
	rows, _, err := s.persons.Get(ctx, "", 1000, 0, nil,
		[]sqldataenums.Sorter{{FieldName: "Name", Sort: sqldataenums.ASC}})
	return rows, err
}

// PersonSummary is a person plus the number of faceprints enrolled for them. The count is not
// decoration: a person with zero faceprints is NOT in the gallery the worker matches against
// (rebuildGallery skips them), so they are enrolled in name only and will never be recognized.
// The roster has to be able to say that, which means the list endpoint has to carry the number.
type PersonSummary struct {
	*appentities.FacePerson
	Photos int `json:"photos"`
}

// ListPeopleWithCounts is ListPeople plus each person's faceprint count. The count comes from the
// query's total, so the encrypted vectors are never loaded just to be counted.
func (s *FaceGalleryService) ListPeopleWithCounts(ctx context.Context) ([]PersonSummary, error) {
	people, err := s.ListPeople(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]PersonSummary, 0, len(people))
	for _, p := range people {
		n, err := s.countEmbeddings(ctx, p.Id)
		if err != nil {
			return nil, err
		}
		out = append(out, PersonSummary{FacePerson: p, Photos: n})
	}
	return out, nil
}

func (s *FaceGalleryService) countEmbeddings(ctx context.Context, personId int64) (int, error) {
	_, total, err := s.embeddings.Get(ctx, "", 1, 0,
		[]sqldataenums.Filter{{FieldName: "PersonId", Compare: sqldataenums.Equal, Value: personId}}, nil)
	return int(total), err
}

func (s *FaceGalleryService) CreatePerson(ctx context.Context, name, notes string, actor int64) (*appentities.FacePerson, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("a person needs a name")
	}
	now := s.now().Unix()
	p := appentities.FacePerson{Name: name, Notes: strings.TrimSpace(notes), Enabled: true, CreatedBy: actor, CreatedAt: now, UpdatedBy: actor, UpdatedAt: now}
	id, err := s.persons.Create(ctx, "", p)
	if err != nil {
		return nil, err
	}
	p.Id = int64(id)
	return &p, nil
}

func (s *FaceGalleryService) UpdatePerson(ctx context.Context, id int64, name, notes string, enabled bool, actor int64) error {
	p, err := s.persons.GetById(ctx, "", uint64(id))
	if err != nil || p == nil {
		return fmt.Errorf("person not found")
	}
	p.Name = strings.TrimSpace(name)
	p.Notes = strings.TrimSpace(notes)
	p.Enabled = enabled
	p.UpdatedBy = actor
	p.UpdatedAt = s.now().Unix()
	if _, err := s.persons.UpdateById(ctx, "", *p); err != nil {
		return err
	}
	return s.rebuildGallery(ctx)
}

// DeletePerson crypto-shreds the person: their embeddings are deleted (the ciphertext becomes
// unrecoverable once the rows are gone), then the person, then the gallery is rebuilt.
func (s *FaceGalleryService) DeletePerson(ctx context.Context, id int64) error {
	if err := s.deleteEmbeddingsFor(ctx, id); err != nil {
		return err
	}
	if _, err := s.persons.DeleteById(ctx, "", uint64(id)); err != nil && !isNoRows(err) {
		return err
	}
	return s.rebuildGallery(ctx)
}

// --- Enrollment -------------------------------------------------------------------------------

// Enroll embeds a photo (or captured frame) and stores the faceprint against a person. It refuses an
// image that is not exactly one clear, large-enough face — a bad enrollment silently poisons every
// future match.
func (s *FaceGalleryService) Enroll(ctx context.Context, personId int64, imageJPEG []byte, source string, actor int64) (*appentities.FaceEmbedding, error) {
	p, err := s.persons.GetById(ctx, "", uint64(personId))
	if err != nil || p == nil {
		return nil, fmt.Errorf("person not found")
	}
	faces, err := s.embedder.Embed(ctx, imageJPEG)
	if err != nil {
		return nil, fmt.Errorf("face embedding failed: %w", err)
	}
	f, err := pickEnrollFace(faces)
	if err != nil {
		return nil, err
	}

	enc, err := s.encodeVector(f.Vector)
	if err != nil {
		return nil, err
	}
	now := s.now().Unix()
	row := appentities.FaceEmbedding{
		PersonId: personId, Vector: enc, Dim: len(f.Vector), Model: s.embedder.Model(),
		Source: defaultStr(source, "upload"), Quality: f.Quality, CreatedAt: now,
	}
	// Keep the cropped face beside the faceprint so the enrollment screen can show what was
	// actually enrolled. The crop, not the original photo: the original is somebody's picture
	// and we have no reason to store it, while the crop is the thing the model looked at.
	if len(f.ThumbJPEG) > 0 {
		row.Thumbnail = base64.StdEncoding.EncodeToString(f.ThumbJPEG)
	}
	id, err := s.embeddings.Create(ctx, "", row)
	if err != nil {
		return nil, err
	}
	row.Id = int64(id)

	// First enrolled face becomes the roster thumbnail.
	if strings.TrimSpace(p.Thumbnail) == "" && len(f.ThumbJPEG) > 0 {
		p.Thumbnail = base64.StdEncoding.EncodeToString(f.ThumbJPEG)
		p.UpdatedAt = now
		_, _ = s.persons.UpdateById(ctx, "", *p)
	}

	if err := s.rebuildGallery(ctx); err != nil {
		s.logf("face gallery rebuild after enroll: %v", err)
	}
	row.Vector = "" // never echo the stored ciphertext back to a caller
	return &row, nil
}

func (s *FaceGalleryService) ListEmbeddings(ctx context.Context, personId int64) ([]*appentities.FaceEmbedding, error) {
	rows, err := s.embeddingsFor(ctx, personId)
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		r.Vector = "" // metadata only; never expose the faceprint bytes over the API
	}
	return rows, nil
}

func (s *FaceGalleryService) DeleteEmbedding(ctx context.Context, id int64) error {
	if _, err := s.embeddings.DeleteById(ctx, "", uint64(id)); err != nil && !isNoRows(err) {
		return err
	}
	return s.rebuildGallery(ctx)
}

// pickEnrollFace enforces "exactly one clear, large face" for enrollment. A bad enrollment silently
// poisons every future match, so it errors rather than guessing. Pure (no I/O) so it is unit-tested.
func pickEnrollFace(faces []DetectedFace) (DetectedFace, error) {
	switch {
	case len(faces) == 0:
		return DetectedFace{}, ErrNoFace
	case len(faces) > 1:
		return DetectedFace{}, ErrMultipleFaces
	}
	f := faces[0]
	if f.Quality < faceMinEnrollQuality {
		return DetectedFace{}, ErrNoFace
	}
	if f.Box[2] < faceMinBoxFraction {
		return DetectedFace{}, ErrFaceTooSmall
	}
	return f, nil
}

// --- Gallery export (what the worker matches against) -----------------------------------------

type faceGalleryExport struct {
	Model   string              `json:"model"`
	Persons []faceGalleryPerson `json:"persons"`
}

type faceGalleryPerson struct {
	Id         int64       `json:"id"`
	Name       string      `json:"name"`
	Embeddings [][]float32 `json:"embeddings"`
}

// rebuildGallery writes the plaintext gallery file the live worker loads, then reloads the worker.
// Only ENABLED people with at least one embedding are included. Written atomically (temp + rename)
// so the worker never reads a half-written file.
func (s *FaceGalleryService) rebuildGallery(ctx context.Context) error {
	if strings.TrimSpace(s.galleryPath) == "" {
		return nil
	}
	people, err := s.ListPeople(ctx)
	if err != nil {
		return err
	}
	exp := faceGalleryExport{Model: s.embedder.Model()}
	for _, p := range people {
		if !p.Enabled {
			continue
		}
		rows, err := s.embeddingsFor(ctx, p.Id)
		if err != nil {
			return err
		}
		var vecs [][]float32
		for _, r := range rows {
			v, err := s.decodeVector(r.Vector)
			if err != nil {
				s.logf("face gallery: skip unreadable embedding %d: %v", r.Id, err)
				continue
			}
			// A ZERO-LENGTH OR ODD-LENGTH VECTOR MUST NEVER REACH THE WORKER. It loads each
			// person's embeddings with np.asarray(embs), which raises on a ragged list — so ONE
			// malformed row does not degrade that one person, it fails the whole gallery load and
			// nobody is recognized on any camera, with a line on the worker's stderr as the only
			// symptom. Skipping here keeps a bad row local to itself.
			if len(v) == 0 {
				s.logf("face gallery: skip empty embedding %d for person %d", r.Id, p.Id)
				continue
			}
			if len(vecs) > 0 && len(v) != len(vecs[0]) {
				s.logf("face gallery: skip embedding %d for person %d: %d dimensions, expected %d (a model change leaves incompatible faceprints behind)",
					r.Id, p.Id, len(v), len(vecs[0]))
				continue
			}
			vecs = append(vecs, v)
		}
		if len(vecs) == 0 {
			continue
		}
		exp.Persons = append(exp.Persons, faceGalleryPerson{Id: p.Id, Name: p.Name, Embeddings: vecs})
	}

	data, err := json.Marshal(exp)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.galleryPath), 0o755); err != nil {
		return err
	}
	tmp := s.galleryPath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.galleryPath); err != nil {
		return err
	}
	s.reload()
	return nil
}

// --- helpers ----------------------------------------------------------------------------------

func (s *FaceGalleryService) embeddingsFor(ctx context.Context, personId int64) ([]*appentities.FaceEmbedding, error) {
	rows, _, err := s.embeddings.Get(ctx, "", 1000, 0,
		[]sqldataenums.Filter{{FieldName: "PersonId", Compare: sqldataenums.Equal, Value: personId}}, nil)
	return rows, err
}

func (s *FaceGalleryService) deleteEmbeddingsFor(ctx context.Context, personId int64) error {
	_, err := s.embeddings.Delete(ctx, "",
		[]sqldataenums.Filter{{FieldName: "PersonId", Compare: sqldataenums.Equal, Value: personId}})
	if err != nil && !isNoRows(err) {
		return err
	}
	return nil
}

// encodeVector / decodeVector delegate to the shared at-rest vector codec (vector_codec.go),
// which appearance search also uses. One implementation on purpose: these two features store
// the same kind of data with the same portability and encryption requirements, and a second
// copy of the byte order or the envelope is a second thing that can drift into unreadable
// vectors nobody notices until a match silently stops working.
func (s *FaceGalleryService) encodeVector(vec []float32) (string, error) {
	return encodeVectorAtRest(s.cipher, vec)
}

func (s *FaceGalleryService) decodeVector(enc string) ([]float32, error) {
	return decodeVectorAtRest(s.cipher, enc)
}

func defaultStr(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

// isNoRows tolerates a "no rows" delete on an already-absent row.
// isNoRows reports the repository's ways of saying "there was nothing to do".
//
// The generic repo treats BOTH as errors: a read that matched nothing ("no result found"),
// and a DELETE or UPDATE that affected no rows ("total affected: 0"). For a purge, the
// second is not an error at all — it is the normal case, and the whole point of a purge is
// that it is safe to run when there is nothing to purge.
//
// "total affected: 0" was missing, and it cost a camera delete. Deleting a camera runs a
// cascade of purges, one of which clears its appearance descriptors; a camera that had never
// produced one failed that purge, the failure aborted the cascade by design, and the delete
// came back as a bare 500. Appearance search is off by default, so that was MOST cameras on
// MOST installs. Found by the W3-3b bench, which is the first one that ever deleted a camera.
func isNoRows(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "no result found") ||
		strings.Contains(msg, "no rows") ||
		strings.Contains(msg, "total affected: 0")
}

// stablePeopleSort keeps the roster deterministic where an ordered slice is handed out.
func stablePeopleSort(people []*appentities.FacePerson) {
	sort.SliceStable(people, func(i, j int) bool { return people[i].Name < people[j].Name })
}
