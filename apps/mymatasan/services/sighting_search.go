package services

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"github.com/mysayasan/kopiv2/apps/mymatasan/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// This file is the node's half of federated fleet search (W2-4). The control plane asks
// every node it can reach "what did you see", merges the answers, and reports which nodes
// answered. Everything here exists to make ONE tunneled request per node enough, and to
// make the answer honest about its own limits.
//
// TWO THINGS THIS OWES ITS CALLER, and they are the reason it is not just the existing
// grid query with different arguments:
//
//  1. CAMERA NAMES. A federated result identified only by cameraId is unreadable: "camera 3"
//     is a different camera on every node in the estate. The node is the only party that
//     knows its camera names (the control plane has no camera table at all), so it joins
//     them here rather than making the caller issue a second tunneled request per node and
//     stitch it up.
//
//  2. AN EXPLICIT CAP SIGNAL. Every federated search is bounded per node, so a node with
//     more matches than the limit returns a PREFIX of the truth. Saying so — Capped, plus
//     Oldest, the timestamp the returned prefix reaches back to — is what lets the control
//     plane tell the operator where completeness ends. Without it, a result set that stops
//     at 200 rows is indistinguishable from an estate that only saw 200 things, and an
//     investigator would read "nothing older" off a page that simply ran out of room.

const (
	// defaultSearchLimit is the per-node result cap when the caller does not set one.
	defaultSearchLimit = 200
	// maxSearchLimit bounds what a caller may ask one node for in a single search. The
	// results cross the control channel as one JSON body, so this is also the ceiling on
	// how much a single search can put on the tunnel.
	maxSearchLimit = 1000
	// identityScanPageSize / identityScanPages bound the alert scan behind an identity
	// search. Alert events are the highest-write table in the app and identity matching is
	// a substring test the SQL layer cannot express, so rows are read in pages and filtered
	// in memory. Reaching the bound is reported as Capped, never as "no more matches".
	identityScanPageSize = 500
	identityScanPages    = 40
)

// SightingHit is one thing a node saw, in the shape a fleet-wide search consumes.
//
// Object sightings (presence intervals from the metadata recorder) and identity sightings
// (a recognized plate or face, which live in alert events) are deliberately ONE type. They
// answer the same operator question — "where and when was this seen" — and a fleet search
// that returned two differently-shaped lists would push the job of interleaving them onto
// every caller, including the browser.
type SightingHit struct {
	// Kind is "object" (an observed object class) or "identity" (a recognized plate/face).
	Kind string `json:"kind"`
	// Id is the node-local row id. It is NOT unique across the fleet — the control plane
	// qualifies it with the node id.
	Id         int64  `json:"id"`
	CameraId   int64  `json:"cameraId"`
	CameraName string `json:"cameraName"`
	// Label is what to show: the object class ("person") or the alert's rendered label
	// ("Plate WXY1234 (white car)", "Alice (94%)").
	Label string `json:"label"`
	// Identity is the matched text for an identity hit — the plate string or the person's
	// name — carried separately from Label so the caller can group by who/what rather than
	// by a rendered sentence. Empty for object hits.
	Identity string `json:"identity,omitempty"`
	// IdentityKind is "plate" or "face" for identity hits, empty otherwise.
	IdentityKind string `json:"identityKind,omitempty"`
	// StartedAt/EndedAt bound the sighting. An identity hit is a moment, so both carry the
	// alert time — the caller can sort and window one field for both kinds.
	StartedAt  int64   `json:"startedAt"`
	EndedAt    int64   `json:"endedAt"`
	Confidence float64 `json:"confidence"`
	// Count is the peak simultaneous count for an object interval (1 for identity hits).
	Count int `json:"count,omitempty"`
	// PeakBox / PeakAt locate the clearest frame of the sighting, for the drawn box and
	// the playback seek. PeakBox is the stored JSON box string, passed through verbatim.
	PeakBox string `json:"peakBox,omitempty"`
	PeakAt  int64  `json:"peakAt,omitempty"`
	// SegmentId / SegmentCodec / SeekSeconds link an object hit to playable footage.
	SegmentId    int64  `json:"segmentId,omitempty"`
	SegmentCodec string `json:"segmentCodec,omitempty"`
	SeekSeconds  int64  `json:"seekSeconds,omitempty"`
	// FootagePending marks a sighting whose covering segment has not been finalized yet —
	// footage exists and is coming, it just cannot be opened this second.
	FootagePending bool `json:"footagePending,omitempty"`
	// HasSnapshot reports that an identity hit has a stored alert snapshot to fetch.
	HasSnapshot bool `json:"hasSnapshot,omitempty"`
}

