package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mysayasan/kopiv2/apps/myseliasan/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	"github.com/mysayasan/kopiv2/infra/control"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// N+1 node failover, the control-plane half (W3-7).
//
// A recorder holds its own cameras and its own footage, and nothing else records them. The
// control plane already knows which recorders are alive and can already reach any of them
// over the fleet tunnel; what it could not do was arrange for a SPARE to pick up a
// recorder's cameras when that recorder stops. That is this.
//
// THE ONE FACT EVERYTHING FOLLOWS FROM: when failover matters, the failed appliance is
// unreachable. Nothing can be fetched from it — not its camera list, not its credentials,
// not its recording settings. So the copy is taken EARLY and repeatedly, while it is
// healthy, and the spare is asked to PROVE it can open those cameras before anything has
// gone wrong. The proof is the feature; a spare nobody has drilled is a line in a contract.
//
// THE CONTROL PLANE CARRIES AN ENVELOPE IT CANNOT OPEN. Moving a camera set means moving
// camera credentials, and the obvious implementation — a "list my cameras with passwords"
// endpoint, relayed through here — would turn this service into a fleet-wide credential
// vault and that endpoint into a bulk dump readable by anything that can call it. Instead
// the SPARE mints a one-exchange key, the PROTECTED appliance seals its set to that key and
// binds it to the spare's node id, and this service relays the result. It never holds a
// camera password, never stores one, and could not decrypt a bundle if it wanted to; a
// bundle intercepted here cannot be staged onto any other appliance. See infra/handoff.
//
// WHAT IT DELIBERATELY WILL NOT DO, and why the alternative is worse:
//
//   - It never tells a returning appliance to stand down. A control plane that cannot reach
//     a node cannot tell "dead" from "on the far side of a partition and recording
//     perfectly". Fencing that node means being willing to stop the only thing recording,
//     on evidence that is definitionally incomplete. Failover here is ADDITIVE: at worst
//     two appliances record the same camera until an operator fails back, which costs a
//     duplicate stream and duplicate footage. Nothing recording is the one unrecoverable
//     outcome, and no path here can produce it.
//
//   - It does not fail back automatically. A recorder that returns for thirty seconds and
//     dies again would otherwise thrash every camera in the building between two appliances.
//     The return is a notification and a banner; the handover back is a decision.
//
//   - It does not recover the footage that was on the failed appliance. That footage is on
//     that appliance. Only the clips already pulled off it by the critical-clip archive
//     (W2-3) exist anywhere else. The screen says so in those words.

const (
	// failoverStageInterval is how often a healthy plan re-copies the camera set. An hour
	// is well inside the rate at which sites gain cameras, and each pass is three tunneled
	// calls — cheap next to the sweep the fleet policy reconciler already runs.
	failoverStageInterval = time.Hour
	// failoverDrillInterval is how often a staged plan is re-drilled unattended. Daily,
	// because the drill is the only thing that can catch the failures that develop while
	// nothing is happening: a VLAN change, a camera password rotated on the camera, a spare
	// moved to a different switch. A plan proved six months ago has not been proved.
	failoverDrillInterval = 24 * time.Hour
	// failoverFirstDrillDelay is how long a plan that has NEVER been drilled waits before
	// the sweep drills it unattended.
	//
	// It exists because "never" is not "long ago", and treating it as such broke the
	// feature's central distinction in the most literal way possible. `now - LastDrillAt`
	// on a plan that has never been drilled is fifty-five years, so the sweep drilled every
	// new plan on its first tick — and the badge an operator had just watched say "never
	// tested" went green by itself half a minute later, next to a sentence telling them to
	// press Test to find out whether the spare can reach the cameras. The product and its
	// own screen disagreed, and the difference between copied and PROVED — which is the
	// whole feature — was invisible in ordinary use.
	//
	// The unattended first drill is still worth having: it is the backstop for the plan
	// nobody revisits. It just does not happen the instant a form is saved, when nobody has
	// looked at the plan yet and a burst of camera logins across a site is the last thing
	// anyone asked for. Found by the screen pass.
	failoverFirstDrillDelay = 15 * time.Minute
	// failoverDefaultHoldDown is the default silence before a plan acts. Comfortably longer
	// than the liveness grace window (three heartbeats, floor 90s) so a routine restart
	// never triggers a takeover, and short enough that a genuinely dead recorder's cameras
	// are being recorded again within minutes.
	failoverDefaultHoldDown = 300
	// failoverMinHoldDown refuses a hold-down that cannot outlast the grace window. A plan
	// set to 10 seconds does not act 10 seconds after a failure — the node is not declared
	// lost for at least 90 — so the number would be a promise the system cannot keep.
	failoverMinHoldDown = 120
	// failoverRequestTimeout bounds one tunneled call. Staging a large site is a single
	// request carrying a sealed set, and a drill on the far side opens every camera.
	failoverRequestTimeout = 120 * time.Second
	// failoverMaxPlans caps a sweep.
	failoverMaxPlans = 500
)

// ErrFailoverPlanNotFound is returned for an id that is not a plan.
var ErrFailoverPlanNotFound = errors.New("no such failover plan")

// FailoverPlanView is a plan with everything the screen needs to judge it: the names of
// both appliances, whether each is reachable right now, and the plain-language reason it is
// or is not ready.
type FailoverPlanView struct {
	Plan *entities.FailoverPlan `json:"plan"`

	ProtectedName   string `json:"protectedName"`
	ProtectedStatus string `json:"protectedStatus"`
	ProtectedSeenAt int64  `json:"protectedSeenAt"`
	StandbyName     string `json:"standbyName"`
	StandbyStatus   string `json:"standbyStatus"`

	// Ready is the single question the screen leads with: if the protected appliance died
	// right now, would this work? It is TRUE only when a drill has actually proved every
	// staged camera reachable from the spare — never on the strength of a successful copy.
	Ready bool `json:"ready"`
	// ReadyState is a machine-readable reason, rendered by the SPA in the operator's own
	// language. A sentence composed here would arrive in English on an Arabic screen — the
	// exact defect W3-4 and W3-6 each shipped once and each had found by the screen pass.
	ReadyState string `json:"readyState"`
	// Cameras is the per-camera drill detail from the spare, when it has been asked.
	Cameras []FailoverCameraView `json:"cameras"`
}

