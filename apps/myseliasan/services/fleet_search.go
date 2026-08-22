package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mysayasan/kopiv2/apps/myseliasan/entities"
	"github.com/mysayasan/kopiv2/infra/control"
)

// Federated cross-node search (W2-4, finding F-10).
//
// "Where was this seen?" is the question a fleet of recorders exists to answer, and until
// now it could only be asked one appliance at a time. This asks every node the operator can
// reach, at once, and merges the answers.
//
// WHY THE QUERY IS FEDERATED RATHER THAN THE INDEX REPLICATED UPWARD. The alternative was
// to copy each node's observation index into the control plane and search locally. That
// index is the highest-volume derived data in the product — one row per label-presence span
// per camera — so replicating it makes the control-plane database grow with the whole
// estate's detection volume, and it needs a queue on every appliance, a retention policy of
// its own, and a backfill for anything that was written while the link was down. The node
// already holds the index AND the footage the answer must link to, and the control plane
// already has an authenticated, authorized, audited transport to it. So the query travels
// instead of the data.
//
// That choice has one real cost: a node that is offline contributes nothing. THAT IS NOT A
// BUG TO BE HIDDEN — it is the single most important thing this service reports. An
// investigator who searches for a vehicle and is shown an empty result must be able to tell
// "the fleet never saw it" from "the depot recorder has been unreachable for a week".
// Replication would have papered over exactly that distinction with stale data, which is
// worse: it answers confidently and wrongly. Every result here carries a Coverage block
// saying which nodes answered, which did not, and why.

const (
	// fleetSearchNodeTimeout bounds ONE node's contribution. A wedged appliance must not
	// hold an interactive search open; it is reported as a timeout, which is a fact about
	// that node, not an absence of sightings.
	fleetSearchNodeTimeout = 15 * time.Second
	// fleetSearchConcurrency bounds how many nodes are queried at once. A fleet search
	// otherwise puts one tunneled request per node onto the control channel simultaneously.
	fleetSearchConcurrency = 8
	// fleetSearchBudget bounds the WHOLE search, not just each node.
	//
	// Without it the worst case is the per-node timeout multiplied by the number of batches:
	// a fifty-node estate where every appliance is wedged would hold the operator's request
	// open for minutes and then answer. The budget turns that into a bounded answer with the
	// nodes it could not get to reported as timed out — which is the same contract as every
	// other failure here: say what was not covered rather than take longer pretending to.
	fleetSearchBudget = 45 * time.Second
	// fleetSearchDefaultLimit / fleetSearchMaxLimit bound the merged result set.
	fleetSearchDefaultLimit = 200
	fleetSearchMaxLimit     = 2000
	// fleetSearchNodePathObjects / fleetSearchNodePathIdentities are the node endpoints
	// this federates over. They are separate because they are governed by separate node
	// grants — see the comments on the routes themselves.
	fleetSearchNodePathObjects    = "/api/observations/search"
	fleetSearchNodePathIdentities = "/api/vision/alerts/identities"
)

// Per-node / per-source outcomes.
//
// The vocabulary keeps apart four things that all look like "no results" if they are not
// named, and which an operator would act on completely differently.
const (
	// SearchNodeOk means the node answered.
	SearchNodeOk = "ok"
	// SearchNodeOffline means the node has no live control channel — nothing was asked.
	SearchNodeOffline = "offline"
	// SearchNodeTimeout means the node was asked and did not answer in time.
	SearchNodeTimeout = "timeout"
	// SearchNodeDenied means the node refused the request: the role the tunnel asserts is
	// not permitted to read that data THERE. Reported rather than swallowed, because the
	// fix is a grant on the node, not a different search.
	SearchNodeDenied = "denied"
	// SearchNodeUnsupported means the node has no such endpoint — an older build. A mixed-
	// version fleet is the normal state during a rollout, and an old node silently
	// contributing zero rows is how a search quietly stops covering half the estate.
	SearchNodeUnsupported = "unsupported"
	// SearchNodeError is any other failure.
	SearchNodeError = "error"
)

