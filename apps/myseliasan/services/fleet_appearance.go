package services

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// Federated appearance search (W3-2): "where else in the estate did this go?"
//
// An operator watching one recorder picks a sighting; this asks every node they can reach
// whether it saw anything that looks like it, and merges the ranked answers.
//
// THE QUERY TRAVELS, NOT THE INDEX — the same call W2-4 made, and here it is not even close.
// Replicating appearance descriptors upward would move a two-kilobyte vector per sighting
// per camera into the control plane, which is the estate's entire detection volume again in
// a form that only grows. The node already holds the vectors AND the footage a hit must link
// to, and the control plane already has an authenticated, authorized, audited transport.
//
// TWO HOPS, NOT ONE, AND THAT IS THE INTERESTING PART. The operator names a sighting on one
// node. That id means nothing anywhere else, so the control plane first fetches the
// DESCRIPTOR from the node that holds it, then fans the descriptor out. A federated search
// that skipped the first hop and passed the id around would return results only from the
// node that happened to record the sighting — and would look like it worked.
//
// The coverage vocabulary is shared verbatim with the object search, because an
// investigator's question is the same one: an empty result must distinguish "nothing in the
// estate looks like this" from "the depot recorder has been unreachable for a week".

const (
	// The node endpoints federated over. Both sit under the Objects page grant, so a role
	// that can search this node's object index can be asked these too, and no role gains
	// reach because a fleet feature shipped.
	fleetAppearanceNodePathVector = "/api/observations/appearance/vector"
	fleetAppearanceNodePathSearch = "/api/observations/appearance"
)

// FleetAppearanceHit is one ranked sighting, qualified with where in the fleet it came from.
type FleetAppearanceHit struct {
	NodeId        string  `json:"nodeId"`
	NodeName      string  `json:"nodeName"`
	SiteId        int64   `json:"siteId,omitempty"`
	SiteName      string  `json:"siteName,omitempty"`
	ObservationId int64   `json:"observationId"`
	CameraId      int64   `json:"cameraId"`
	SeenAt        int64   `json:"seenAt"`
	Label         string  `json:"label"`
	Similarity    float64 `json:"similarity"`
	Standout      float64 `json:"standout"`
	Confidence    float64 `json:"confidence"`
}

// FleetAppearanceResult is a whole federated appearance search.
type FleetAppearanceResult struct {
	Items    []FleetAppearanceHit `json:"items"`
	Total    int                  `json:"total"`
	Coverage SearchCoverage       `json:"coverage"`
	// Scanned is how many descriptors the whole fleet actually compared. An empty list
	// after 40,000 comparisons and an empty list after 3 are different answers, and the
	// list alone tells them apart for nobody.
	Scanned   int  `json:"scanned"`
	Truncated bool `json:"truncated"`
	// Model and Label echo the feature space and class the ranking ran in. Reported
	// because a node running a different embedder contributes nothing and must be seen to
	// have contributed nothing, rather than merely appearing to have found no matches.
	Model         string  `json:"model"`
	Label         string  `json:"label"`
	MinStandout float64 `json:"minStandout"`
}

// FleetAppearanceQuery is one federated appearance search.
type FleetAppearanceQuery struct {
	// SourceNodeId + SourceObservationId name the sighting the operator picked. The
	// control plane resolves them to a descriptor before fanning out.
	SourceNodeId        string
	SourceObservationId int64
	From, To            int64
	// SiteId / NodeId narrow which nodes are asked, exactly as an object search does.
	SiteId        int64
	NodeId        string
	// MinStandout is the relative floor, in robust deviations above the median similarity
	// of everything compared. Each node calibrates against ITS OWN candidate set, which is
	// the right unit: "stands out at this site" is the question, and a fleet-wide
	// distribution would let a busy site's spread decide what a quiet one may report.
	MinStandout float64
	Limit       int
}