// FailoverCameraView is one staged camera as the spare reported it.
type FailoverCameraView struct {
	Name        string `json:"name"`
	Host        string `json:"host"`
	State       string `json:"state"`
	CheckStatus string `json:"checkStatus"`
	CheckDetail string `json:"checkDetail"`
	CheckedAt   int64  `json:"checkedAt"`
	// Outcome is a STATE the appliance reported (mymatasan's StandbyOutcome* codes), never
	// a finished sentence — the SPA renders it in the operator's language. OutcomeDetail is
	// the machine's own words about a failure, which stay raw on purpose: they cannot be
	// enumerated in advance, and a paraphrase would be less useful to whoever has to act.
	Outcome       string `json:"outcome,omitempty"`
	OutcomeDetail string `json:"outcomeDetail,omitempty"`
}

// ReadyState values. Each names a DIFFERENT thing to go and do, which is the only reason to
// have more than one: "not staged" is a control-plane or connectivity problem, "untested"
// means press the drill button, "partial" sends somebody to specific cameras, and "blind"
// sends them to the network between the spare and the site.
const (
	FailoverReadyDisabled    = "disabled"
	FailoverReadyNotStaged   = "not-staged"
	FailoverReadyUntested    = "untested"
	FailoverReadyReady       = "ready"
	FailoverReadyPartial     = "partial"
	FailoverReadyBlind       = "blind"
	FailoverReadyActive      = "active"
	FailoverReadyStandbyDown = "standby-down"
)

// SaveFailoverPlanRequest creates or updates a plan.
type SaveFailoverPlanRequest struct {
	Id              int64  `json:"id"`
	Name            string `json:"name"`
	ProtectedNodeId string `json:"protectedNodeId"`
	StandbyNodeId   string `json:"standbyNodeId"`
	Enabled         bool   `json:"enabled"`
	AutoActivate    bool   `json:"autoActivate"`
	HoldDownSeconds int    `json:"holdDownSeconds"`
}

// IFailoverService owns the fleet's failover plans and every step of carrying one out.
type IFailoverService interface {
	List(ctx context.Context) ([]FailoverPlanView, error)
	Get(ctx context.Context, id int64) (*FailoverPlanView, error)
	Save(ctx context.Context, req SaveFailoverPlanRequest, actor int64) (*FailoverPlanView, error)
	Delete(ctx context.Context, id int64, actor int64) error
	// Stage copies the protected appliance's camera set onto the spare, now.
	Stage(ctx context.Context, id int64, actor int64) (*FailoverPlanView, error)
	// Drill asks the spare to open every staged camera and report what happened.
	Drill(ctx context.Context, id int64, actor int64) (*FailoverPlanView, error)
	// Activate hands the cameras to the spare. automatic=true is the sweep acting on its
	// own; it is recorded differently because "who decided this" is the first question.
	Activate(ctx context.Context, id int64, actor int64, automatic bool) (*FailoverPlanView, error)
	// Release hands them back. The footage the spare recorded stays on the spare.
	Release(ctx context.Context, id int64, actor int64) (*FailoverPlanView, error)
	// Sweep is the leader-gated tick: keep healthy plans staged and drilled, and act on
	// the ones whose recorder has gone quiet.
	Sweep(ctx context.Context)
}

// FailoverNodeSource is all this service needs of the node registry.
//
// Narrowed deliberately, the same way the fleet policy reconciler narrows it: INodeRegistry
// is the adoption, enrollment and REVOCATION surface, and a component that can take a
// building's cameras over should not be one refactor away from being able to release a node.
type FailoverNodeSource interface {
	List(ctx context.Context) ([]*entities.ManagedNode, error)
}

// FailoverNotifier publishes an operator-facing message. A function rather than the
// notification service, so the one place that decides what a failover event SOUNDS like is
// the app's wiring, next to every other fleet notification.
type FailoverNotifier func(kind string, plan *entities.FailoverPlan, protectedName, standbyName string, detail string)

// Notification kinds handed to the notifier.
const (
	FailoverNotifyReadyToActivate = "ready-to-activate"
	FailoverNotifyActivated       = "activated"
	FailoverNotifyActivateFailed  = "activate-failed"
	FailoverNotifyProtectedBack   = "protected-back"
	FailoverNotifyReleased        = "released"
	FailoverNotifyDrillFailed     = "drill-failed"
)

type failoverService struct {
	plans  dbsql.IGenericRepo[entities.FailoverPlan]
	nodes  FailoverNodeSource
	sender ControlSender
	audit  IAuditService
	notify FailoverNotifier
	logf   func(string, ...any)

	// sweeping serialises passes for the same reason the policy reconciler's does: the
	// half-minute ticker and an operator's button can overlap, and two passes would stage
	// the same plan twice — two sealed sets in flight to one spare, and, worse, two
	// takeovers racing.
	sweeping sync.Mutex
}

// NewFailoverService builds the control plane's failover service. audit and notify may be
// nil (tests); logf may be nil.
func NewFailoverService(
	db dbsql.IDbCrud,
	nodes FailoverNodeSource,
	sender ControlSender,
	audit IAuditService,
	notify FailoverNotifier,
	logf func(string, ...any),
) IFailoverService {
	return newFailoverServiceWith(dbsql.NewGenericRepo[entities.FailoverPlan](db), nodes, sender, audit, notify, logf)
}

// newFailoverServiceWith is the constructor the tests use, so the behaviour that decides
// whether a building keeps recording — when a plan counts as ready, when a takeover fires,
// what is sent to which appliance in what order — is exercised without a database standing
// between the assertion and the thing being asserted.
func newFailoverServiceWith(
	plans dbsql.IGenericRepo[entities.FailoverPlan],
	nodes FailoverNodeSource,
	sender ControlSender,
	audit IAuditService,
	notify FailoverNotifier,
	logf func(string, ...any),
) IFailoverService {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &failoverService{
		plans: plans,
		nodes: nodes, sender: sender, audit: audit, notify: notify, logf: logf,
	}
}

// --- reads ---------------------------------------------------------------------------