// Search sources.
const (
	SearchSourceObjects    = "objects"
	SearchSourceIdentities = "identities"
)

// FleetSightingHit is one sighting, qualified with where in the fleet it came from.
//
// The node-local fields are decoded verbatim from the node's answer rather than re-modelled
// here: the node owns the meaning of a sighting, and a second definition of it on this side
// is a second thing to keep in step.
type FleetSightingHit struct {
	NodeId   string `json:"nodeId"`
	NodeName string `json:"nodeName"`
	SiteId   int64  `json:"siteId,omitempty"`
	SiteName string `json:"siteName,omitempty"`

	Kind         string  `json:"kind"`
	Id           int64   `json:"id"`
	CameraId     int64   `json:"cameraId"`
	CameraName   string  `json:"cameraName,omitempty"`
	Label        string  `json:"label"`
	Identity     string  `json:"identity,omitempty"`
	IdentityKind string  `json:"identityKind,omitempty"`
	StartedAt    int64   `json:"startedAt"`
	EndedAt      int64   `json:"endedAt"`
	Confidence   float64 `json:"confidence"`
	Count        int     `json:"count,omitempty"`

	PeakBox        string `json:"peakBox,omitempty"`
	PeakAt         int64  `json:"peakAt,omitempty"`
	SegmentId      int64  `json:"segmentId,omitempty"`
	SegmentCodec   string `json:"segmentCodec,omitempty"`
	SeekSeconds    int64  `json:"seekSeconds,omitempty"`
	FootagePending bool   `json:"footagePending,omitempty"`
	HasSnapshot    bool   `json:"hasSnapshot,omitempty"`
}

// SourceCoverage is what one source on one node contributed.
type SourceCoverage struct {
	Source string `json:"source"`
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
	Count  int    `json:"count"`
	// Capped is the node saying it returned a prefix of its matches.
	Capped bool `json:"capped"`
	// Oldest is how far back the returned prefix reaches (0 when it returned nothing).
	Oldest int64 `json:"oldest,omitempty"`
}

// NodeCoverage is one node's contribution to a fleet search.
type NodeCoverage struct {
	NodeId   string           `json:"nodeId"`
	NodeName string           `json:"nodeName"`
	SiteId   int64            `json:"siteId,omitempty"`
	SiteName string           `json:"siteName,omitempty"`
	Status   string           `json:"status"`
	Reason   string           `json:"reason,omitempty"`
	Sources  []SourceCoverage `json:"sources"`
}

// SearchCoverage is the honest account of what the search actually covered.
type SearchCoverage struct {
	// Nodes lists every node that was searched, answered or not.
	//
	// Nodes the requesting role has NO access to are absent entirely rather than listed as
	// denied: the fleet a user can see is the fleet they have access to, and enumerating
	// the rest would leak the estate's shape to someone granted one recorder.
	Nodes []NodeCoverage `json:"nodes"`
	// Searched / Answered count nodes, not sources. Answered counts a node that returned at
	// least one source successfully.
	Searched int `json:"searched"`
	Answered int `json:"answered"`
	// Complete is true only when every searched node answered every requested source AND
	// none of them capped. It is the one field a caller may reduce this to.
	Complete bool `json:"complete"`
	// CompleteThrough is the timestamp back to which the result set IS complete, when some
	// node capped. Sightings at or after it are all here; older ones may be missing. 0 means
	// nothing capped — in which case completeness is decided by Complete alone.
	CompleteThrough int64 `json:"completeThrough,omitempty"`
	// Skipped counts accessible nodes that were not searched because they cannot hold
	// sightings at all (an IoT hub, a door controller). Reported so "5 of 9 nodes" never
	// reads as four failures.
	SkippedKind int `json:"skippedKind"`
}

// FleetSearchResult is a whole fleet search.
type FleetSearchResult struct {
	Items    []FleetSightingHit `json:"items"`
	Total    int                `json:"total"`
	Coverage SearchCoverage     `json:"coverage"`
	// Truncated is true when the merged set exceeded the requested limit and was cut. It is
	// distinct from a node's own cap: this one is the control plane running out of room.
	Truncated bool `json:"truncated"`
}