// AppearanceSearch runs a fleet-wide appearance search on behalf of roleId.
func (s *FleetSearchService) AppearanceSearch(ctx context.Context, roleId int64, q FleetAppearanceQuery) (FleetAppearanceResult, error) {
	out := FleetAppearanceResult{Items: []FleetAppearanceHit{}, MinStandout: q.MinStandout}
	if strings.TrimSpace(q.SourceNodeId) == "" || q.SourceObservationId <= 0 {
		return out, fmt.Errorf("a source node and sighting are required")
	}

	// Hop one: get the descriptor from the node that holds the sighting. This is done
	// against the SAME access rules as the search itself — resolving the query through a
	// node the caller cannot reach would let them search by a sighting they are not
	// allowed to see, which is a read they do not have dressed up as a query.
	sourceTargets, _, err := s.targets(ctx, roleId, FleetSearchQuery{NodeId: q.SourceNodeId})
	if err != nil {
		return out, err
	}
	if len(sourceTargets) == 0 {
		return out, fmt.Errorf("the recorder holding that sighting is not one you can search")
	}
	vector, model, label, err := s.fetchQueryVector(ctx, sourceTargets[0], q.SourceObservationId)
	if err != nil {
		return out, err
	}
	out.Model, out.Label = model, label

	targets, skippedKind, err := s.targets(ctx, roleId, FleetSearchQuery{SiteId: q.SiteId, NodeId: q.NodeId})
	if err != nil {
		return out, err
	}
	out.Coverage.SkippedKind = skippedKind
	out.Coverage.Searched = len(targets)
	if len(targets) == 0 {
		out.Coverage.Nodes = []NodeCoverage{}
		out.Coverage.Complete = false
		return out, nil
	}

	nodeQuery := appearanceNodeQuery(vector, model, label, q)

	ctx, cancel := context.WithTimeout(ctx, s.searchBudget())
	defer cancel()

	coverage := make([]NodeCoverage, len(targets))
	hits := make([][]FleetAppearanceHit, len(targets))
	scanned := make([]int, len(targets))

	sem := make(chan struct{}, fleetSearchConcurrency)
	var wg sync.WaitGroup
	for i, target := range targets {
		wg.Add(1)
		go func(i int, target searchTarget) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			cov := NodeCoverage{
				NodeId:   target.node.NodeId,
				NodeName: nodeLabel(target.node),
				SiteId:   target.node.SiteId,
				SiteName: target.site,
				Sources:  []SourceCoverage{},
			}
			// The node the sighting came from is asked to EXCLUDE it, so the operator's own
			// pick does not come back as its own best match at 1.00.
			path := fleetAppearanceNodePathSearch + "?" + nodeQuery
			if target.node.NodeId == q.SourceNodeId {
				path += "&excludeObservationId=" + strconv.FormatInt(q.SourceObservationId, 10)
			}
			sc, rows, n := s.askNodeAppearance(ctx, target, path)
			cov.Sources = append(cov.Sources, sc)
			cov.Status, cov.Reason = rollUpSourceStatus(cov.Sources)
			coverage[i] = cov
			hits[i] = rows
			scanned[i] = n
		}(i, target)
	}
	wg.Wait()

	merged := make([]FleetAppearanceHit, 0, 64)
	for i, batch := range hits {
		merged = append(merged, batch...)
		out.Scanned += scanned[i]
	}
	// Most similar first, then the clearer view, then newest, then node — deterministic to
	// the last tie-break because an investigator comparing two runs of the same search must
	// not see rows shuffle.
	sort.SliceStable(merged, func(a, b int) bool {
		// Ordered by STANDOUT, not raw similarity. Each node calibrates against its own
		// candidate set, so "0.97 at the depot" and "0.97 at the gate" are not comparable
		// quantities — how far each stands out from its own crowd is.
		if merged[a].Standout != merged[b].Standout {
			return merged[a].Standout > merged[b].Standout
		}
		if merged[a].Confidence != merged[b].Confidence {
			return merged[a].Confidence > merged[b].Confidence
		}
		if merged[a].SeenAt != merged[b].SeenAt {
			return merged[a].SeenAt > merged[b].SeenAt
		}
		return merged[a].NodeId < merged[b].NodeId
	})

	limit := q.Limit
	if limit <= 0 || limit > fleetSearchMaxLimit {
		limit = fleetSearchMaxLimit
	}
	if len(merged) > limit {
		merged = merged[:limit]
		out.Truncated = true
	}
	out.Items = merged
	out.Total = len(merged)
	out.Coverage.Nodes = coverage
	out.Coverage.Answered, out.Coverage.Complete, out.Coverage.CompleteThrough = summarizeCoverage(coverage)
	return out, nil
}