func (s *failoverService) List(ctx context.Context) ([]FailoverPlanView, error) {
	plans, err := s.allPlans(ctx)
	if err != nil {
		return nil, err
	}
	byNode, err := s.nodeIndex(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]FailoverPlanView, 0, len(plans))
	for _, plan := range plans {
		out = append(out, s.view(plan, byNode))
	}
	sortPlansByProtected(out)
	return out, nil
}

func (s *failoverService) Get(ctx context.Context, id int64) (*FailoverPlanView, error) {
	plan, err := s.plan(ctx, id)
	if err != nil {
		return nil, err
	}
	byNode, err := s.nodeIndex(ctx)
	if err != nil {
		return nil, err
	}
	view := s.view(plan, byNode)
	// The per-camera detail is read LIVE from the spare rather than mirrored into a column
	// here, for the same reason PTZ presets are not mirrored: the spare is the only thing
	// that knows what it is holding, and two answers to "which cameras are staged" part
	// company the first time one of them is written by anything else. Best effort — an
	// unreachable spare leaves the list empty, and the plan's own status already says so.
	if cams, err := s.stagedCameras(ctx, plan); err == nil {
		view.Cameras = cams
	}
	return &view, nil
}

// stagedCameras asks the spare what it is holding for this plan's protected appliance.
func (s *failoverService) stagedCameras(ctx context.Context, plan *entities.FailoverPlan) ([]FailoverCameraView, error) {
	body, err := s.send(ctx, plan.StandbyNodeId, http.MethodGet, "/api/standby", nil)
	if err != nil {
		return nil, err
	}
	var status struct {
		Sets []standbySetPayload `json:"sets"`
	}
	if err := json.Unmarshal(body, &status); err != nil {
		return nil, err
	}
	for _, set := range status.Sets {
		if set.SourceNodeId != plan.ProtectedNodeId {
			continue
		}
		return cameraViews(&set), nil
	}
	return []FailoverCameraView{}, nil
}

// --- writes --------------------------------------------------------------------------

func (s *failoverService) Save(ctx context.Context, req SaveFailoverPlanRequest, actor int64) (*FailoverPlanView, error) {
	protected := strings.TrimSpace(req.ProtectedNodeId)
	standby := strings.TrimSpace(req.StandbyNodeId)
	if protected == "" || standby == "" {
		return nil, errors.New("a plan needs an appliance to protect and a spare to protect it with")
	}
	if protected == standby {
		return nil, errors.New("an appliance cannot stand by for itself")
	}

	byNode, err := s.nodeIndex(ctx)
	if err != nil {
		return nil, err
	}
	pNode, ok := byNode[protected]
	if !ok {
		return nil, errors.New("the appliance to protect is not in this fleet")
	}
	sNode, ok := byNode[standby]
	if !ok {
		return nil, errors.New("the spare appliance is not in this fleet")
	}
	// Both ends must be RECORDERS. A door controller has no cameras to hand over and no
	// recorder to hand them to; a plan naming one would stage nothing forever and show as
	// permanently unready — an alarm that cannot be cleared, which is how operators learn
	// to ignore the ones that matter.
	if !isCameraNode(pNode) || !isCameraNode(sNode) {
		return nil, errors.New("failover covers camera recorders; both appliances must be recorders")
	}
	// A spare that is itself protected, or a protected appliance that is somebody's spare,
	// builds a chain: A fails to B, B fails to C, and the cameras of a site nobody has
	// looked at end up on a box three hops away. Refused at save time, where it can be
	// explained, rather than discovered during an outage.
	existing, err := s.allPlans(ctx)
	if err != nil {
		return nil, err
	}
	for _, other := range existing {
		if other.Id == req.Id {
			continue
		}
		if other.ProtectedNodeId == protected {
			return nil, errors.New("that appliance is already protected by another plan")
		}
		if other.ProtectedNodeId == standby {
			return nil, errors.New("that spare is itself protected by another plan; failover does not chain")
		}
		if other.StandbyNodeId == protected {
			return nil, errors.New("that appliance is already the spare for another plan; failover does not chain")
		}
	}

	holdDown := req.HoldDownSeconds
	if holdDown == 0 {
		holdDown = failoverDefaultHoldDown
	}
	if holdDown < failoverMinHoldDown {
		return nil, fmt.Errorf("the hold-down must be at least %d seconds: an appliance is not declared lost sooner than that, so a shorter wait would be a promise this cannot keep", failoverMinHoldDown)
	}

	now := time.Now().Unix()
	plan := entities.FailoverPlan{
		Id:              req.Id,
		Name:            strings.TrimSpace(req.Name),
		ProtectedNodeId: protected,
		StandbyNodeId:   standby,
		Enabled:         req.Enabled,
		AutoActivate:    req.AutoActivate,
		HoldDownSeconds: holdDown,
		State:           entities.FailoverStatePending,
		UpdatedBy:       actor,
		UpdatedAt:       now,
	}
	if plan.Name == "" {
		plan.Name = displayNodeName(pNode)
	}

	if req.Id > 0 {
		prev, err := s.plan(ctx, req.Id)
		if err != nil {
			return nil, err
		}
		// Everything the plan LEARNED belongs to the plan, not to the edit — unless the
		// pairing itself changed, in which case what the old spare holds says nothing
		// about the new one and carrying the drill result over would be a green tick for a
		// test that was run against a different machine.
		if prev.ProtectedNodeId == protected && prev.StandbyNodeId == standby {
			plan.State = prev.State
			plan.LastStagedAt = prev.LastStagedAt
			plan.LastStageError = prev.LastStageError
			plan.CameraCount = prev.CameraCount
			plan.LastDrillAt = prev.LastDrillAt
			plan.DrillReadiness = prev.DrillReadiness
			plan.DrillReachable = prev.DrillReachable
			plan.DrillTotal = prev.DrillTotal
			plan.ActivatedAt = prev.ActivatedAt
			plan.ActivatedAutomatically = prev.ActivatedAutomatically
			plan.ReleasedAt = prev.ReleasedAt
			plan.NotifiedLostAt = prev.NotifiedLostAt
			plan.NotifiedBackAt = prev.NotifiedBackAt
		} else if prev.State == entities.FailoverStateActive {
			return nil, errors.New("this plan is currently carrying the cameras; fail back before repointing it")
		}
		plan.CreatedBy = prev.CreatedBy
		plan.CreatedAt = prev.CreatedAt
		if _, err := s.plans.UpdateById(ctx, "", plan); err != nil {
			return nil, err
		}
	} else {
		plan.CreatedBy = actor
		plan.CreatedAt = now
		id, err := s.plans.Create(ctx, "", plan)
		if err != nil {
			return nil, err
		}
		plan.Id = int64(id)
	}

	s.record(ctx, ActionFailoverPlanSave, &plan, OutcomeSuccess,
		fmt.Sprintf("%s is covered by %s (hold-down %ds, automatic takeover %v)",
			displayNodeName(pNode), displayNodeName(sNode), holdDown, plan.AutoActivate),
		map[string]any{"autoActivate": plan.AutoActivate, "holdDownSeconds": holdDown, "enabled": plan.Enabled})

	view := s.view(&plan, byNode)
	return &view, nil
}