// FleetLabelsResult is the fleet-wide union of recorded object labels, with the same
// coverage account — a label picker built from four of nine nodes is offering an operator a
// filter list that will silently exclude what the other five saw.
type FleetLabelsResult struct {
	Labels   []string       `json:"labels"`
	Coverage SearchCoverage `json:"coverage"`
}

// FleetSearchQuery is one fleet-wide search.
//
// There is deliberately NO offset. A global offset cannot be honoured across independent
// sources — each node pages its own result set, so "skip the first 200 fleet-wide" has no
// meaning any node can implement, and the usual workaround (over-fetch then slice) silently
// drops rows the moment any node caps. Narrow the window instead; the coverage block says
// when the window is too wide.
type FleetSearchQuery struct {
	From int64
	To   int64
	// Sources selects "objects" and/or "identities" (empty = both).
	Sources []string
	// Labels filters object hits to these detector labels (empty = any).
	Labels []string
	// Text is the identity substring — a plate, part of one, or a person's name.
	Text string
	// IdentityKinds narrows identity hits to "plate" and/or "face" (empty = both).
	IdentityKinds []string
	// MinConfidence is a 0..1 floor.
	MinConfidence float64
	// SiteId restricts the search to nodes at one site (0 = every site).
	SiteId int64
	// NodeId restricts the search to one node (empty = every accessible node).
	NodeId string
	// Limit caps the merged result set.
	Limit int
}

// searchNodeLister is the slice of the node registry a fleet search needs.
//
// Narrowed for the same reason the policy reconciler narrows its own: a read-only feature
// holding the adoption and revocation surface is one refactor away from being able to
// release a node.
type searchNodeLister interface {
	List(ctx context.Context) ([]*entities.ManagedNode, error)
}

// searchSiteNamer resolves site ids to names for the result rows.
type searchSiteNamer interface {
	ListSites(ctx context.Context) ([]*entities.Site, error)
}

// FleetSearchService answers fleet-wide sighting searches by fanning out over the control
// channel.
type FleetSearchService struct {
	nodes  searchNodeLister
	sites  searchSiteNamer
	sender ControlSender
	access INodeAccessService
	// budget bounds one whole search. It matters precisely when the CALLER has no deadline
	// of its own, which is the normal case — a browser waiting on the endpoint imposes
	// none. Held as a field so a test can shrink it; nothing else should set it.
	budget time.Duration
}

// NewFleetSearchService builds the federation service. sites may be nil (site names are
// then omitted, which degrades a label, not a result).
func NewFleetSearchService(nodes searchNodeLister, sites searchSiteNamer, sender ControlSender, access INodeAccessService) *FleetSearchService {
	return &FleetSearchService{nodes: nodes, sites: sites, sender: sender, access: access, budget: fleetSearchBudget}
}

// searchTarget is one node the search will visit.
type searchTarget struct {
	node *entities.ManagedNode
	role string
	site string
}

