package services

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/mysayasan/kopiv2/apps/mymatasan/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	"github.com/mysayasan/kopiv2/infra/atrest"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// Appearance search: "find more like this" over recorded sightings.
//
// An operator watching a person walk out of shot wants to know where else that person went.
// Until now the only way to ask was by object class — "show me every person today" — which
// on a busy site is thousands of rows and no help at all. This ranks those same sightings by
// how much they LOOK like one the operator picked.
//
// WHAT THIS IS AND IS NOT, because the difference decides whether the feature helps or
// misleads. The descriptor is a general-purpose ImageNet embedding of the crop, produced by
// the resnet18 the anomaly feature already installs (see yolo_worker.py's _crop_backbone).
// It separates coarse appearance well — a person in a red jacket from a person in a black
// one, a white van from a blue hatchback — and it is markedly weaker than a purpose-trained
// re-identification network at matching the SAME person across large changes in pose,
// lighting or camera. So this returns a RANKED SHORTLIST for a human to confirm, and it
// never asserts that two sightings are the same individual. The screen says so too. Every
// row records the model that produced it so a better one can replace this without old
// vectors being silently compared against new.
//
// The alternative — requiring a re-ID model download — was rejected because this product is
// deployed into networks with no egress, and a differentiating feature that only works on
// installations that can reach the internet is not a differentiator on the sites that most
// need it.

// AppearanceHit is one ranked sighting.
type AppearanceHit struct {
	ObservationId int64   `json:"observationId"`
	CameraId      int64   `json:"cameraId"`
	SeenAt        int64   `json:"seenAt"`
	Label string `json:"label"`
	// Similarity is the raw cosine. It is REPORTED but never presented as a match
	// percentage, because on this feature space it is not one — see the scoring note above
	// AppearanceResult.
	Similarity float64 `json:"similarity"`
	// Standout is how far this sighting's similarity sits above the middle of everything
	// compared, in robust deviations. THIS is the number that means something, and it is
	// what the screen bands on.
	Standout   float64 `json:"standout"`
	Confidence float64 `json:"confidence"`
	// SegmentId and Seek are filled in by the API layer, which holds the observation
	// service, so a hit can be opened. Left out of this service so ranking has no footage
	// dependency — and zero when nothing covers the moment, which the screen shows as
	// "footage expired" rather than as a broken play button.
	SegmentId int64 `json:"segmentId,omitempty"`
	Seek      int64 `json:"seek,omitempty"`
}

// AppearanceQuery is one ranked search.
type AppearanceQuery struct {
	// Vector is the thing being looked for. Model must name the feature space it came from.
	Vector []float32
	Model  string
	// Label scopes the search to one object class and is REQUIRED. A person and a car are
	// both points in the same feature space and will cheerfully return a similarity for one
	// another — a confident answer to a question nobody asked.
	Label string
	// From/To bound the window. CameraIds optionally restricts which cameras are searched.
	From, To  int64
	CameraIds []int64
	// MinStandout drops matches that do not stand out from the crowd, measured in robust
	// deviations above the median similarity of everything compared. An ABSOLUTE cosine
	// floor was tried first and is useless here — see the scoring note above
	// AppearanceResult — because every crop of a person scores above 0.94 against every
	// other one.
	MinStandout float64
	Limit         int
	// ExcludeObservationId is the sighting the query came from, so the search does not
	// helpfully rank a thing against itself at 1.00 and call it the best match.
	ExcludeObservationId int64
}