func (s *failoverService) Delete(ctx context.Context, id int64, actor int64) error {
	plan, err := s.plan(ctx, id)
	if err != nil {
		return err
	}
	if plan.State == entities.FailoverStateActive {
		return errors.New("this plan is currently carrying the cameras; fail back before deleting it")
	}
	// The staged copy on the spare is dropped too. Leaving it would leave one appliance
	// holding another site's camera credentials for a plan that no longer exists — and
	// nothing anywhere would say why it had them.
	if _, err := s.send(ctx, plan.StandbyNodeId, http.MethodPost,
		"/api/standby/"+plan.ProtectedNodeId+"/forget", nil); err != nil {
		s.logf("failover: the spare could not be told to drop the staged set: %v", err)
	}
	if _, err := s.plans.DeleteById(ctx, "", uint64(id)); err != nil {
		return err
	}
	s.record(ctx, ActionFailoverPlanDelete, plan, OutcomeSuccess,
		fmt.Sprintf("%s is no longer covered by %s", plan.ProtectedNodeId, plan.StandbyNodeId), nil)
	return nil
}

// --- the three steps -------------------------------------------------------------------

// Stage runs the whole exchange: the spare mints a key, the protected appliance seals its
// camera set to it, and the spare opens it. Three tunneled calls, and this service is
// carrying an envelope it cannot read for the middle one.
func (s *failoverService) Stage(ctx context.Context, id int64, actor int64) (*FailoverPlanView, error) {
	plan, err := s.plan(ctx, id)
	if err != nil {
		return nil, err
	}
	set, err := s.stage(ctx, plan)
	if err != nil {
		plan.LastStageError = err.Error()
		s.persist(ctx, plan)
		s.record(ctx, ActionFailoverStage, plan, OutcomeError, err.Error(), nil)
		return nil, err
	}
	s.record(ctx, ActionFailoverStage, plan, OutcomeSuccess,
		fmt.Sprintf("copied %d camera(s) from %s onto %s", len(set.Cameras), plan.ProtectedNodeId, plan.StandbyNodeId),
		map[string]any{"cameraCount": len(set.Cameras)})
	return s.viewWith(ctx, plan, set)
}

func (s *failoverService) stage(ctx context.Context, plan *entities.FailoverPlan) (*standbySetPayload, error) {
	keyBody, err := s.send(ctx, plan.StandbyNodeId, http.MethodGet, "/api/standby/handoff-key", nil)
	if err != nil {
		return nil, fmt.Errorf("the spare could not be asked for a handoff key: %w", err)
	}
	var key struct {
		NodeId    string `json:"nodeId"`
		PublicKey string `json:"publicKey"`
	}
	if err := json.Unmarshal(keyBody, &key); err != nil {
		return nil, fmt.Errorf("the spare's handoff key was unreadable: %w", err)
	}
	// The appliance that answered must be the one we addressed. A tunnel that delivered to
	// the wrong node would otherwise mean sealing a site's camera credentials to a machine
	// nobody chose — silently, since every later step would succeed.
	if strings.TrimSpace(key.NodeId) != plan.StandbyNodeId {
		return nil, fmt.Errorf("the appliance that answered is %q, not the spare this plan names", key.NodeId)
	}
	if strings.TrimSpace(key.PublicKey) == "" {
		return nil, errors.New("the spare returned no handoff key")
	}

	handoffReq, _ := json.Marshal(map[string]string{
		"recipientNodeId": plan.StandbyNodeId,
		"publicKey":       key.PublicKey,
	})
	sealedBody, err := s.send(ctx, plan.ProtectedNodeId, http.MethodPost, "/api/standby/handoff", handoffReq)
	if err != nil {
		return nil, fmt.Errorf("the protected appliance could not hand over its camera set: %w", err)
	}
	var handoff struct {
		CameraCount int    `json:"cameraCount"`
		Sealed      string `json:"sealed"`
	}
	if err := json.Unmarshal(sealedBody, &handoff); err != nil {
		return nil, fmt.Errorf("the camera set was unreadable: %w", err)
	}
	if strings.TrimSpace(handoff.Sealed) == "" {
		return nil, errors.New("the protected appliance returned an empty camera set")
	}

	stageReq, _ := json.Marshal(map[string]string{"sealed": handoff.Sealed})
	stagedBody, err := s.send(ctx, plan.StandbyNodeId, http.MethodPost, "/api/standby/stage", stageReq)
	if err != nil {
		return nil, fmt.Errorf("the spare could not take the camera set: %w", err)
	}
	set, err := decodeStandbySet(stagedBody)
	if err != nil {
		return nil, err
	}

	now := time.Now().Unix()
	plan.LastStagedAt = now
	plan.LastStageError = ""
	plan.CameraCount = len(set.Cameras)
	// Staging never advances a plan out of ACTIVE — the spare is carrying the cameras and
	// re-copying the list does not change that — and never claims readiness, which only a
	// drill can establish.
	if plan.State != entities.FailoverStateActive {
		plan.State = entities.FailoverStateStaged
	}
	s.persist(ctx, plan)
	return set, nil
}