// Search runs a fleet-wide sighting search on behalf of roleId.
func (s *FleetSearchService) Search(ctx context.Context, roleId int64, q FleetSearchQuery) (FleetSearchResult, error) {
	out := FleetSearchResult{Items: []FleetSightingHit{}}
	targets, skippedKind, err := s.targets(ctx, roleId, q)
	if err != nil {
		return out, err
	}
	out.Coverage.SkippedKind = skippedKind
	out.Coverage.Searched = len(targets)
	if len(targets) == 0 {
		// No node was searched, so nothing is known about the window. Reporting this as a
		// complete, empty result is the failure mode this whole file exists to prevent.
		out.Coverage.Nodes = []NodeCoverage{}
		out.Coverage.Complete = false
		return out, nil
	}

	wantObjects, wantIdentities := searchSourcesWanted(q.Sources)
	nodeQuery := s.nodeQueryString(q)

	ctx, cancel := context.WithTimeout(ctx, s.searchBudget())
	defer cancel()

	coverage := make([]NodeCoverage, len(targets))
	hits := make([][]FleetSightingHit, len(targets))

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
			var collected []FleetSightingHit
			if wantObjects {
				sc, rows := s.askNode(ctx, target, SearchSourceObjects, fleetSearchNodePathObjects, nodeQuery)
				cov.Sources = append(cov.Sources, sc)
				collected = append(collected, rows...)
			}
			if wantIdentities {
				sc, rows := s.askNode(ctx, target, SearchSourceIdentities, fleetSearchNodePathIdentities, nodeQuery)
				cov.Sources = append(cov.Sources, sc)
				collected = append(collected, rows...)
			}
			cov.Status, cov.Reason = rollUpSourceStatus(cov.Sources)
			coverage[i] = cov
			hits[i] = collected
		}(i, target)
	}
	wg.Wait()

	merged := make([]FleetSightingHit, 0, 64)
	for _, batch := range hits {
		merged = append(merged, batch...)
	}
	// Newest first, then by node and id so a tie is ordered deterministically — an
	// investigator comparing two runs of the same search must not see rows shuffle.
	sort.SliceStable(merged, func(a, b int) bool {
		if merged[a].StartedAt != merged[b].StartedAt {
			return merged[a].StartedAt > merged[b].StartedAt
		}
		if merged[a].NodeId != merged[b].NodeId {
			return merged[a].NodeId < merged[b].NodeId
		}
		return merged[a].Id < merged[b].Id
	})

	limit := clampFleetLimit(q.Limit)
	if len(merged) > limit {
		merged = merged[:limit]
		out.Truncated = true
	}
	out.Items = merged
	out.Total = len(merged)

	sort.SliceStable(coverage, func(a, b int) bool { return coverage[a].NodeId < coverage[b].NodeId })
	out.Coverage.Nodes = coverage
	out.Coverage.Answered, out.Coverage.Complete, out.Coverage.CompleteThrough = summarizeCoverage(coverage)
	if out.Truncated {
		// The control plane cut the merged set, so completeness now ends at the oldest row
		// still present — regardless of what the nodes said. Reporting Complete here would
		// be claiming the estate saw exactly what fits on one page.
		out.Coverage.Complete = false
		if n := len(out.Items); n > 0 {
			oldestKept := out.Items[n-1].StartedAt
			if oldestKept > out.Coverage.CompleteThrough {
				out.Coverage.CompleteThrough = oldestKept
			}
		}
	}
	return out, nil
}

// Labels returns the union of object labels recorded across the searchable fleet.
func (s *FleetSearchService) Labels(ctx context.Context, roleId int64, q FleetSearchQuery) (FleetLabelsResult, error) {
	out := FleetLabelsResult{Labels: []string{}}
	targets, skippedKind, err := s.targets(ctx, roleId, q)
	if err != nil {
		return out, err
	}
	out.Coverage.SkippedKind = skippedKind
	out.Coverage.Searched = len(targets)
	out.Coverage.Nodes = []NodeCoverage{}
	if len(targets) == 0 {
		return out, nil
	}

	ctx, cancel := context.WithTimeout(ctx, s.searchBudget())
	defer cancel()

	coverage := make([]NodeCoverage, len(targets))
	perNode := make([][]string, len(targets))
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
			}
			sc, labels := s.askNodeLabels(ctx, target)
			cov.Sources = []SourceCoverage{sc}
			cov.Status, cov.Reason = rollUpSourceStatus(cov.Sources)
			coverage[i] = cov
			perNode[i] = labels
		}(i, target)
	}
	wg.Wait()

	seen := map[string]bool{}
	for _, labels := range perNode {
		for _, l := range labels {
			if l = strings.TrimSpace(l); l != "" && !seen[l] {
				seen[l] = true
				out.Labels = append(out.Labels, l)
			}
		}
	}
	sort.Strings(out.Labels)
	sort.SliceStable(coverage, func(a, b int) bool { return coverage[a].NodeId < coverage[b].NodeId })
	out.Coverage.Nodes = coverage
	out.Coverage.Answered, out.Coverage.Complete, out.Coverage.CompleteThrough = summarizeCoverage(coverage)
	return out, nil
}