// AppearanceResult is a ranked search plus what it actually looked at.
type AppearanceResult struct {
	Hits []AppearanceHit `json:"hits"`
	// Scanned is how many stored descriptors were compared. Reported because "no matches
	// out of 40,000" and "no matches out of 3" are completely different answers, and a
	// ranked list gives an operator no way to tell them apart.
	Scanned int `json:"scanned"`
	// Model and MinStandout echo what the ranking actually used.
	Model       string  `json:"model"`
	MinStandout float64 `json:"minStandout"`
	// Median and Spread describe the similarity distribution this search ranked against —
	// the numbers Standout is measured in. Reported so a result can be audited rather than
	// taken on trust, and so an operator comparing two searches can see when one had far
	// less to go on than the other.
	Median float64 `json:"median"`
	Spread float64 `json:"spread"`
	// Calibrated is false when there were too few candidates for "stands out from the
	// crowd" to mean anything. The hits are then ordered by raw similarity and the screen
	// says the ranking is uncalibrated, rather than dressing three results up as a
	// statistical finding.
	Calibrated bool  `json:"calibrated"`
	From       int64 `json:"from"`
	To         int64 `json:"to"`
	Label      string `json:"label"`
}

// HOW THIS IS SCORED, AND WHY IT IS NOT A PERCENTAGE.
//
// Cosine similarity over these embeddings does not span a usable range. Measured on the
// real model: two crops of the SAME subject score 0.9825, and two crops of OBVIOUSLY
// DIFFERENT subjects — a red figure and a blue one — score 0.9498. ImageNet features are
// dominated by structure (a person-shaped thing against a dark background) and discard most
// of what an operator actually means by "the man in the red jacket".
//
// So the raw number is fine for ORDERING and worthless as a VERDICT. An absolute floor of
// 0.45 filters nothing; a band at "strong ≥ 0.75" marks every result strong; and a screen
// reporting "95% match" between two unrelated people is not a weak feature, it is a wrong
// answer delivered confidently.
//
// What is meaningful is how far a candidate stands out from the OTHERS THAT WERE COMPARED.
// That is self-calibrating: it adapts to the site, the camera and the object class without
// anybody tuning a threshold. Standout is a robust z-score — deviations above the MEDIAN,
// scaled by the median absolute deviation — because a handful of near-duplicates of the
// query would drag a mean and a standard deviation up and flatten everything else. The same
// median/MAD shape the dashboard's baseline analytics already use.
const (
	// madToSigma converts a median absolute deviation into a standard-deviation-equivalent
	// for normally distributed data, so "3 deviations" means the usual thing.
	madToSigma = 1.4826
	// appearanceMinCalibrationSample is how many comparisons are needed before "stands out
	// from the crowd" describes anything. Below it the search still ranks, by raw
	// similarity, and says it is uncalibrated.
	appearanceMinCalibrationSample = 12
)

// ErrAppearanceRangeTooWide is returned when the window holds more descriptors than one
// ranked search may compare.
//
// It REFUSES rather than ranking the newest N, for the reason a ranked search makes worse
// than a listing does: the rows are read newest-first, so a truncated scan drops the oldest
// candidates — and the match an investigator is looking for is usually the older one. The
// result would be a confident shortlist with the answer missing from it, and nothing on
// screen could say so.
var ErrAppearanceRangeTooWide = errors.New("too many sightings in this range to rank")

const (
	// appearanceMaxScan bounds one ranked search. Each candidate costs a decrypt and a
	// 512-float dot product, so this is roughly a second of work on an appliance.
	appearanceMaxScan = 50000
	// appearanceDefaultLimit / appearanceMaxLimit bound the returned shortlist. A ranked
	// list past a few dozen is not something a person reviews; it is something they scroll.
	appearanceDefaultLimit = 50
	appearanceMaxLimit     = 200
	// appearanceDefaultMinStandout is the floor when a caller does not set one, in robust
	// deviations above the median. Two is deliberately permissive: this produces a
	// shortlist for a person to confirm, and hiding a real match to keep the list tidy is
	// the more expensive mistake.
	appearanceDefaultMinStandout = 2.0
)

// scoredCandidate is one stored descriptor with its similarity to the query.
type scoredCandidate struct {
	row *entities.ObjectAppearance
	sim float64
}

// AppearanceService stores and ranks appearance descriptors.
type AppearanceService struct {
	repo   dbsql.IGenericRepo[entities.ObjectAppearance]
	cipher *atrest.Cipher
	now    func() time.Time
}