// SightingPage is one node's answer to one federated search.
type SightingPage struct {
	Items []SightingHit `json:"items"`
	// Capped is true when the node had more matches than it was allowed to return. The
	// caller MUST surface this: without it the returned prefix reads as the whole truth.
	Capped bool `json:"capped"`
	// Oldest is the earliest StartedAt in Items (0 when empty). Combined with Capped it is
	// the node's completeness horizon: everything at or after Oldest is complete, anything
	// before it may be missing rows this node did not return.
	Oldest int64 `json:"oldest"`
}

// SightingQuery is the federated search a node is asked to answer.
type SightingQuery struct {
	// From/To bound the sighting time (unix seconds). 0 means unbounded on that side.
	From int64
	To   int64
	// Labels restricts object hits to these raw detector labels (empty = any).
	Labels []string
	// MinConfidence is a 0..1 floor on the sighting's confidence.
	MinConfidence float64
	// Text is a case-insensitive substring an identity must contain (empty = any
	// identity). It is matched against the plate string / person name, and — so an
	// operator searching "white car" still finds something — the rendered alert label.
	Text string
	// IdentityKinds restricts identity hits to "plate" and/or "face" (empty = both).
	IdentityKinds []string
	// Limit caps the rows returned. Clamped to [1, maxSearchLimit].
	Limit int
}

// SightingSearch answers federated searches over what this node saw.
//
// It reads through the SAME service the node's own screens use — the observation service
// resolves footage exactly as the local Objects page does, so a row found by a fleet
// search and the same row found on the node agree about which segment plays it. A search
// path with its own footage resolution would be a second implementation of the trickiest
// logic in the app, free to drift.
type SightingSearch struct {
	observations *ObservationService
	alerts       dbsql.IGenericRepo[entities.AlertEvent]
	cameras      dbsql.IGenericRepo[entities.Camera]
}

// NewSightingSearch builds the search service. Any dependency may be nil in tests that
// exercise only one half; a nil dependency yields no hits of that kind rather than an error.
func NewSightingSearch(observations *ObservationService, alerts dbsql.IGenericRepo[entities.AlertEvent], cameras dbsql.IGenericRepo[entities.Camera]) *SightingSearch {
	return &SightingSearch{observations: observations, alerts: alerts, cameras: cameras}
}

// clampSearchLimit normalizes a caller-supplied limit.
func clampSearchLimit(limit int) int {
	if limit <= 0 {
		return defaultSearchLimit
	}
	if limit > maxSearchLimit {
		return maxSearchLimit
	}
	return limit
}