// fetchQueryVector performs hop one: the descriptor for the operator's chosen sighting.
func (s *FleetSearchService) fetchQueryVector(ctx context.Context, target searchTarget, observationId int64) ([]float32URL, string, string, error) {
	path := fleetAppearanceNodePathVector + "?observationId=" + strconv.FormatInt(observationId, 10)
	body, status, err := s.tunnel(ctx, target, path)
	if err != nil {
		return nil, "", "", fmt.Errorf("could not reach the recorder holding that sighting: %w", err)
	}
	if status != http.StatusOK {
		// A node that cannot describe the sighting is a hard stop rather than an empty
		// result: there is no question to ask the rest of the fleet, and returning "no
		// matches" would say the estate never saw anything like it.
		return nil, "", "", fmt.Errorf("that sighting has no appearance description on %s", nodeLabel(target.node))
	}
	var payload struct {
		Vector string `json:"vector"`
		Model  string `json:"model"`
		Label  string `json:"label"`
	}
	if derr := decodeNodeResult(body, &payload); derr != nil {
		return nil, "", "", fmt.Errorf("unreadable answer from %s: %w", nodeLabel(target.node), derr)
	}
	if strings.TrimSpace(payload.Vector) == "" || strings.TrimSpace(payload.Model) == "" {
		return nil, "", "", fmt.Errorf("that sighting has no appearance description on %s", nodeLabel(target.node))
	}
	return []float32URL{float32URL(payload.Vector)}, payload.Model, payload.Label, nil
}

// float32URL is the already-encoded wire form of a vector. It is passed around as the
// encoded string rather than decoded and re-encoded on this side: the control plane has no
// reason to look inside a descriptor, and a decode/re-encode round trip is a place for the
// bytes to change without anything noticing.
type float32URL string

func appearanceNodeQuery(vector []float32URL, model, label string, q FleetAppearanceQuery) string {
	v := url.Values{}
	if len(vector) > 0 {
		v.Set("vector", string(vector[0]))
	}
	v.Set("model", model)
	v.Set("label", label)
	if q.From > 0 {
		v.Set("from", strconv.FormatInt(q.From, 10))
	}
	if q.To > 0 {
		v.Set("to", strconv.FormatInt(q.To, 10))
	}
	if q.MinStandout > 0 {
		v.Set("minStandout", strconv.FormatFloat(q.MinStandout, 'f', 4, 64))
	}
	if q.Limit > 0 {
		v.Set("limit", strconv.Itoa(q.Limit))
	}
	return v.Encode()
}

func (s *FleetSearchService) askNodeAppearance(ctx context.Context, target searchTarget, path string) (SourceCoverage, []FleetAppearanceHit, int) {
	cov := SourceCoverage{Source: SearchSourceObjects}
	body, status, err := s.tunnel(ctx, target, path)
	if err != nil || status != http.StatusOK {
		cov.Status, cov.Reason = classifySearchFailure(status, err)
		return cov, nil, 0
	}
	var page struct {
		Hits []struct {
			ObservationId int64   `json:"observationId"`
			CameraId      int64   `json:"cameraId"`
			SeenAt        int64   `json:"seenAt"`
			Label         string  `json:"label"`
			Similarity    float64 `json:"similarity"`
			Standout      float64 `json:"standout"`
			Confidence    float64 `json:"confidence"`
		} `json:"hits"`
		Scanned int `json:"scanned"`
	}
	if derr := decodeNodeResult(body, &page); derr != nil {
		cov.Status = SearchNodeError
		cov.Reason = "unreadable answer: " + derr.Error()
		return cov, nil, 0
	}
	rows := make([]FleetAppearanceHit, 0, len(page.Hits))
	for _, h := range page.Hits {
		rows = append(rows, FleetAppearanceHit{
			NodeId:        target.node.NodeId,
			NodeName:      nodeLabel(target.node),
			SiteId:        target.node.SiteId,
			SiteName:      target.site,
			ObservationId: h.ObservationId,
			CameraId:      h.CameraId,
			SeenAt:        h.SeenAt,
			Label:         h.Label,
			Similarity:    h.Similarity,
			Standout:      h.Standout,
			Confidence:    h.Confidence,
		})
	}
	cov.Status = SearchNodeOk
	cov.Count = len(rows)
	return cov, rows, page.Scanned
}