func (s *failoverService) Drill(ctx context.Context, id int64, actor int64) (*FailoverPlanView, error) {
	plan, err := s.plan(ctx, id)
	if err != nil {
		return nil, err
	}
	set, err := s.drill(ctx, plan)
	if err != nil {
		s.record(ctx, ActionFailoverDrill, plan, OutcomeError, err.Error(), nil)
		return nil, err
	}
	outcome := OutcomeSuccess
	if set.Readiness != standbyReadyReady && set.Readiness != standbyReadyActive {
		// A drill that RAN and found the spare cannot open the cameras is not an error —
		// it is the drill working. It is recorded as a failure outcome anyway, because
		// what an investigator is looking for later is "when did we last know this would
		// work", and a success row next to 3-of-40 answers that wrongly.
		outcome = OutcomeError
	}
	s.record(ctx, ActionFailoverDrill, plan, outcome,
		fmt.Sprintf("%s opened %d of %d camera(s) belonging to %s",
			plan.StandbyNodeId, set.Reachable, set.Total, plan.ProtectedNodeId),
		map[string]any{"reachable": set.Reachable, "total": set.Total, "readiness": set.Readiness})
	if outcome == OutcomeError {
		s.notifyPlan(ctx, FailoverNotifyDrillFailed, plan,
			fmt.Sprintf("%d of %d camera(s) could be opened", set.Reachable, set.Total))
	}
	return s.viewWith(ctx, plan, set)
}

func (s *failoverService) drill(ctx context.Context, plan *entities.FailoverPlan) (*standbySetPayload, error) {
	body, err := s.send(ctx, plan.StandbyNodeId, http.MethodPost,
		"/api/standby/"+plan.ProtectedNodeId+"/drill", nil)
	if err != nil {
		return nil, fmt.Errorf("the spare could not be asked to test the cameras: %w", err)
	}
	set, err := decodeStandbySet(body)
	if err != nil {
		return nil, err
	}
	plan.LastDrillAt = time.Now().Unix()
	plan.DrillReadiness = set.Readiness
	plan.DrillReachable = set.Reachable
	plan.DrillTotal = set.Total
	s.persist(ctx, plan)
	return set, nil
}

func (s *failoverService) Activate(ctx context.Context, id int64, actor int64, automatic bool) (*FailoverPlanView, error) {
	plan, err := s.plan(ctx, id)
	if err != nil {
		return nil, err
	}
	if plan.State == entities.FailoverStateActive {
		return nil, errors.New("the spare is already carrying these cameras")
	}
	if plan.LastStagedAt == 0 {
		// The honest refusal. There is nothing on the spare to activate, and the failure
		// an operator must never meet is pressing this in an emergency and being told
		// afterwards that nothing was ever copied.
		return nil, errors.New("nothing has been copied onto the spare yet, so there is nothing to take over")
	}
	body, err := s.send(ctx, plan.StandbyNodeId, http.MethodPost,
		"/api/standby/"+plan.ProtectedNodeId+"/activate", nil)
	if err != nil {
		s.record(ctx, ActionFailoverActivate, plan, OutcomeError, err.Error(),
			map[string]any{"automatic": automatic})
		s.notifyPlan(ctx, FailoverNotifyActivateFailed, plan, err.Error())
		return nil, fmt.Errorf("the spare could not take the cameras over: %w", err)
	}
	set, err := decodeStandbySet(body)
	if err != nil {
		return nil, err
	}
	now := time.Now().Unix()
	plan.State = entities.FailoverStateActive
	plan.ActivatedAt = now
	plan.ActivatedAutomatically = automatic
	plan.ReleasedAt = 0
	plan.NotifiedBackAt = 0
	s.persist(ctx, plan)

	recording, total := countRecording(set)
	how := "an operator"
	if automatic {
		how = "the hold-down expiring with no contact"
	}
	s.record(ctx, ActionFailoverActivate, plan, OutcomeSuccess,
		fmt.Sprintf("%s took over %s's cameras (%d of %d recording), triggered by %s",
			plan.StandbyNodeId, plan.ProtectedNodeId, recording, total, how),
		map[string]any{"automatic": automatic, "recording": recording, "total": total,
			"outcomes": cameraOutcomes(set)})
	s.notifyPlan(ctx, FailoverNotifyActivated, plan,
		fmt.Sprintf("%d of %d camera(s) are recording on the spare", recording, total))
	return s.viewWith(ctx, plan, set)
}

func (s *failoverService) Release(ctx context.Context, id int64, actor int64) (*FailoverPlanView, error) {
	plan, err := s.plan(ctx, id)
	if err != nil {
		return nil, err
	}
	if plan.State != entities.FailoverStateActive {
		return nil, errors.New("the spare is not carrying these cameras")
	}
	body, err := s.send(ctx, plan.StandbyNodeId, http.MethodPost,
		"/api/standby/"+plan.ProtectedNodeId+"/release", nil)
	if err != nil {
		s.record(ctx, ActionFailoverRelease, plan, OutcomeError, err.Error(), nil)
		return nil, fmt.Errorf("the spare could not hand the cameras back: %w", err)
	}
	set, err := decodeStandbySet(body)
	if err != nil {
		return nil, err
	}
	plan.State = entities.FailoverStateReleased
	plan.ReleasedAt = time.Now().Unix()
	plan.NotifiedLostAt = 0
	s.persist(ctx, plan)

	s.record(ctx, ActionFailoverRelease, plan, OutcomeSuccess,
		fmt.Sprintf("%s handed %s's cameras back; the footage it recorded during the outage stays on it",
			plan.StandbyNodeId, plan.ProtectedNodeId), nil)
	s.notifyPlan(ctx, FailoverNotifyReleased, plan, "")
	return s.viewWith(ctx, plan, set)
}

// --- the sweep ---------------------------------------------------------------------