// NewAppearanceService builds the service. cipher may be nil (plaintext vectors, tests).
func NewAppearanceService(repo dbsql.IGenericRepo[entities.ObjectAppearance], cipher *atrest.Cipher) *AppearanceService {
	return &AppearanceService{repo: repo, cipher: cipher, now: time.Now}
}

// AppearanceRecord is one descriptor to persist, as the metadata recorder produces it.
type AppearanceRecord struct {
	ObservationId int64
	CameraId      int64
	SeenAt        int64
	Label         string
	Confidence    float64
	Vector        []float32
	Model         string
}

// Store persists one descriptor for a written observation.
//
// A missing vector or model is not an error: the appearance stage skips crops that are too
// small or too uncertain, so most observations legitimately have nothing to store, and
// treating that as a failure would fill the log with noise about the system working.
func (s *AppearanceService) Store(ctx context.Context, rec AppearanceRecord) error {
	if s == nil || s.repo == nil {
		return nil
	}
	if len(rec.Vector) == 0 || strings.TrimSpace(rec.Model) == "" || rec.ObservationId <= 0 {
		return nil
	}
	enc, err := encodeVectorAtRest(s.cipher, rec.Vector)
	if err != nil {
		return err
	}
	_, err = s.repo.Create(ctx, "", entities.ObjectAppearance{
		ObservationId: rec.ObservationId,
		CameraId:      rec.CameraId,
		SeenAt:        rec.SeenAt,
		Label:         strings.ToLower(strings.TrimSpace(rec.Label)),
		Vector:        enc,
		Dim:           len(rec.Vector),
		Model:         strings.TrimSpace(rec.Model),
		Confidence:    rec.Confidence,
		CreatedAt:     s.now().UTC().Unix(),
	})
	return err
}

// VectorFor returns the stored descriptor for one observation, so "find more like this
// sighting" can be asked by id rather than by uploading an image.
func (s *AppearanceService) VectorFor(ctx context.Context, observationId int64) ([]float32, string, string, error) {
	if s == nil || s.repo == nil || observationId <= 0 {
		return nil, "", "", nil
	}
	rows, _, err := s.repo.Get(ctx, "", 1, 0,
		[]sqldataenums.Filter{{FieldName: "ObservationId", Compare: sqldataenums.Equal, Value: observationId}}, nil)
	if err != nil || len(rows) == 0 || rows[0] == nil {
		return nil, "", "", err
	}
	vec, err := decodeVectorAtRest(s.cipher, rows[0].Vector)
	if err != nil {
		return nil, "", "", err
	}
	return vec, rows[0].Model, rows[0].Label, nil
}