// targets resolves which nodes this search will visit, and how many accessible nodes were
// left out because their kind cannot hold sightings.
func (s *FleetSearchService) targets(ctx context.Context, roleId int64, q FleetSearchQuery) ([]searchTarget, int, error) {
	nodes, err := s.nodes.List(ctx)
	if err != nil {
		return nil, 0, err
	}
	siteNames := s.siteNames(ctx)
	targets := make([]searchTarget, 0, len(nodes))
	skippedKind := 0
	for _, node := range nodes {
		if node == nil {
			continue
		}
		if q.NodeId != "" && node.NodeId != q.NodeId {
			continue
		}
		if q.SiteId > 0 && node.SiteId != q.SiteId {
			continue
		}
		// Access first, kind second. A node the role cannot reach must not even be counted
		// as skipped — that count would still disclose that it exists.
		role := ""
		if s.access != nil {
			acc, aerr := s.access.Resolve(ctx, roleId, node.NodeId)
			if aerr != nil {
				return nil, 0, aerr
			}
			role = acc.Role()
			if role == "" {
				continue
			}
		} else {
			role = "viewer"
		}
		if !nodeKindHoldsSightings(node.Kind) {
			skippedKind++
			continue
		}
		targets = append(targets, searchTarget{node: node, role: role, site: siteNames[node.SiteId]})
	}
	return targets, skippedKind, nil
}

// nodeQueryString renders the query terms the node endpoints understand. Both node paths
// take the same parameters, so one string serves both and the two halves of a search cannot
// drift apart in what they were asked.
func (s *FleetSearchService) nodeQueryString(q FleetSearchQuery) string {
	v := url.Values{}
	if q.From > 0 {
		v.Set("from", strconv.FormatInt(q.From, 10))
	}
	if q.To > 0 {
		v.Set("to", strconv.FormatInt(q.To, 10))
	}
	if len(q.Labels) > 0 {
		v.Set("labels", strings.Join(q.Labels, ","))
	}
	if len(q.IdentityKinds) > 0 {
		v.Set("identityKinds", strings.Join(q.IdentityKinds, ","))
	}
	if strings.TrimSpace(q.Text) != "" {
		v.Set("text", strings.TrimSpace(q.Text))
	}
	if q.MinConfidence > 0 {
		v.Set("minConfidence", strconv.FormatFloat(q.MinConfidence, 'f', -1, 64))
	}
	// Every node is asked for the full merged limit. Asking each for limit/N would be
	// cheaper and wrong: the fleet's newest N sightings can all come from one busy node,
	// and dividing the budget evenly would drop real hits to make room for rows that do not
	// exist elsewhere.
	v.Set("limit", strconv.Itoa(clampFleetLimit(q.Limit)))
	return v.Encode()
}

// nodeSightingPage mirrors the node's SightingPage on the wire.
type nodeSightingPage struct {
	Items  []FleetSightingHit `json:"items"`
	Capped bool               `json:"capped"`
	Oldest int64              `json:"oldest"`
}

// askNode runs one source against one node and converts the outcome into coverage.
func (s *FleetSearchService) askNode(ctx context.Context, target searchTarget, source, path, query string) (SourceCoverage, []FleetSightingHit) {
	cov := SourceCoverage{Source: source}
	full := path
	if query != "" {
		full += "?" + query
	}
	body, status, err := s.tunnel(ctx, target, full)
	if err != nil || status != http.StatusOK {
		cov.Status, cov.Reason = classifySearchFailure(status, err)
		return cov, nil
	}
	var page nodeSightingPage
	if derr := decodeNodeResult(body, &page); derr != nil {
		cov.Status = SearchNodeError
		cov.Reason = "unreadable answer: " + derr.Error()
		return cov, nil
	}
	rows := make([]FleetSightingHit, 0, len(page.Items))
	for _, hit := range page.Items {
		hit.NodeId = target.node.NodeId
		hit.NodeName = nodeLabel(target.node)
		hit.SiteId = target.node.SiteId
		hit.SiteName = target.site
		rows = append(rows, hit)
	}
	cov.Status = SearchNodeOk
	cov.Count = len(rows)
	cov.Capped = page.Capped
	cov.Oldest = page.Oldest
	return cov, rows
}