// Sweep is the unattended half: keep every healthy plan copied and drilled, and act on the
// ones whose recorder has gone quiet.
//
// It is leader-gated by the caller, for the same reason the heartbeat is. Two instances
// sweeping would stage the same plan twice — and, far worse, could both decide to activate
// the same plan, which is two takeover commands racing at one spare.
func (s *failoverService) Sweep(ctx context.Context) {
	s.sweeping.Lock()
	defer s.sweeping.Unlock()

	plans, err := s.allPlans(ctx)
	if err != nil {
		s.logf("failover: listing plans: %v", err)
		return
	}
	byNode, err := s.nodeIndex(ctx)
	if err != nil {
		s.logf("failover: listing nodes: %v", err)
		return
	}
	now := time.Now().Unix()
	for _, plan := range plans {
		if !plan.Enabled {
			continue
		}
		protectedNode := byNode[plan.ProtectedNodeId]
		standbyNode := byNode[plan.StandbyNodeId]
		if protectedNode == nil || standbyNode == nil {
			// A plan naming an appliance that has been released from the fleet. Left alone
			// rather than deleted: the plan is somebody's intent, and quietly removing it
			// would take with it the only record that this recorder was meant to be covered.
			continue
		}
		s.sweepPlan(ctx, plan, protectedNode, standbyNode, now)
	}
}

func (s *failoverService) sweepPlan(ctx context.Context, plan *entities.FailoverPlan, protectedNode, standbyNode *entities.ManagedNode, now int64) {
	if plan.State == entities.FailoverStateActive {
		// The spare is carrying the cameras. The ONE thing worth watching now is the
		// protected appliance coming back — at which point both may be recording, and
		// somebody has to be told so they can fail back. Deliberately NOT automatic: an
		// appliance that returns for thirty seconds and dies again would thrash every
		// camera in the building between two recorders.
		if protectedNode.Status == "online" && plan.NotifiedBackAt == 0 {
			plan.NotifiedBackAt = now
			s.persist(ctx, plan)
			s.notifyPlan(ctx, FailoverNotifyProtectedBack, plan,
				"both appliances may now be recording these cameras")
		}
		return
	}

	if protectedNode.Status == "lost" {
		s.considerTakeover(ctx, plan, protectedNode, now)
		return
	}
	if protectedNode.Status != "online" || standbyNode.Status != "online" {
		// self-dropped, unknown, or a spare that is itself away. Nothing to copy and
		// nothing to prove; the screen already reports both appliances' status.
		return
	}
	// Healthy. Clear the "we were about to fail over" edge so a later outage notifies again.
	if plan.NotifiedLostAt != 0 {
		plan.NotifiedLostAt = 0
		s.persist(ctx, plan)
	}
	if now-plan.LastStagedAt >= int64(failoverStageInterval.Seconds()) {
		if _, err := s.stage(ctx, plan); err != nil {
			plan.LastStageError = err.Error()
			s.persist(ctx, plan)
			s.logf("failover: staging %s onto %s: %v", plan.ProtectedNodeId, plan.StandbyNodeId, err)
			return
		}
	}
	if plan.LastStagedAt > 0 && s.drillIsDue(plan, now) {
		if _, err := s.drill(ctx, plan); err != nil {
			s.logf("failover: drilling %s: %v", plan.StandbyNodeId, err)
		}
	}
}

// drillIsDue separates "this has never been proved" from "the proof has aged out". They are
// different questions with different right answers, and one subtraction cannot tell them
// apart: a plan drilled at the zero epoch is not overdue, it is untried.
func (s *failoverService) drillIsDue(plan *entities.FailoverPlan, now int64) bool {
	if plan.LastDrillAt == 0 {
		return now-plan.LastStagedAt >= int64(failoverFirstDrillDelay.Seconds())
	}
	return now-plan.LastDrillAt >= int64(failoverDrillInterval.Seconds())
}

// considerTakeover decides whether a lost recorder has been quiet long enough to act on.
//
// The hold-down is measured from the last time the fleet HEARD from the appliance, not from
// when this sweep noticed — a sweep that has just started up has noticed everything at once
// and would otherwise treat a fleet-wide restart of the control plane as every site failing
// simultaneously.
func (s *failoverService) considerTakeover(ctx context.Context, plan *entities.FailoverPlan, protectedNode *entities.ManagedNode, now int64) {
	holdDown := int64(plan.HoldDownSeconds)
	if holdDown <= 0 {
		holdDown = failoverDefaultHoldDown
	}
	silent := now - protectedNode.LastSeenAt
	if protectedNode.LastSeenAt == 0 || silent < holdDown {
		return
	}
	if plan.LastStagedAt == 0 {
		// Nothing was ever copied, so there is nothing to take over. This is the one case
		// that MUST reach a human: the plan exists, the recorder is down, and the promise
		// cannot be kept. Silence here would be the feature failing exactly when it was
		// supposed to work.
		if plan.NotifiedLostAt == 0 {
			plan.NotifiedLostAt = now
			s.persist(ctx, plan)
			s.notifyPlan(ctx, FailoverNotifyActivateFailed, plan,
				"nothing was ever copied onto the spare, so its cameras cannot be taken over")
		}
		return
	}
	if !plan.AutoActivate {
		if plan.NotifiedLostAt == 0 {
			plan.NotifiedLostAt = now
			s.persist(ctx, plan)
			s.notifyPlan(ctx, FailoverNotifyReadyToActivate, plan,
				fmt.Sprintf("out of contact for %s", humanizeSeconds(silent)))
		}
		return
	}
	if _, err := s.Activate(ctx, plan.Id, 0, true); err != nil {
		s.logf("failover: automatic takeover of %s failed: %v", plan.ProtectedNodeId, err)
	}
}

// --- plumbing ---------------------------------------------------------------------

// standbySetPayload is the spare's answer, decoded. It mirrors mymatasan's StandbySet
// rather than importing it: the control plane must not depend on an appliance app's
// packages, and the two are joined by a wire format, not by a type.
type standbySetPayload struct {
	SourceNodeId string `json:"sourceNodeId"`
	State        string `json:"state"`
	Readiness    string `json:"readiness"`
	Reachable    int    `json:"reachable"`
	Total        int    `json:"total"`
	StagedAt     int64  `json:"stagedAt"`
	DrilledAt    int64  `json:"drilledAt"`
	Cameras      []struct {
		Name          string `json:"name"`
		Host          string `json:"host"`
		State         string `json:"state"`
		CheckStatus   string `json:"checkStatus"`
		CheckDetail   string `json:"checkDetail"`
		CheckedAt     int64  `json:"checkedAt"`
		Outcome       string `json:"outcome"`
		OutcomeDetail string `json:"outcomeDetail"`
	} `json:"cameras"`
}