// Search ranks stored descriptors against the query vector.
func (s *AppearanceService) Search(ctx context.Context, q AppearanceQuery) (AppearanceResult, error) {
	label := strings.ToLower(strings.TrimSpace(q.Label))
	model := strings.TrimSpace(q.Model)
	minStandout := q.MinStandout
	if minStandout <= 0 {
		minStandout = appearanceDefaultMinStandout
	}
	limit := q.Limit
	if limit <= 0 {
		limit = appearanceDefaultLimit
	}
	if limit > appearanceMaxLimit {
		limit = appearanceMaxLimit
	}
	out := AppearanceResult{
		Hits: []AppearanceHit{}, Model: model, MinStandout: minStandout,
		From: q.From, To: q.To, Label: label,
	}
	if s == nil || s.repo == nil || len(q.Vector) == 0 || label == "" || model == "" {
		return out, nil
	}

	filters := []sqldataenums.Filter{
		{FieldName: "Label", Compare: sqldataenums.Equal, Value: label},
		// Never rank across feature spaces. Cosine similarity between vectors from two
		// different networks produces ordinary-looking numbers with nothing behind them,
		// which is worse than no result because it cannot be told apart from one.
		{FieldName: "Model", Compare: sqldataenums.Equal, Value: model},
	}
	if q.From > 0 {
		filters = append(filters, sqldataenums.Filter{FieldName: "SeenAt", Compare: sqldataenums.GreaterThanOrEqualTo, Value: q.From})
	}
	if q.To > 0 {
		filters = append(filters, sqldataenums.Filter{FieldName: "SeenAt", Compare: sqldataenums.LessThan, Value: q.To})
	}
	if len(q.CameraIds) == 1 {
		filters = append(filters, sqldataenums.Filter{FieldName: "CameraId", Compare: sqldataenums.Equal, Value: q.CameraIds[0]})
	} else if len(q.CameraIds) > 1 {
		ids := make([]any, 0, len(q.CameraIds))
		for _, id := range q.CameraIds {
			ids = append(ids, id)
		}
		filters = append(filters, sqldataenums.Filter{FieldName: "CameraId", Compare: sqldataenums.In, Value: ids})
	}

	rows, total, err := s.repo.Get(ctx, "", appearanceMaxScan+1, 0, filters,
		[]sqldataenums.Sorter{{FieldName: "SeenAt", Sort: sqldataenums.DESC}})
	if err != nil {
		return out, err
	}
	if total > uint64(appearanceMaxScan) || len(rows) > appearanceMaxScan {
		return out, fmt.Errorf("%w: %d sightings of %q in this range (limit %d) — search a shorter window or fewer cameras",
			ErrAppearanceRangeTooWide, total, label, appearanceMaxScan)
	}

	// Every candidate is scored BEFORE anything is dropped: the threshold is expressed
	// relative to the distribution, so the distribution has to be known first. Filtering
	// as it goes would calibrate against the survivors, which is circular.
	all := make([]scoredCandidate, 0, len(rows))
	scanned := 0
	for _, row := range rows {
		if row == nil || row.ObservationId == q.ExcludeObservationId {
			continue
		}
		// A stored vector of a different length cannot be compared even if the model name
		// matches — that combination means something wrote a row it should not have, and
		// scoring it would launder a bug into a ranking.
		if row.Dim != len(q.Vector) {
			continue
		}
		vec, derr := decodeVectorAtRest(s.cipher, row.Vector)
		if derr != nil {
			// One unreadable row must not fail a whole search: it is almost always a
			// vector written under a key that has since been rotated, and the honest
			// response is to rank everything else and report a smaller Scanned.
			continue
		}
		scanned++
		all = append(all, scoredCandidate{row: row, sim: cosineSimilarity(q.Vector, vec)})
	}

	median, spread := similarityCentreAndSpread(all)
	out.Median = round4(median)
	out.Spread = round4(spread)
	out.Calibrated = scanned >= appearanceMinCalibrationSample && spread > 0

	hits := make([]AppearanceHit, 0, limit)
	for _, c := range all {
		standout := 0.0
		if out.Calibrated {
			standout = (c.sim - median) / (madToSigma * spread)
			if standout < minStandout {
				continue
			}
		}
		hits = append(hits, AppearanceHit{
			ObservationId: c.row.ObservationId,
			CameraId:      c.row.CameraId,
			SeenAt:        c.row.SeenAt,
			Label:         c.row.Label,
			Similarity:    round4(c.sim),
			Standout:      round4(standout),
			Confidence:    c.row.Confidence,
		})
	}

	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].Similarity != hits[j].Similarity {
			return hits[i].Similarity > hits[j].Similarity
		}
		// Tie-break on the clearer view, then newest — deterministic ordering matters
		// because an operator comparing two runs of the same search must not see the rows
		// shuffle for no reason.
		if hits[i].Confidence != hits[j].Confidence {
			return hits[i].Confidence > hits[j].Confidence
		}
		return hits[i].SeenAt > hits[j].SeenAt
	})
	if len(hits) > limit {
		hits = hits[:limit]
	}
	out.Hits = hits
	out.Scanned = scanned
	return out, nil
}

// similarityCentreAndSpread returns the median similarity and its median absolute
// deviation.
//
// Median and MAD rather than mean and standard deviation because the thing being looked for
// is an OUTLIER, and a mean is dragged towards whatever outliers exist. If a person walked
// past six cameras, six near-duplicates of the query are in the set; a mean would climb
// towards them and a standard deviation would widen, so the very matches the search exists
// to surface would be the ones it flattened.
func similarityCentreAndSpread(items []scoredCandidate) (float64, float64) {
	if len(items) == 0 {
		return 0, 0
	}
	sims := make([]float64, len(items))
	for i, c := range items {
		sims[i] = c.sim
	}
	median := medianOf(sims)
	devs := make([]float64, len(sims))
	for i, v := range sims {
		devs[i] = math.Abs(v - median)
	}
	return median, medianOf(devs)
}