// askNodeLabels fetches one node's recorded object labels.
func (s *FleetSearchService) askNodeLabels(ctx context.Context, target searchTarget) (SourceCoverage, []string) {
	cov := SourceCoverage{Source: SearchSourceObjects}
	body, status, err := s.tunnel(ctx, target, "/api/observations/labels")
	if err != nil || status != http.StatusOK {
		cov.Status, cov.Reason = classifySearchFailure(status, err)
		return cov, nil
	}
	var labels []string
	if derr := decodeNodeResult(body, &labels); derr != nil {
		cov.Status = SearchNodeError
		cov.Reason = "unreadable answer: " + derr.Error()
		return cov, nil
	}
	cov.Status = SearchNodeOk
	cov.Count = len(labels)
	return cov, labels
}

// tunnel sends one GET to a node over the control channel, under its own deadline.
func (s *FleetSearchService) tunnel(ctx context.Context, target searchTarget, path string) ([]byte, int, error) {
	if s.sender == nil {
		return nil, 0, errors.New("control channel unavailable")
	}
	nodeCtx, cancel := context.WithTimeout(ctx, fleetSearchNodeTimeout)
	defer cancel()
	resp, err := s.sender.SendRequest(nodeCtx, target.node.NodeId, control.Request{
		Method: http.MethodGet,
		Path:   path,
		Role:   target.role,
		Actor:  "fleet-search",
	})
	if err != nil {
		return nil, 0, err
	}
	return resp.Body, resp.Status, nil
}