// SearchObjects returns object-class sightings (presence intervals), newest first.
//
// Unlike the node's own Objects grid this does NOT hide sightings that have no playable
// footage. A fleet search answers "was it seen", not "can I watch it": a detect-only
// camera at a remote gate is often the only thing that saw the vehicle, and dropping its
// sightings would answer "never seen here" to an investigator — the one wrong answer this
// feature must not give. Footage state is reported per row instead, so the caller can show
// what is playable without pretending the rest did not happen.
func (s *SightingSearch) SearchObjects(ctx context.Context, q SightingQuery) (SightingPage, error) {
	page := SightingPage{Items: []SightingHit{}}
	if s.observations == nil {
		return page, nil
	}
	limit := clampSearchLimit(q.Limit)

	filters := make([]sqldataenums.Filter, 0, 4)
	if q.From > 0 {
		filters = append(filters, sqldataenums.Filter{FieldName: "StartedAt", Compare: sqldataenums.GreaterThanOrEqualTo, Value: q.From})
	}
	if q.To > 0 {
		filters = append(filters, sqldataenums.Filter{FieldName: "StartedAt", Compare: sqldataenums.LessThanOrEqualTo, Value: q.To})
	}
	if labels := normalizeSearchLabels(q.Labels); len(labels) > 0 {
		filters = append(filters, sqldataenums.Filter{FieldName: "Label", Compare: sqldataenums.In, Value: labels})
	}
	if q.MinConfidence > 0 {
		filters = append(filters, sqldataenums.Filter{FieldName: "MaxConfidence", Compare: sqldataenums.GreaterThanOrEqualTo, Value: q.MinConfidence})
	}
	sorters := []sqldataenums.Sorter{{FieldName: "StartedAt", Sort: sqldataenums.DESC}}

	// Ask for one row more than the limit: the extra row is how "there were more" is known
	// without a second count query, and it is dropped before the page is returned.
	rows, _, err := s.observations.repo.Get(ctx, "", uint64(limit+1), 0, filters, sorters)
	if err != nil {
		return page, err
	}
	if len(rows) > limit {
		page.Capped = true
		rows = rows[:limit]
	}
	if len(rows) == 0 {
		return page, nil
	}

	segs, newestEnd := s.observations.resolveCoveringSegments(ctx, rows)
	names := s.cameraNames(ctx)
	// Which cameras actually keep footage. Used ONLY to label a sighting that has no
	// segment, never to filter one out.
	//
	// "Newer than this camera's newest saved footage" means the footage is still being
	// written — but ONLY on a camera that records. A detect-only camera keeps no segments
	// at all, so every one of its sightings is newer than its (empty) footage and would be
	// labelled "recording…" forever, promising a clip that is never coming. nil means the
	// configs could not be read, in which case the older behaviour stands (fail open: claim
	// pending rather than assert an absence we cannot verify).
	recordingOn := s.observations.camerasRecording(ctx)
	for i, row := range rows {
		if row == nil {
			continue
		}
		hit := SightingHit{
			Kind:       "object",
			Id:         row.Id,
			CameraId:   row.CameraId,
			CameraName: names[row.CameraId],
			Label:      row.Label,
			StartedAt:  row.StartedAt,
			EndedAt:    row.EndedAt,
			Confidence: row.MaxConfidence,
			Count:      row.MaxCount,
			PeakBox:    row.PeakBox,
			PeakAt:     row.PeakAt,
		}
		if seg := segs[i]; seg != nil {
			seekAt := row.StartedAt
			if row.PeakAt > 0 && row.PeakAt >= seg.StartedAt {
				seekAt = row.PeakAt
			}
			hit.SegmentId = seg.Id
			hit.SegmentCodec = seg.Codec
			if seekAt > seg.StartedAt {
				hit.SeekSeconds = seekAt - seg.StartedAt
			}
		} else if row.StartedAt >= newestEnd[row.CameraId] && (recordingOn == nil || recordingOn[row.CameraId]) {
			// Newer than this camera's newest saved footage, on a camera that records: it
			// falls inside the segment still being written, so footage exists but is not
			// openable yet. On a camera that records nothing, the same test is true forever
			// and the sighting simply has no footage — which is worth saying plainly.
			hit.FootagePending = true
		}
		page.Items = append(page.Items, hit)
		if page.Oldest == 0 || (row.StartedAt > 0 && row.StartedAt < page.Oldest) {
			page.Oldest = row.StartedAt
		}
	}
	return page, nil
}