// The readiness vocabulary the appliance speaks. Duplicated as string constants rather than
// imported for the same reason as the payload above.
const (
	standbyReadyUntested = "untested"
	standbyReadyReady    = "ready"
	standbyReadyPartial  = "partial"
	standbyReadyBlind    = "blind"
	standbyReadyActive   = "active"
)

func decodeStandbySet(body []byte) (*standbySetPayload, error) {
	var set standbySetPayload
	if err := json.Unmarshal(body, &set); err != nil {
		return nil, fmt.Errorf("the spare's answer was unreadable: %w", err)
	}
	return &set, nil
}

func countRecording(set *standbySetPayload) (int, int) {
	recording := 0
	for _, cam := range set.Cameras {
		if cam.Outcome == "recording" {
			recording++
		}
	}
	return recording, len(set.Cameras)
}

func cameraOutcomes(set *standbySetPayload) map[string]string {
	out := map[string]string{}
	for _, cam := range set.Cameras {
		if cam.Outcome == "" {
			continue
		}
		// The trail keeps the reason as well as the verdict: "took over 40 cameras" with six
		// of them not recording is the entry that has to be answerable later, and the code
		// alone does not say why.
		if cam.OutcomeDetail != "" {
			out[cam.Name] = cam.Outcome + ": " + cam.OutcomeDetail
			continue
		}
		out[cam.Name] = cam.Outcome
	}
	return out
}