// decodeNodeResult unwraps the node's response envelope into out.
//
// The node's controllers.SendResult renders {message, durationMs, result}. Some paths in
// this estate have historically been double-wrapped as {data:{result}}, and a caller that
// only understood one shape would report a perfectly healthy node as broken — so all three
// are accepted, matching what the agent chat already does over the same tunnel.
func decodeNodeResult(body []byte, out any) error {
	var envelope struct {
		Result json.RawMessage `json:"result"`
		Data   *struct {
			Result json.RawMessage `json:"result"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err == nil {
		if envelope.Data != nil && len(envelope.Data.Result) > 0 {
			return json.Unmarshal(envelope.Data.Result, out)
		}
		if len(envelope.Result) > 0 {
			return json.Unmarshal(envelope.Result, out)
		}
	}
	return json.Unmarshal(body, out)
}

// classifySearchFailure turns a transport error or a non-200 node status into the coverage
// vocabulary. Each branch exists because the operator's next action differs: reconnect the
// node, grant a role on it, upgrade it, or read the error.
func classifySearchFailure(status int, err error) (string, string) {
	switch {
	case errors.Is(err, ErrNodeOffline), errors.Is(err, ErrNodeDisconnected):
		return SearchNodeOffline, "node is not connected"
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		// Either this node ran out its own deadline, or the whole search ran out of budget
		// before reaching it. Both mean the same thing to the operator — this node's
		// sightings are not in the answer — and neither may be rounded to "found nothing".
		return SearchNodeTimeout, fmt.Sprintf("node did not answer within %s", fleetSearchNodeTimeout)
	case err != nil:
		return SearchNodeError, err.Error()
	}
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return SearchNodeDenied, "the role this control plane asserts may not read that on the node"
	case http.StatusNotFound, http.StatusMethodNotAllowed:
		return SearchNodeUnsupported, "this node's build has no federated search endpoint"
	default:
		return SearchNodeError, fmt.Sprintf("node returned HTTP %d", status)
	}
}

// rollUpSourceStatus reduces a node's per-source outcomes to one node status.
//
// A node counts as ok only when EVERY source it was asked for answered. Partial success is
// reported as the failing source's status, because the half that failed is the half the
// operator has to act on — and calling a node "ok" when its identity search was denied is
// how "this plate was never seen here" gets said about a node that was never asked.
func rollUpSourceStatus(sources []SourceCoverage) (string, string) {
	if len(sources) == 0 {
		return SearchNodeError, "no source was queried"
	}
	worst, reason := SearchNodeOk, ""
	for _, sc := range sources {
		if sc.Status == SearchNodeOk {
			continue
		}
		if worst == SearchNodeOk {
			worst, reason = sc.Status, sc.Reason
		}
	}
	return worst, reason
}

// summarizeCoverage counts answering nodes and works out the completeness horizon.
//
// A node "answered" when at least one of its sources came back — it contributed evidence,
// even if not all of it. Completeness is stricter: every source of every node must have
// answered and none of them capped.
//
// CompleteThrough is the NEWEST of the capped sources' horizons, not the oldest. Each capped
// source is complete only back to where its own prefix stops; the fleet is complete only
// back to the point every capped source still covers, which is the latest of those stops.
// Taking the oldest would claim coverage the earliest-stopping node never provided.
func summarizeCoverage(nodes []NodeCoverage) (answered int, complete bool, completeThrough int64) {
	complete = len(nodes) > 0
	for _, node := range nodes {
		nodeAnswered := false
		for _, sc := range node.Sources {
			if sc.Status != SearchNodeOk {
				complete = false
				continue
			}
			nodeAnswered = true
			if sc.Capped {
				complete = false
				if sc.Oldest > completeThrough {
					completeThrough = sc.Oldest
				}
			}
		}
		if len(node.Sources) == 0 {
			complete = false
		}
		if nodeAnswered {
			answered++
		}
	}
	return answered, complete, completeThrough
}

// searchSourcesWanted resolves the requested sources. An empty or unrecognized request means
// BOTH — a typo must not silently narrow an investigative search.
func searchSourcesWanted(sources []string) (objects, identities bool) {
	for _, s := range sources {
		switch strings.ToLower(strings.TrimSpace(s)) {
		case SearchSourceObjects:
			objects = true
		case SearchSourceIdentities:
			identities = true
		}
	}
	if !objects && !identities {
		return true, true
	}
	return objects, identities
}

// nodeKindHoldsSightings reports whether a node kind can hold camera sightings at all.
// Empty means camera, exactly as ManagedNode.Kind documents: every node adopted before that
// field existed is a mymatasan recorder.
func nodeKindHoldsSightings(kind string) bool {
	k := strings.ToLower(strings.TrimSpace(kind))
	return k == "" || k == "camera"
}

// nodeLabel is the node's display name, falling back to its id.
func nodeLabel(node *entities.ManagedNode) string {
	if name := strings.TrimSpace(node.Name); name != "" {
		return name
	}
	return node.NodeId
}

// siteNames maps site id → name. A read failure yields an empty map: a missing site label
// must not fail a search.
func (s *FleetSearchService) siteNames(ctx context.Context) map[int64]string {
	out := map[int64]string{}
	if s.sites == nil {
		return out
	}
	sites, err := s.sites.ListSites(ctx)
	if err != nil {
		return out
	}
	for _, site := range sites {
		if site != nil {
			out[site.Id] = site.Name
		}
	}
	return out
}

// searchBudget is the whole-search deadline, defaulted for a zero-valued service.
func (s *FleetSearchService) searchBudget() time.Duration {
	if s.budget <= 0 {
		return fleetSearchBudget
	}
	return s.budget
}

// clampFleetLimit normalizes the merged-result limit.
func clampFleetLimit(limit int) int {
	if limit <= 0 {
		return fleetSearchDefaultLimit
	}
	if limit > fleetSearchMaxLimit {
		return fleetSearchMaxLimit
	}
	return limit
}