// SearchIdentities returns recognized-identity sightings — license plates and faces —
// newest first.
//
// These do NOT live in the observation index. The metadata recorder coalesces object
// CLASSES ("car", "person"); the identity behind one is resolved by the LPR and face
// stages and recorded on the alert the rule raised, in its label and its metadata. So
// "find plate WXY1234 across the fleet" is a question about alert events, and this is the
// only path that can answer it.
//
// The substring match is done in memory over a bounded scan because the repository layer
// has no LIKE. Reaching that bound sets Capped — the same contract as any other cap here:
// a prefix of the truth, declared as one.
func (s *SightingSearch) SearchIdentities(ctx context.Context, q SightingQuery) (SightingPage, error) {
	page := SightingPage{Items: []SightingHit{}}
	if s.alerts == nil {
		return page, nil
	}
	limit := clampSearchLimit(q.Limit)
	wantPlate, wantFace := identityKindWanted(q.IdentityKinds)
	needle := strings.ToLower(strings.TrimSpace(q.Text))

	filters := make([]sqldataenums.Filter, 0, 3)
	if q.From > 0 {
		filters = append(filters, sqldataenums.Filter{FieldName: "CreatedAt", Compare: sqldataenums.GreaterThanOrEqualTo, Value: q.From})
	}
	if q.To > 0 {
		filters = append(filters, sqldataenums.Filter{FieldName: "CreatedAt", Compare: sqldataenums.LessThanOrEqualTo, Value: q.To})
	}
	// Diagnostics ("sampled", "capture_failed") are the bulk of the table and can never
	// carry an identity, so they are excluded at the query rather than scanned past.
	filters = append(filters, sqldataenums.Filter{FieldName: "IsDiagnostic", Compare: sqldataenums.Equal, Value: false})
	sorters := []sqldataenums.Sorter{{FieldName: "CreatedAt", Sort: sqldataenums.DESC}}

	names := s.cameraNames(ctx)
	for pageNo := 0; pageNo < identityScanPages; pageNo++ {
		batch, _, err := s.alerts.Get(ctx, "", identityScanPageSize, uint64(pageNo*identityScanPageSize), filters, sorters)
		if err != nil {
			return page, err
		}
		if len(batch) == 0 {
			return page, nil
		}
		for _, alert := range batch {
			if alert == nil {
				continue
			}
			hit, ok := identityHit(alert, wantPlate, wantFace, needle, q.MinConfidence)
			if !ok {
				continue
			}
			hit.CameraName = names[alert.CameraId]
			page.Items = append(page.Items, hit)
			if page.Oldest == 0 || (hit.StartedAt > 0 && hit.StartedAt < page.Oldest) {
				page.Oldest = hit.StartedAt
			}
			if len(page.Items) >= limit {
				// The limit stopped us, not the data. There may be nothing further back —
				// but this node cannot say so, and guessing in the reassuring direction is
				// exactly the bias this whole file exists to avoid.
				page.Capped = true
				return page, nil
			}
		}
		if len(batch) < identityScanPageSize {
			return page, nil
		}
	}
	// The scan bound stopped us mid-table: older matches may exist and were never examined.
	page.Capped = true
	return page, nil
}

// Labels returns the distinct object labels this node has recorded, for the fleet-wide
// filter list. It delegates to the observation service so the node's own picker and the
// fleet picker are populated from exactly the same scan.
func (s *SightingSearch) Labels(ctx context.Context) ([]string, error) {
	if s.observations == nil {
		return []string{}, nil
	}
	return s.observations.Labels(ctx, 0)
}