// send makes one tunneled call to a node and unwraps the standard {message,result}
// envelope, so every caller here works with the payload rather than the envelope.
func (s *failoverService) send(ctx context.Context, nodeID, method, path string, body []byte) (json.RawMessage, error) {
	if s.sender == nil {
		return nil, errors.New("the control channel is not available")
	}
	ctx, cancel := context.WithTimeout(ctx, failoverRequestTimeout)
	defer cancel()

	req := control.Request{
		Method: method,
		Path:   path,
		// The same assertion the fleet policy reconciler makes: a ROLE NAME, which the node
		// resolves against its OWN roles and matrix. The control plane does not get a
		// private capability on the appliance — an appliance that does not grant admin to
		// this name refuses, and the refusal surfaces here as an error rather than as a
		// silent success.
		Role:  "admin",
		Actor: "failover",
	}
	if body != nil {
		req.Body = body
		req.Headers = map[string]string{"Content-Type": "application/json"}
	}
	resp, err := s.sender.SendRequest(ctx, nodeID, req)
	if err != nil {
		if errors.Is(err, ErrNodeOffline) || errors.Is(err, ErrNodeDisconnected) {
			return nil, errors.New("the appliance is not connected")
		}
		return nil, err
	}
	if resp.Status == http.StatusNotFound {
		return nil, errors.New("this appliance does not support failover; it is running an older build")
	}
	if resp.Status == http.StatusTooManyRequests {
		return nil, errors.New("the appliance is rate-limiting the control plane; this will be retried")
	}
	if resp.Status < 200 || resp.Status >= 300 {
		return nil, fmt.Errorf("the appliance refused (%d): %s", resp.Status, standbyErrorText(resp.Body))
	}
	var env struct {
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(resp.Body, &env); err != nil {
		return nil, fmt.Errorf("unreadable response from the appliance: %w", err)
	}
	if len(env.Result) == 0 {
		return nil, errors.New("the appliance returned nothing")
	}
	return env.Result, nil
}

// standbyErrorText pulls the message out of a node's error envelope. Without it a refusal
// reads as "the appliance refused (400)" and the reason the appliance gave — which is
// usually the whole answer — is discarded at the one moment somebody needs it.
func standbyErrorText(body []byte) string {
	var env struct {
		Message string `json:"message"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal(body, &env); err == nil {
		if strings.TrimSpace(env.Message) != "" {
			return env.Message
		}
		if strings.TrimSpace(env.Error) != "" {
			return env.Error
		}
	}
	return "no reason given"
}

func (s *failoverService) allPlans(ctx context.Context) ([]*entities.FailoverPlan, error) {
	plans, _, err := s.plans.Get(ctx, "", failoverMaxPlans, 0, nil,
		[]sqldataenums.Sorter{{FieldName: "Id", Sort: sqldataenums.ASC}})
	if err != nil && !isNoResultErr(err) {
		return nil, err
	}
	out := make([]*entities.FailoverPlan, 0, len(plans))
	for _, plan := range plans {
		if plan != nil {
			out = append(out, plan)
		}
	}
	return out, nil
}

func (s *failoverService) plan(ctx context.Context, id int64) (*entities.FailoverPlan, error) {
	if id <= 0 {
		return nil, ErrFailoverPlanNotFound
	}
	// GetById ERRORS on a missing row rather than returning nil, so the not-found case has
	// to be recognised here or it surfaces as an internal error.
	plan, err := s.plans.GetById(ctx, "", uint64(id))
	if err != nil {
		if isNoResultErr(err) {
			return nil, ErrFailoverPlanNotFound
		}
		return nil, err
	}
	if plan == nil {
		return nil, ErrFailoverPlanNotFound
	}
	return plan, nil
}

func (s *failoverService) persist(ctx context.Context, plan *entities.FailoverPlan) {
	plan.UpdatedAt = time.Now().Unix()
	if _, err := s.plans.UpdateById(ctx, "", *plan); err != nil {
		s.logf("failover: saving plan %d: %v", plan.Id, err)
	}
}

func (s *failoverService) nodeIndex(ctx context.Context) (map[string]*entities.ManagedNode, error) {
	nodes, err := s.nodes.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string]*entities.ManagedNode, len(nodes))
	for _, node := range nodes {
		if node != nil {
			out[node.NodeId] = node
		}
	}
	return out, nil
}

func (s *failoverService) viewOf(ctx context.Context, plan *entities.FailoverPlan) (*FailoverPlanView, error) {
	return s.viewWith(ctx, plan, nil)
}

// viewWith renders a plan and attaches the per-camera detail the appliance just reported.
//
// Passing the set through is not a convenience. A takeover's per-camera OUTCOME — this one
// is recording, that one could not be created, this one the source was not recording anyway
// — exists for exactly one moment: the appliance computes it while taking over and does not
// store it, because it is a result, not a state. Rebuilding the view from the database
// afterwards therefore drops it, and the operator who has just pressed the button in an
// emergency is handed a plan that says "active" and nothing about which of their forty
// cameras is actually being recorded. That is the one fact they need.
//
// Found by the live bench: the appliance reported it, the audit trail recorded it, and the
// API returned an empty list. The same shape as the redact flag W3-6 dropped between the
// screen and the service — a field that existed at both ends and never crossed the middle.
func (s *failoverService) viewWith(ctx context.Context, plan *entities.FailoverPlan, set *standbySetPayload) (*FailoverPlanView, error) {
	byNode, err := s.nodeIndex(ctx)
	if err != nil {
		return nil, err
	}
	view := s.view(plan, byNode)
	if set != nil {
		view.Cameras = cameraViews(set)
	}
	return &view, nil
}

// cameraViews converts the appliance's per-camera report into the screen's shape.
func cameraViews(set *standbySetPayload) []FailoverCameraView {
	out := make([]FailoverCameraView, 0, len(set.Cameras))
	for _, cam := range set.Cameras {
		out = append(out, FailoverCameraView{
			Name: cam.Name, Host: cam.Host, State: cam.State,
			CheckStatus: cam.CheckStatus, CheckDetail: cam.CheckDetail, CheckedAt: cam.CheckedAt,
			Outcome: cam.Outcome, OutcomeDetail: cam.OutcomeDetail,
		})
	}
	return out
}

// view renders a plan for the screen, and decides the ONE thing the screen leads with:
// would this work right now?
//
// READY IS NEVER INFERRED FROM A SUCCESSFUL COPY. A staged set proves the two appliances
// can talk to each other; it says nothing about whether the spare can reach the CAMERAS,
// which is a different network path, different credentials and the thing that actually
// fails. Only a drill answers that, so a plan that has never been drilled reports
// "untested" no matter how recently it was staged.
func (s *failoverService) view(plan *entities.FailoverPlan, byNode map[string]*entities.ManagedNode) FailoverPlanView {
	view := FailoverPlanView{Plan: plan, Cameras: []FailoverCameraView{}}
	if p := byNode[plan.ProtectedNodeId]; p != nil {
		view.ProtectedName = displayNodeName(p)
		view.ProtectedStatus = p.Status
		view.ProtectedSeenAt = p.LastSeenAt
	}
	if sn := byNode[plan.StandbyNodeId]; sn != nil {
		view.StandbyName = displayNodeName(sn)
		view.StandbyStatus = sn.Status
	}

	switch {
	case !plan.Enabled:
		view.ReadyState = FailoverReadyDisabled
	case plan.State == entities.FailoverStateActive:
		view.ReadyState = FailoverReadyActive
	case view.StandbyStatus != "" && view.StandbyStatus != "online":
		// A spare that is itself off the network cannot take anything over, whatever its
		// last drill said. Reported before the drill result, because it is the more urgent
		// fact and the one that makes every other reading stale.
		view.ReadyState = FailoverReadyStandbyDown
	case plan.LastStagedAt == 0:
		view.ReadyState = FailoverReadyNotStaged
	case plan.LastDrillAt == 0 || plan.DrillReadiness == "" || plan.DrillReadiness == standbyReadyUntested:
		view.ReadyState = FailoverReadyUntested
	case plan.DrillReadiness == standbyReadyReady:
		view.ReadyState = FailoverReadyReady
		view.Ready = true
	case plan.DrillReadiness == standbyReadyBlind:
		view.ReadyState = FailoverReadyBlind
	default:
		view.ReadyState = FailoverReadyPartial
	}
	return view
}

func (s *failoverService) record(ctx context.Context, action string, plan *entities.FailoverPlan, outcome, detail string, meta map[string]any) {
	if s.audit == nil {
		return
	}
	if meta == nil {
		meta = map[string]any{}
	}
	meta["protectedNodeId"] = plan.ProtectedNodeId
	meta["standbyNodeId"] = plan.StandbyNodeId
	s.audit.Record(ctx, AuditEntry{
		Action:     action,
		TargetType: "failover",
		TargetId:   plan.ProtectedNodeId,
		Outcome:    outcome,
		Detail:     detail,
		Metadata:   meta,
	})
}

func (s *failoverService) notifyPlan(ctx context.Context, kind string, plan *entities.FailoverPlan, detail string) {
	if s.notify == nil {
		return
	}
	protectedName, standbyName := plan.ProtectedNodeId, plan.StandbyNodeId
	if byNode, err := s.nodeIndex(ctx); err == nil {
		if p := byNode[plan.ProtectedNodeId]; p != nil {
			protectedName = displayNodeName(p)
		}
		if sn := byNode[plan.StandbyNodeId]; sn != nil {
			standbyName = displayNodeName(sn)
		}
	}
	s.notify(kind, plan, protectedName, standbyName, detail)
}

// isCameraNode treats an empty kind as a camera recorder, exactly as the rest of the fleet
// does: every node adopted before kinds existed is a mymatasan node.
func isCameraNode(node *entities.ManagedNode) bool {
	kind := strings.TrimSpace(node.Kind)
	return kind == "" || kind == "camera"
}

// humanizeSeconds renders a duration for an audit line and a notification body. Deliberately
// coarse — "about 6 minutes" is what somebody reading an alert needs, and a precise 371 is
// a number they then have to convert.
func humanizeSeconds(sec int64) string {
	switch {
	case sec < 120:
		return fmt.Sprintf("%d seconds", sec)
	case sec < 7200:
		return fmt.Sprintf("about %d minutes", sec/60)
	case sec < 172800:
		return fmt.Sprintf("about %d hours", sec/3600)
	default:
		return fmt.Sprintf("about %d days", sec/86400)
	}
}

// sortPlansByProtected keeps a listing stable across sweeps so the screen does not reorder
// itself under the operator's cursor.
func sortPlansByProtected(views []FailoverPlanView) {
	sort.SliceStable(views, func(i, j int) bool {
		return views[i].Plan.ProtectedNodeId < views[j].Plan.ProtectedNodeId
	})
}