// medianOf sorts a COPY and returns the middle value. The copy matters: the caller's slice
// order is the ranking order further down, and sorting it here would silently reorder the
// results by similarity before the tie-breaks have been applied.
func medianOf(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	cp := append([]float64(nil), values...)
	sort.Float64s(cp)
	mid := len(cp) / 2
	if len(cp)%2 == 1 {
		return cp[mid]
	}
	return (cp[mid-1] + cp[mid]) / 2
}

// DeleteForObservations removes descriptors whose sighting has been purged.
//
// Called from the observation retention and per-camera purge paths. A descriptor that
// outlives the sighting it describes is both a storage leak and a retention breach: the
// footage and the index are gone, and what remains is a searchable record of a person who
// was supposed to have been forgotten.
func (s *AppearanceService) DeleteForObservations(ctx context.Context, observationIds []int64) (int, error) {
	if s == nil || s.repo == nil || len(observationIds) == 0 {
		return 0, nil
	}
	ids := make([]any, 0, len(observationIds))
	for _, id := range observationIds {
		if id > 0 {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return 0, nil
	}
	n, err := s.repo.Delete(ctx, "",
		[]sqldataenums.Filter{{FieldName: "ObservationId", Compare: sqldataenums.In, Value: ids}})
	if err != nil && !isNoRows(err) {
		return 0, err
	}
	return int(n), nil
}

// DeleteForCamera removes every descriptor for one camera, for the camera-delete cascade
// and the per-camera purge.
func (s *AppearanceService) DeleteForCamera(ctx context.Context, cameraId int64) (int, error) {
	if s == nil || s.repo == nil || cameraId <= 0 {
		return 0, nil
	}
	n, err := s.repo.Delete(ctx, "",
		[]sqldataenums.Filter{{FieldName: "CameraId", Compare: sqldataenums.Equal, Value: cameraId}})
	if err != nil && !isNoRows(err) {
		return 0, err
	}
	return int(n), nil
}

// EncodeAppearanceVectorParam / DecodeAppearanceVectorParam are the WIRE form of a query
// vector, used when the control plane fans one node's sighting out to every other node.
//
// It is base64url of the raw float32 little-endian bytes — the same byte layout the at-rest
// codec uses, minus the encryption, because this is in flight on an already-encrypted mTLS
// channel rather than at rest on a disk that can be stolen. Base64 rather than a list of
// decimal floats because a 512-dimension vector is 2.7 KB encoded and roughly 6 KB spelled
// out, and this travels in a URL.
//
// Unpadded base64url specifically: a query parameter carrying "+", "/" or "=" is one
// mis-escaped hop away from arriving as a different vector, and a vector that decodes to
// slightly wrong numbers does not fail — it silently ranks the wrong things.
func EncodeAppearanceVectorParam(vec []float32) string {
	buf := make([]byte, len(vec)*4)
	for i, f := range vec {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(f))
	}
	return base64.RawURLEncoding.EncodeToString(buf)
}

func DecodeAppearanceVectorParam(raw string) ([]float32, error) {
	buf, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(raw))
	if err != nil {
		return nil, fmt.Errorf("appearance vector is not valid base64url: %w", err)
	}
	if len(buf) == 0 || len(buf)%4 != 0 {
		return nil, fmt.Errorf("appearance vector length %d is not a whole number of float32s", len(buf))
	}
	vec := make([]float32, len(buf)/4)
	for i := range vec {
		vec[i] = math.Float32frombits(binary.LittleEndian.Uint32(buf[i*4:]))
	}
	return vec, nil
}

func round4(v float64) float64 {
	return float64(int64(v*10000+0.5)) / 10000
}