// identityHit decides whether one alert is an identity sighting the query wants, and
// renders it. It returns ok=false for an alert carrying no identity at all.
func identityHit(alert *entities.AlertEvent, wantPlate, wantFace bool, needle string, minConfidence float64) (SightingHit, bool) {
	kind, identity, detail := alertIdentity(alert)
	if kind == "" {
		return SightingHit{}, false
	}
	if (kind == "plate" && !wantPlate) || (kind == "face" && !wantFace) {
		return SightingHit{}, false
	}
	if minConfidence > 0 && alert.Confidence > 0 && alert.Confidence < minConfidence {
		return SightingHit{}, false
	}
	if needle != "" {
		// Match the identity first, then the rendered label and descriptor. The label is
		// included so a search for "white car" — a descriptor the operator read off an
		// alert, not a plate — still finds it; matching only the plate string would answer
		// "no such sighting" to text that is visibly on the screen they copied it from.
		if !strings.Contains(strings.ToLower(identity), needle) &&
			!strings.Contains(strings.ToLower(alert.Label), needle) &&
			!strings.Contains(strings.ToLower(detail), needle) {
			return SightingHit{}, false
		}
	}
	at := alert.CreatedAt
	return SightingHit{
		Kind:         "identity",
		Id:           alert.Id,
		CameraId:     alert.CameraId,
		Label:        alert.Label,
		Identity:     identity,
		IdentityKind: kind,
		StartedAt:    at,
		EndedAt:      at,
		Confidence:   alert.Confidence,
		Count:        1,
		PeakBox:      alert.BoundingBox,
		PeakAt:       at,
		HasSnapshot:  strings.TrimSpace(alert.SnapshotPath) != "",
	}, true
}

// alertIdentity extracts the recognized identity an alert carries, if any:
//
//	plate → metadata.plate,      detail = "<color> <vehicleType>"
//	face  → metadata.personName, detail = "" (an unnamed face is NOT an identity)
//
// An unrecognized face is deliberately not an identity hit. "Unknown face" is a real and
// useful alert, but it names nobody, so returning it from an identity search would put
// rows in front of an operator that can never match what they typed — and, worse, would
// make a fruitless search look like it found something.
func alertIdentity(alert *entities.AlertEvent) (kind, identity, detail string) {
	meta := map[string]any{}
	if raw := strings.TrimSpace(alert.Metadata); raw != "" {
		if err := json.Unmarshal([]byte(raw), &meta); err != nil {
			return "", "", ""
		}
	}
	str := func(key string) string {
		if v, ok := meta[key].(string); ok {
			return strings.TrimSpace(v)
		}
		return ""
	}
	if plate := str("plate"); plate != "" {
		parts := make([]string, 0, 2)
		if c := str("color"); c != "" {
			parts = append(parts, c)
		}
		if vt := str("vehicleType"); vt != "" {
			parts = append(parts, vt)
		}
		return "plate", plate, strings.Join(parts, " ")
	}
	if person := str("personName"); person != "" {
		return "face", person, ""
	}
	return "", "", ""
}

// identityKindWanted resolves the requested identity kinds. An empty or unrecognized
// request means BOTH — an operator who did not narrow the search wants everything, and a
// typo in the parameter must not silently return an empty result set.
func identityKindWanted(kinds []string) (plate, face bool) {
	for _, k := range kinds {
		switch strings.ToLower(strings.TrimSpace(k)) {
		case "plate":
			plate = true
		case "face":
			face = true
		}
	}
	if !plate && !face {
		return true, true
	}
	return plate, face
}

// normalizeSearchLabels trims and de-duplicates the requested label filter.
func normalizeSearchLabels(labels []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(labels))
	for _, l := range labels {
		l = strings.TrimSpace(l)
		if l == "" || seen[l] {
			continue
		}
		seen[l] = true
		out = append(out, l)
	}
	sort.Strings(out)
	return out
}

// cameraNames maps camera id → display name for this node. A read failure yields an empty
// map rather than an error: a search that found sightings must not fail because the
// cosmetic half could not be loaded, and the caller falls back to the id.
func (s *SightingSearch) cameraNames(ctx context.Context) map[int64]string {
	out := map[int64]string{}
	if s.cameras == nil {
		return out
	}
	rows, _, err := s.cameras.Get(ctx, "", 1000, 0, nil, nil)
	if err != nil {
		return out
	}
	for _, cam := range rows {
		if cam == nil {
			continue
		}
		if name := strings.TrimSpace(cam.Name); name != "" {
			out[cam.Id] = name
		}
	}
	return out
}
