package services

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mysayasan/kopiv2/apps/mymatasan/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	"github.com/mysayasan/kopiv2/infra/atrest"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
	"github.com/mysayasan/kopiv2/infra/handoff"
	"github.com/mysayasan/kopiv2/infra/onvif"
	"github.com/mysayasan/kopiv2/infra/recording"
)

// N+1 failover, the appliance half (W3-7).
//
// A mymatasan node is the only thing recording its cameras. When it dies — power supply,
// disk, a switch port, somebody carrying it out of the building — those cameras stop being
// recorded, and nothing anywhere starts recording them. Retention, continuity monitoring
// and tamper detection all assume the box is running. This is the feature that assumes it
// is not.
//
// The whole design follows from ONE fact: at the moment failover matters, the appliance
// that held the cameras is unreachable. Nothing can be fetched from it. So the entire
// exchange happens EARLY, while it is healthy — the spare is given a copy of the camera set
// and can be made to prove, before anything has gone wrong, that it can actually open those
// cameras. That proof is the product. A spare nobody has tested is a line item.
//
// THREE THINGS THIS DELIBERATELY DOES NOT DO, each of which reads as an omission until you
// follow it through:
//
//  1. It does not bring back the FOOTAGE that was on the lost appliance. That footage is on
//     that appliance. Failover restores RECORDING, from the moment it happens; the past is
//     what the critical-clip archive (W2-3) is for, and only for the clips flagged into it.
//     Saying so plainly is worth more than a feature name that implies otherwise.
//
//  2. It does not stop the lost appliance. It cannot — it is unreachable, which is the
//     premise — and even when it comes back it is not told to stand down. A control plane
//     that cannot reach a node cannot distinguish "dead" from "on the other side of a
//     partition and still recording perfectly", and the failure it must never cause is ZERO
//     recorders. So failover here is ADDITIVE: worst case both record the same camera for a
//     while, costing a duplicate stream on the camera and duplicate footage, and the
//     operator is told and can fail back. That is a far better worst case than the fencing
//     protocol that gets it wrong once.
//
//  3. It does not delete the taken-over cameras on fail-back. The footage recorded here
//     during the outage hangs off those camera rows, and removing a camera on this
//     appliance purges its footage. Fail-back stops recording; it does not destroy the
//     record of the outage it just covered.

const (
	// standbyKeyTTL bounds how long a minted recipient key stays usable. The whole
	// exchange — mint, seal, stage — is three tunneled calls, so this is generous; what it
	// buys is that a bundle sealed today cannot be opened by anything tomorrow, because
	// the key it was sealed to no longer exists anywhere.
	standbyKeyTTL = 15 * time.Minute
	// standbyBundleVersion is the wire version of a sealed camera set.
	standbyBundleVersion = 1
	// standbyMaxCameras refuses an implausible bundle outright rather than materializing
	// thousands of camera rows from one request.
	standbyMaxCameras = 512
	// standbyDrillConcurrency bounds how many staged cameras are probed at once. A drill
	// on a large set is otherwise a burst of simultaneous RTSP sessions from one appliance.
	standbyDrillConcurrency = 4
	// standbyListPageSize bounds a staged-set read.
	standbyListPageSize = 2000
	// standbyActivateSettle is how long a takeover waits for the recorders it just started
	// to actually put something on disk before it reports what happened.
	//
	// It exists because "the ffmpeg process is alive" is a WEAKER claim than it looks: a
	// recorder pointed at a camera this appliance cannot resolve still has a live process,
	// which retries, so a takeover asked immediately reports every camera as recording —
	// including the ones the drill on the same screen says it could not reach. Found by the
	// screen pass, on a card that said both things at once.
	standbyActivateSettle = 12 * time.Second
	// standbyActivatePoll is how often the settle loop re-reads the recorder.
	standbyActivatePoll = 1500 * time.Millisecond
)

// ErrStandbyNoSuchSet is returned when nothing has been staged for the named appliance.
var ErrStandbyNoSuchSet = errors.New("nothing is staged for that appliance")

// ErrStandbyKeyUnknown is returned when a bundle arrives sealed to a recipient key this
// appliance does not hold — it expired, or the appliance restarted between minting it and
// being handed the bundle. The caller's fix is to start the exchange again, so it says so
// rather than surfacing a decryption failure.
var ErrStandbyKeyUnknown = errors.New("this appliance has no matching handoff key; request a new one and re-stage")

// StandbyHandoffKey is the one-exchange public key a spare publishes so another appliance
// can seal its camera set to it.
type StandbyHandoffKey struct {
	NodeId string `json:"nodeId"`
	// PublicKey is base64 of the X25519 public key. It is not a secret: it lets somebody
	// seal a bundle TO this appliance and never lets them open one.
	PublicKey string `json:"publicKey"`
	ExpiresAt int64  `json:"expiresAt"`
}

// StandbyHandoffRequest asks this appliance to seal ITS camera set for a named spare.
type StandbyHandoffRequest struct {
	RecipientNodeId string `json:"recipientNodeId"`
	PublicKey       string `json:"publicKey"`
}

// StandbyHandoffResult is the sealed camera set, plus the little that can be said about it
// without opening it. The counts are here so the control plane can report what it moved
// while remaining unable to read any of it.
type StandbyHandoffResult struct {
	SourceNodeId   string `json:"sourceNodeId"`
	SourceNodeName string `json:"sourceNodeName"`
	CameraCount    int    `json:"cameraCount"`
	CreatedAt      int64  `json:"createdAt"`
	// Sealed is base64 of the handoff envelope. The control plane relays this and cannot
	// open it: it is sealed to the spare's key and bound to the spare's node id.
	Sealed string `json:"sealed"`
}

// StandbyStageRequest hands a sealed camera set to the spare it was sealed for.
type StandbyStageRequest struct {
	Sealed string `json:"sealed"`
}

// What a takeover or a fail-back did to one camera, as a STATE rather than a sentence.
//
// A sentence composed here arrives in English on an Arabic screen. That is not a
// hypothetical: W3-4 shipped it with schedule summaries and W3-6 shipped it with the privacy
// status line, and both were found by the same screen pass that found this one. The rule the
// programme settled on is that the appliance says WHAT HAPPENED and the screen says it in
// the operator's language.
//
// The DETAIL that travels beside the code — an ffmpeg error, a refusal from the camera
// service — is deliberately still raw. It is a machine's own words about a failure, it
// cannot be enumerated in advance, and a translated paraphrase of it would be less useful to
// the engineer who has to act on it, not more.
const (
	// StandbyOutcomeRecording means the recorder is running AND has written something. It
	// is the only outcome that claims footage exists.
	StandbyOutcomeRecording = "recording"
	// StandbyOutcomePending means the recorder is up and nothing has reached the disk yet.
	StandbyOutcomePending = "pending"
	// StandbyOutcomeNotRecording means the recorder is not running for this camera.
	StandbyOutcomeNotRecording = "not-recording"
	// StandbyOutcomeNotWanted means the source appliance was not recording this camera, so
	// neither is this one — taking over continues what was happening.
	StandbyOutcomeNotWanted = "not-wanted"
	// StandbyOutcomeCreateFailed means the camera could not be created here at all.
	StandbyOutcomeCreateFailed = "create-failed"
	// StandbyOutcomeConfigFailed means the camera exists but could not be set to record.
	StandbyOutcomeConfigFailed = "config-failed"
	// StandbyOutcomeRefused means the recorder rejected the configuration.
	StandbyOutcomeRefused = "refused"
	// StandbyOutcomeNoRecorder means this build has no recorder at all.
	StandbyOutcomeNoRecorder = "no-recorder"
	// StandbyOutcomeAlready means the camera was already being recorded here.
	StandbyOutcomeAlready = "already-recording"
	// StandbyOutcomeStopped means a fail-back stopped this camera; its footage stays.
	StandbyOutcomeStopped = "stopped"
	// StandbyOutcomeStopFailed means a fail-back could not stop it.
	StandbyOutcomeStopFailed = "stop-failed"
)

// standbyOutcome is a verdict about one camera: what happened, and the machine's own words
// about it where there are any.
type standbyOutcome struct {
	code   string
	detail string
}

// StandbyCameraView is one staged camera as a screen or a control plane sees it. It is
// NOT the entity: the entity holds a camera password.
type StandbyCameraView struct {
	SourceCameraId  int64  `json:"sourceCameraId"`
	Name            string `json:"name"`
	Host            string `json:"host"`
	State           string `json:"state"`
	LocalCameraId   int64  `json:"localCameraId"`
	RecordingWanted bool   `json:"recordingWanted"`
	CheckStatus     string `json:"checkStatus"`
	CheckDetail     string `json:"checkDetail"`
	CheckedAt       int64  `json:"checkedAt"`
	// Outcome is set only on the result of an activate or a release: what actually
	// happened to this camera just now, read back rather than assumed. It is one of the
	// StandbyOutcome* codes; OutcomeDetail carries the machine's own words where there are
	// any. The SENTENCE is composed by whatever renders it, in the reader's language.
	Outcome       string `json:"outcome,omitempty"`
	OutcomeDetail string `json:"outcomeDetail,omitempty"`
}

// Readiness verdicts for a staged set. UNTESTED is kept distinct from READY for the same
// reason the fleet policy reconciler keeps "unknown" distinct from "compliant": a spare
// that has never been asked to open the cameras is not evidence of anything, and colouring
// it green is how a building ends up with a failover plan that has never worked.
const (
	StandbyReadinessUntested = "untested"
	StandbyReadinessReady    = "ready"
	StandbyReadinessPartial  = "partial"
	StandbyReadinessBlind    = "blind"
	// StandbyReadinessActive means the question is moot: this appliance is already
	// recording the set.
	StandbyReadinessActive = "active"
)

// StandbySet is everything this appliance holds on behalf of one other appliance.
type StandbySet struct {
	SourceNodeId   string              `json:"sourceNodeId"`
	SourceNodeName string              `json:"sourceNodeName"`
	State          string              `json:"state"`
	StagedAt       int64               `json:"stagedAt"`
	ActivatedAt    int64               `json:"activatedAt"`
	ReleasedAt     int64               `json:"releasedAt"`
	Cameras        []StandbyCameraView `json:"cameras"`
	// Readiness is one of the StandbyReadiness* values, and Reachable/Total are the drill
	// counts behind it.
	Readiness string `json:"readiness"`
	Reachable int    `json:"reachable"`
	Total     int    `json:"total"`
	DrilledAt int64  `json:"drilledAt"`
}

// StandbyStatus is this appliance's whole standby position: who it is, and what it holds.
type StandbyStatus struct {
	NodeId string       `json:"nodeId"`
	Name   string       `json:"name"`
	Sets   []StandbySet `json:"sets"`
}

// IStandbyService is the appliance's failover surface. Everything the control plane does
// to arrange a failover goes through it, so there is one place that audits, one place that
// refuses, and no path that reaches the camera table sideways.
type IStandbyService interface {
	// HandoffKey mints (or returns) this appliance's current one-exchange recipient key.
	HandoffKey(ctx context.Context) (StandbyHandoffKey, error)
	// Handoff seals THIS appliance's camera set for the named recipient. Run on the
	// appliance being protected, while it is healthy.
	Handoff(ctx context.Context, req StandbyHandoffRequest) (StandbyHandoffResult, error)
	// Stage opens a sealed set and stores it. Run on the spare.
	Stage(ctx context.Context, req StandbyStageRequest) (*StandbySet, error)
	// Status reports what this appliance is holding for everyone.
	Status(ctx context.Context) (StandbyStatus, error)
	// Drill asks the cameras in one staged set whether THIS appliance can open them.
	Drill(ctx context.Context, sourceNodeId string) (*StandbySet, error)
	// Activate materializes the set and starts recording it.
	Activate(ctx context.Context, sourceNodeId string) (*StandbySet, error)
	// Release stops recording a set it took over. The cameras and their footage stay.
	Release(ctx context.Context, sourceNodeId string) (*StandbySet, error)
	// Forget drops a staged set this appliance no longer covers. It never touches a
	// camera row or a byte of footage — see the comment on the method.
	Forget(ctx context.Context, sourceNodeId string) error
}

// Narrow consumer interfaces rather than the whole camera and recording services. The
// standby service needs five methods between them; depending on ICameraService (fifty-odd)
// would make a fake in a test fifty stubs of which forty-five are `panic("unused")`, and
// would break this file every time an unrelated ONVIF call is added.
type standbyCameras interface {
	Get(ctx context.Context, limit uint64, offset uint64) ([]*CameraDetail, uint64, error)
	Save(ctx context.Context, detail CameraDetail) (uint64, error)
	VerifyDeviceCredentials(ctx context.Context, detail CameraDetail, credentials onvif.Credentials) (string, error)
}

type standbyRecordings interface {
	GetConfig(ctx context.Context, cameraId int64) (*entities.RecordingConfig, error)
	SaveConfig(ctx context.Context, req SaveRecordingConfigRequest) (*entities.RecordingConfig, error)
}

// standbyRecorder is the running recorder. Activation MUST poke it: saving a recording
// config row starts nothing by itself — the settings screen has always had to hot-reload
// the recorder after a save, and a failover that only wrote rows would report success and
// record nothing until the next restart. It is also the read-back: the recorder knows
// whether ffmpeg is actually running for a camera, which a 200 does not.
type standbyRecorder interface {
	Configure(cfg recording.RecorderConfig) error
	Statuses() []recording.CameraStatus
}

type standbyRecorderConfig interface {
	ForRecording(ctx context.Context, cfg *entities.RecordingConfig) (recording.RecorderConfig, string)
}

// standbyIdentity is the node's own fleet identity (IPairingService satisfies it).
//
// Status rather than NodeID, because the NAME is needed too and it is a live value: an
// operator who renames this appliance has renamed it everywhere the fleet shows it, and a
// name captured at boot would keep the old one until the next restart.
type standbyIdentity interface {
	Status(ctx context.Context) (PairingStatus, error)
}

type standbyService struct {
	repo        dbsql.IGenericRepo[entities.StandbyCamera]
	cameras     standbyCameras
	recordings  standbyRecordings
	recorder    standbyRecorder
	recorderCfg standbyRecorderConfig
	identity    standbyIdentity
	cipher      *atrest.Cipher
	logf        func(string, ...any)

	// settleFor bounds the wait a takeover gives its recorders to put something on disk.
	// A field rather than the constant so a unit test can shorten it: at the shipped value
	// every activation test would pay twelve seconds to assert something that has nothing
	// to do with the wait.
	settleFor time.Duration

	keyMu     sync.Mutex
	recipient *handoff.Recipient
	keyExpiry int64
}

// NewStandbyService builds the appliance's failover surface. recorder/recorderCfg may be
// nil (a build without a recorder, and every unit test that is not about starting ffmpeg);
// cipher may be nil (encryption at rest disabled).
func NewStandbyService(
	repo dbsql.IGenericRepo[entities.StandbyCamera],
	cameras standbyCameras,
	recordings standbyRecordings,
	recorder standbyRecorder,
	recorderCfg standbyRecorderConfig,
	identity standbyIdentity,
	cipher *atrest.Cipher,
	logf func(string, ...any),
) IStandbyService {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &standbyService{
		repo: repo, cameras: cameras, recordings: recordings,
		recorder: recorder, recorderCfg: recorderCfg,
		identity: identity, cipher: cipher, logf: logf,
		settleFor: standbyActivateSettle,
	}
}

// --- the sealed bundle -------------------------------------------------------------

// standbyBundle is what crosses between two appliances. It is never persisted in this
// shape and never leaves either appliance unsealed.
type standbyBundle struct {
	Version        int                   `json:"version"`
	SourceNodeId   string                `json:"sourceNodeId"`
	SourceNodeName string                `json:"sourceNodeName"`
	CreatedAt      int64                 `json:"createdAt"`
	Cameras        []standbyBundleCamera `json:"cameras"`
}

type standbyBundleCamera struct {
	CameraId      int64  `json:"cameraId"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	Host          string `json:"host"`
	Port          int    `json:"port"`
	RTSPUrl       string `json:"rtspUrl"`
	SnapshotURI   string `json:"snapshotUri"`
	RTSPTransport string `json:"rtspTransport"`
	XAddr         string `json:"xAddr"`
	MediaXAddr    string `json:"mediaXAddr"`
	PTZXAddr      string `json:"ptzXAddr"`
	PTZSupported  bool   `json:"ptzSupported"`
	ProfileToken  string `json:"profileToken"`
	Username      string `json:"username"`
	Password      string `json:"password"`

	RecordingWanted bool `json:"recordingWanted"`
	RetentionDays   int  `json:"retentionDays"`
	SegmentMinutes  int  `json:"segmentMinutes"`
	PreRollSec      int  `json:"preRollSec"`
	PostRollSec     int  `json:"postRollSec"`
}

func (s *standbyService) HandoffKey(ctx context.Context) (StandbyHandoffKey, error) {
	nodeId, _, err := s.self(ctx)
	if err != nil {
		return StandbyHandoffKey{}, err
	}
	now := time.Now().Unix()

	s.keyMu.Lock()
	defer s.keyMu.Unlock()
	if s.recipient == nil || now >= s.keyExpiry {
		r, err := handoff.NewRecipient()
		if err != nil {
			return StandbyHandoffKey{}, err
		}
		s.recipient = r
		s.keyExpiry = now + int64(standbyKeyTTL.Seconds())
	}
	return StandbyHandoffKey{
		NodeId:    nodeId,
		PublicKey: base64.StdEncoding.EncodeToString(s.recipient.PublicKey()),
		ExpiresAt: s.keyExpiry,
	}, nil
}

func (s *standbyService) Handoff(ctx context.Context, req StandbyHandoffRequest) (StandbyHandoffResult, error) {
	recipient := strings.TrimSpace(req.RecipientNodeId)
	if recipient == "" {
		return StandbyHandoffResult{}, errors.New("recipientNodeId is required")
	}
	pub, err := base64.StdEncoding.DecodeString(strings.TrimSpace(req.PublicKey))
	if err != nil {
		return StandbyHandoffResult{}, errors.New("publicKey is not valid base64")
	}
	nodeId, nodeName, err := s.self(ctx)
	if err != nil {
		return StandbyHandoffResult{}, err
	}
	// An appliance must not stand by for itself. It reads as harmless until you notice
	// that the whole feature is about one appliance failing, and a plan that names the
	// same box twice is a plan that protects nothing while showing green.
	if recipient == nodeId {
		return StandbyHandoffResult{}, errors.New("an appliance cannot stand by for itself")
	}

	cams, _, err := s.cameras.Get(ctx, standbyMaxCameras, 0)
	if err != nil {
		return StandbyHandoffResult{}, err
	}
	bundle := standbyBundle{
		Version:        standbyBundleVersion,
		SourceNodeId:   nodeId,
		SourceNodeName: nodeName,
		CreatedAt:      time.Now().Unix(),
	}
	for _, cam := range cams {
		if cam == nil {
			continue
		}
		bc := standbyBundleCamera{
			CameraId:      cam.Camera.Id,
			Name:          cam.Camera.Name,
			Description:   cam.Camera.Description,
			Host:          cam.Camera.Host,
			Port:          cam.Camera.Port,
			RTSPUrl:       cam.Camera.RTSPUrl,
			SnapshotURI:   cam.Camera.SnapshotURI,
			RTSPTransport: cam.Camera.RTSPTransport,
			XAddr:         cam.XAddr,
			MediaXAddr:    cam.MediaXAddr,
			PTZXAddr:      cam.PTZXAddr,
			PTZSupported:  cam.PTZSupported,
			ProfileToken:  cam.ProfileToken,
			Username:      cam.Username,
			Password:      cam.Password,
		}
		// The recording INTENT travels with the camera, so a takeover continues what was
		// happening rather than inventing a policy. A camera the source had switched off
		// must not come up recording on the spare.
		if cfg, cerr := s.recordings.GetConfig(ctx, cam.Camera.Id); cerr == nil && cfg != nil {
			bc.RecordingWanted = cfg.Enabled
			bc.RetentionDays = cfg.RetentionDays
			bc.SegmentMinutes = cfg.SegmentMinutes
			bc.PreRollSec = cfg.PreRollSec
			bc.PostRollSec = cfg.PostRollSec
		}
		bundle.Cameras = append(bundle.Cameras, bc)
	}

	plain, err := json.Marshal(bundle)
	if err != nil {
		return StandbyHandoffResult{}, err
	}
	// The recipient's node id is the associated data, which is what stops a bundle
	// captured on its way to one spare from being staged onto another.
	sealed, err := handoff.Seal(pub, []byte(recipient), plain)
	if err != nil {
		return StandbyHandoffResult{}, err
	}
	return StandbyHandoffResult{
		SourceNodeId:   nodeId,
		SourceNodeName: nodeName,
		CameraCount:    len(bundle.Cameras),
		CreatedAt:      bundle.CreatedAt,
		Sealed:         base64.StdEncoding.EncodeToString(sealed),
	}, nil
}

func (s *standbyService) Stage(ctx context.Context, req StandbyStageRequest) (*StandbySet, error) {
	sealed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(req.Sealed))
	if err != nil {
		return nil, errors.New("sealed bundle is not valid base64")
	}
	nodeId, _, err := s.self(ctx)
	if err != nil {
		return nil, err
	}

	s.keyMu.Lock()
	recipient := s.recipient
	expiry := s.keyExpiry
	s.keyMu.Unlock()
	if recipient == nil || time.Now().Unix() >= expiry {
		return nil, ErrStandbyKeyUnknown
	}

	plain, err := recipient.Open(sealed, []byte(nodeId))
	if err != nil {
		if errors.Is(err, handoff.ErrNotForYou) {
			// Deliberately not "decryption failed". This is the one error an operator can
			// act on: the bundle is somebody else's.
			return nil, errors.New("this camera set was sealed for a different appliance")
		}
		return nil, ErrStandbyKeyUnknown
	}

	var bundle standbyBundle
	if err := json.Unmarshal(plain, &bundle); err != nil {
		return nil, fmt.Errorf("unreadable camera set: %w", err)
	}
	if bundle.Version != standbyBundleVersion {
		return nil, fmt.Errorf("this appliance does not understand version %d of a camera set", bundle.Version)
	}
	source := strings.TrimSpace(bundle.SourceNodeId)
	if source == "" {
		return nil, errors.New("the camera set does not say which appliance it came from")
	}
	if source == nodeId {
		return nil, errors.New("an appliance cannot stand by for itself")
	}
	if len(bundle.Cameras) > standbyMaxCameras {
		return nil, fmt.Errorf("a camera set of %d is beyond what this appliance will stage", len(bundle.Cameras))
	}

	existing, err := s.rows(ctx, source)
	if err != nil {
		return nil, err
	}
	byCamera := map[int64]*entities.StandbyCamera{}
	for _, row := range existing {
		byCamera[row.SourceCameraId] = row
	}

	now := time.Now().Unix()
	seen := map[int64]bool{}
	for _, bc := range bundle.Cameras {
		seen[bc.CameraId] = true
		row := entities.StandbyCamera{
			SourceNodeId:    source,
			SourceCameraId:  bc.CameraId,
			SourceNodeName:  bundle.SourceNodeName,
			Name:            bc.Name,
			Description:     bc.Description,
			Host:            bc.Host,
			Port:            bc.Port,
			RTSPUrl:         bc.RTSPUrl,
			SnapshotURI:     bc.SnapshotURI,
			RTSPTransport:   bc.RTSPTransport,
			XAddr:           bc.XAddr,
			MediaXAddr:      bc.MediaXAddr,
			PTZXAddr:        bc.PTZXAddr,
			PTZSupported:    bc.PTZSupported,
			ProfileToken:    bc.ProfileToken,
			Username:        bc.Username,
			Password:        s.seal(bc.Password),
			RecordingWanted: bc.RecordingWanted,
			RetentionDays:   bc.RetentionDays,
			SegmentMinutes:  bc.SegmentMinutes,
			PreRollSec:      bc.PreRollSec,
			PostRollSec:     bc.PostRollSec,
			State:           entities.StandbyStateStaged,
			StagedAt:        now,
		}
		if prev, ok := byCamera[bc.CameraId]; ok {
			row.Id = prev.Id
			// Re-staging must not discard what this appliance has already done with the
			// set. The local camera row it created, the state it is in and the last drill
			// result all belong to THIS appliance, not to the bundle.
			row.LocalCameraId = prev.LocalCameraId
			row.State = prev.State
			row.ActivatedAt = prev.ActivatedAt
			row.ReleasedAt = prev.ReleasedAt
			row.CheckStatus = prev.CheckStatus
			row.CheckDetail = prev.CheckDetail
			row.CheckedAt = prev.CheckedAt
			// A camera whose credentials the source has since rotated arrives with a new
			// password; one whose password field came back empty keeps what we hold rather
			// than being blanked into unusability.
			if bc.Password == "" {
				row.Password = prev.Password
			}
			if _, err := s.repo.UpdateById(ctx, "", row); err != nil {
				return nil, err
			}
			continue
		}
		if _, err := s.repo.Create(ctx, "", row); err != nil {
			return nil, err
		}
	}

	// A camera the source no longer has stops being staged — unless this appliance has
	// already materialized it, in which case the row is the only link between the local
	// camera (and its footage) and where it came from, and dropping it would orphan both.
	for _, row := range existing {
		if seen[row.SourceCameraId] || row.LocalCameraId != 0 {
			continue
		}
		if _, err := s.repo.DeleteById(ctx, "", uint64(row.Id)); err != nil {
			s.logf("standby: dropping stale staged camera %d: %v", row.Id, err)
		}
	}

	return s.set(ctx, source)
}

func (s *standbyService) Status(ctx context.Context) (StandbyStatus, error) {
	nodeId, nodeName, err := s.self(ctx)
	if err != nil {
		return StandbyStatus{}, err
	}
	rows, _, err := s.repo.Get(ctx, "", standbyListPageSize, 0, nil,
		[]sqldataenums.Sorter{{FieldName: "SourceNodeId", Sort: sqldataenums.ASC}})
	if err != nil && !isNoResultFoundErr(err) {
		return StandbyStatus{}, err
	}
	bySource := map[string][]*entities.StandbyCamera{}
	order := []string{}
	for _, row := range rows {
		if row == nil {
			continue
		}
		if _, ok := bySource[row.SourceNodeId]; !ok {
			order = append(order, row.SourceNodeId)
		}
		bySource[row.SourceNodeId] = append(bySource[row.SourceNodeId], row)
	}
	sort.Strings(order)
	out := StandbyStatus{NodeId: nodeId, Name: nodeName, Sets: []StandbySet{}}
	for _, src := range order {
		out.Sets = append(out.Sets, buildStandbySet(bySource[src]))
	}
	return out, nil
}

func (s *standbyService) Drill(ctx context.Context, sourceNodeId string) (*StandbySet, error) {
	rows, err := s.rowsOrErr(ctx, sourceNodeId)
	if err != nil {
		return nil, err
	}

	// Bounded fan-out. The probe is a real ONVIF resolve plus a real RTSP DESCRIBE per
	// camera, and doing forty of them serially makes a drill take minutes; doing forty at
	// once makes one appliance look like an attack to a small switch.
	type result struct {
		idx    int
		status string
		detail string
	}
	sem := make(chan struct{}, standbyDrillConcurrency)
	results := make(chan result, len(rows))
	var wg sync.WaitGroup
	for i, row := range rows {
		wg.Add(1)
		go func(i int, row *entities.StandbyCamera) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			status, detail := s.probe(ctx, row)
			results <- result{idx: i, status: status, detail: detail}
		}(i, row)
	}
	wg.Wait()
	close(results)

	now := time.Now().Unix()
	for r := range results {
		row := rows[r.idx]
		row.CheckStatus = r.status
		row.CheckDetail = r.detail
		row.CheckedAt = now
		if _, err := s.repo.UpdateById(ctx, "", *row); err != nil {
			s.logf("standby: recording drill result for camera %d: %v", row.Id, err)
		}
	}
	set := buildStandbySet(rows)
	return &set, nil
}

// probe asks whether THIS appliance can open one staged camera, right now. It reuses the
// camera service's own verification — the same ONVIF resolve and RTSP DESCRIBE the add-a-
// camera flow uses — so a drill answers the question recording will ask, rather than a
// weaker one (a ping, a TCP connect) that a camera behind a firewall answers happily.
func (s *standbyService) probe(ctx context.Context, row *entities.StandbyCamera) (string, string) {
	detail := s.detailFor(row)
	creds := onvif.Credentials{Username: detail.Username, Password: detail.Password}
	status, err := s.cameras.VerifyDeviceCredentials(ctx, detail, creds)
	switch status {
	case CameraAuthOK:
		return entities.StandbyCheckOK, ""
	case CameraAuthUnauthorized:
		return entities.StandbyCheckUnauthorized, standbyErrText(err)
	default:
		return entities.StandbyCheckUnreachable, standbyErrText(err)
	}
}

func (s *standbyService) Activate(ctx context.Context, sourceNodeId string) (*StandbySet, error) {
	rows, err := s.rowsOrErr(ctx, sourceNodeId)
	if err != nil {
		return nil, err
	}
	now := time.Now().Unix()
	outcomes := map[int64]standbyOutcome{}
	// localCameraId -> sourceCameraId, for the cameras whose recorder has been started and
	// whose verdict is not in yet.
	awaiting := map[int64]int64{}

	for _, row := range rows {
		if row.State == entities.StandbyStateActive && row.LocalCameraId != 0 {
			outcomes[row.SourceCameraId] = standbyOutcome{code: StandbyOutcomeAlready}
			continue
		}
		detail := s.detailFor(row)
		if row.LocalCameraId != 0 {
			// Taking the same set over again reuses the camera row it made last time, so
			// the footage from the previous outage and this one live under one camera.
			detail.Camera.Id = row.LocalCameraId
		}
		id, err := s.cameras.Save(ctx, detail)
		if err != nil {
			outcomes[row.SourceCameraId] = standbyOutcome{
				code: StandbyOutcomeCreateFailed, detail: standbyErrText(err)}
			continue
		}
		row.LocalCameraId = int64(id)
		row.State = entities.StandbyStateActive
		row.ActivatedAt = now
		row.ReleasedAt = 0

		if outcome, started := s.startRecording(ctx, row); started {
			// Its verdict is not known yet — see settleRecording.
			awaiting[row.LocalCameraId] = row.SourceCameraId
		} else {
			outcomes[row.SourceCameraId] = outcome
		}
		if _, err := s.repo.UpdateById(ctx, "", *row); err != nil {
			s.logf("standby: recording activation of camera %d: %v", row.Id, err)
		}
	}

	// Every recorder was started before any of them is judged, so the settle window is paid
	// ONCE for the whole set rather than per camera. A forty-camera site would otherwise
	// spend eight minutes in a takeover, which is the one moment nobody has eight minutes.
	for sourceCameraId, outcome := range s.settleRecording(ctx, awaiting) {
		outcomes[sourceCameraId] = outcome
	}

	set := buildStandbySet(rows)
	attachOutcomes(&set, outcomes)
	return &set, nil
}

// settleRecording waits for the recorders a takeover just started to put something on disk,
// and reports per camera what actually happened.
//
// THE DISTINCTION THIS EXISTS FOR: a live ffmpeg process is not footage. Point a recorder at
// a host this appliance cannot resolve and the process is alive and retrying, so a takeover
// that asks the recorder immediately reports "recording" for a camera it has never reached —
// on the same card whose drill row says "could not be reached". That contradiction is worse
// than saying nothing, because the whole feature is a claim about whether a building is
// being recorded.
//
// LiveFiles is the honest signal: it counts the segment files in the camera's live
// directory, so it is zero until ffmpeg has actually written something.
//
// The wait is bounded and its expiry is NOT a failure — a camera whose process is up and has
// not yet cut its first file says exactly that, rather than being called either recording or
// broken.
func (s *standbyService) settleRecording(ctx context.Context, awaiting map[int64]int64) map[int64]standbyOutcome {
	out := map[int64]standbyOutcome{}
	if len(awaiting) == 0 || s.recorder == nil {
		for _, sourceId := range awaiting {
			out[sourceId] = standbyOutcome{code: StandbyOutcomeNoRecorder}
		}
		return out
	}
	settle := s.settleFor
	if settle <= 0 {
		settle = standbyActivateSettle
	}
	deadline := time.Now().Add(settle)
	var last map[int64]recording.CameraStatus
	for {
		last = map[int64]recording.CameraStatus{}
		writing := 0
		for _, st := range s.recorder.Statuses() {
			if _, want := awaiting[st.CameraId]; !want {
				continue
			}
			last[st.CameraId] = st
			if st.FFmpegRunning && st.LiveFiles > 0 {
				writing++
			}
		}
		if writing == len(awaiting) || time.Now().After(deadline) {
			break
		}
		select {
		case <-ctx.Done():
			// The caller gave up (the control plane's request timed out). Report what is
			// known rather than pretending; the takeover itself already happened.
			goto done
		case <-time.After(min(standbyActivatePoll, settle)):
		}
	}
done:
	for cameraId, sourceId := range awaiting {
		st, seen := last[cameraId]
		switch {
		case !seen || !st.FFmpegRunning:
			detail := ""
			if seen {
				detail = st.LastError
				if detail == "" {
					detail = st.State
				}
			}
			out[sourceId] = standbyOutcome{code: StandbyOutcomeNotRecording, detail: detail}
		case st.LiveFiles > 0:
			out[sourceId] = standbyOutcome{code: StandbyOutcomeRecording}
		case st.LastError != "":
			out[sourceId] = standbyOutcome{code: StandbyOutcomeNotRecording, detail: st.LastError}
		default:
			// Honest third answer. The process is up and has written nothing yet, which is
			// what a camera that is merely slow looks like AND what one that will never
			// connect looks like in the first few seconds. Saying so beats guessing either.
			out[sourceId] = standbyOutcome{code: StandbyOutcomePending}
		}
	}
	return out
}

// startRecording configures the recorder for a just-materialized camera.
//
// It returns (outcome, pending). A `pending` of true means the recorder was started and the
// verdict belongs to settleRecording, which is where "is it actually writing anything"
// is answered — because saving a recording config row starts nothing, and a live ffmpeg
// process is not footage.
func (s *standbyService) startRecording(ctx context.Context, row *entities.StandbyCamera) (standbyOutcome, bool) {
	if !row.RecordingWanted {
		return standbyOutcome{code: StandbyOutcomeNotWanted}, false
	}
	cfg, err := s.recordings.SaveConfig(ctx, SaveRecordingConfigRequest{
		CameraId:       row.LocalCameraId,
		Enabled:        true,
		RetentionDays:  row.RetentionDays,
		SegmentMinutes: row.SegmentMinutes,
		PreRollSec:     row.PreRollSec,
		PostRollSec:    row.PostRollSec,
		// StreamURL/StoragePath are deliberately left to this appliance's own defaults.
		// The other box's disk layout is not ours.
	})
	if err != nil {
		return standbyOutcome{code: StandbyOutcomeConfigFailed, detail: standbyErrText(err)}, false
	}
	if s.recorder == nil || s.recorderCfg == nil {
		return standbyOutcome{code: StandbyOutcomeNoRecorder}, false
	}
	recCfg, warning := s.recorderCfg.ForRecording(ctx, cfg)
	if warning != "" {
		return standbyOutcome{code: StandbyOutcomeRefused, detail: warning}, false
	}
	if err := s.recorder.Configure(recCfg); err != nil {
		return standbyOutcome{code: StandbyOutcomeRefused, detail: standbyErrText(err)}, false
	}
	return standbyOutcome{}, true
}

func (s *standbyService) Release(ctx context.Context, sourceNodeId string) (*StandbySet, error) {
	rows, err := s.rowsOrErr(ctx, sourceNodeId)
	if err != nil {
		return nil, err
	}
	now := time.Now().Unix()
	outcomes := map[int64]standbyOutcome{}

	for _, row := range rows {
		if row.State != entities.StandbyStateActive || row.LocalCameraId == 0 {
			continue
		}
		// Stop recording. The camera row and every segment recorded during the outage
		// STAY: deleting the camera would cascade its footage away, and the footage is the
		// only thing the outage produced that anyone will want afterwards.
		if _, err := s.recordings.SaveConfig(ctx, SaveRecordingConfigRequest{
			CameraId:       row.LocalCameraId,
			Enabled:        false,
			RetentionDays:  row.RetentionDays,
			SegmentMinutes: row.SegmentMinutes,
			PreRollSec:     row.PreRollSec,
			PostRollSec:    row.PostRollSec,
		}); err != nil {
			outcomes[row.SourceCameraId] = standbyOutcome{
				code: StandbyOutcomeStopFailed, detail: standbyErrText(err)}
			continue
		}
		if s.recorder != nil {
			// Configure with Enabled false is how the manager is told to stop and forget a
			// camera. Without it the ffmpeg process keeps running against a camera this
			// appliance is no longer responsible for — the duplicate stream the fail-back
			// was performed to end.
			if err := s.recorder.Configure(recording.RecorderConfig{CameraId: row.LocalCameraId, Enabled: false}); err != nil {
				s.logf("standby: stopping recorder for camera %d: %v", row.LocalCameraId, err)
			}
		}
		row.State = entities.StandbyStateReleased
		row.ReleasedAt = now
		outcomes[row.SourceCameraId] = standbyOutcome{code: StandbyOutcomeStopped}
		if _, err := s.repo.UpdateById(ctx, "", *row); err != nil {
			s.logf("standby: recording release of camera %d: %v", row.Id, err)
		}
	}

	set := buildStandbySet(rows)
	attachOutcomes(&set, outcomes)
	return &set, nil
}

// Forget removes a staged set — the copy of somebody else's camera list — when this
// appliance is no longer standing by for them.
//
// It NEVER deletes a camera row or any footage, including for a set that was activated: the
// local cameras and their recordings outlive the arrangement that created them. What is
// lost is only the link back to where they came from, so a set with materialized cameras
// keeps its rows and is merely marked released. Purging footage is the camera screen's job,
// in front of somebody who meant to.
func (s *standbyService) Forget(ctx context.Context, sourceNodeId string) error {
	rows, err := s.rowsOrErr(ctx, sourceNodeId)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if row.LocalCameraId != 0 {
			if row.State == entities.StandbyStateActive {
				return errors.New("this appliance is still recording that set; fail back first")
			}
			continue
		}
		if _, err := s.repo.DeleteById(ctx, "", uint64(row.Id)); err != nil {
			return err
		}
	}
	return nil
}

// --- plumbing -----------------------------------------------------------------------

// self returns this appliance's fleet id and display name. An appliance with no identity
// cannot take part at all — a sealed set is addressed to a node id, so there is nothing to
// seal to and nothing to check an incoming bundle against.
func (s *standbyService) self(ctx context.Context) (string, string, error) {
	if s.identity == nil {
		return "", "", errors.New("this appliance has no fleet identity")
	}
	st, err := s.identity.Status(ctx)
	if err != nil {
		return "", "", err
	}
	if strings.TrimSpace(st.NodeID) == "" {
		return "", "", errors.New("this appliance has no fleet identity")
	}
	return st.NodeID, st.Name, nil
}

func (s *standbyService) rows(ctx context.Context, sourceNodeId string) ([]*entities.StandbyCamera, error) {
	src := strings.TrimSpace(sourceNodeId)
	if src == "" {
		return nil, errors.New("sourceNodeId is required")
	}
	// Get + Equal, never GetByForeign: that helper returns exactly ONE row, which would
	// turn a forty-camera set into a one-camera set with no error anywhere.
	rows, _, err := s.repo.Get(ctx, "", standbyListPageSize, 0,
		[]sqldataenums.Filter{{FieldName: "SourceNodeId", Compare: sqldataenums.Equal, Value: src}},
		[]sqldataenums.Sorter{{FieldName: "SourceCameraId", Sort: sqldataenums.ASC}})
	if err != nil && !isNoResultFoundErr(err) {
		return nil, err
	}
	out := make([]*entities.StandbyCamera, 0, len(rows))
	for _, row := range rows {
		if row != nil {
			out = append(out, row)
		}
	}
	return out, nil
}

func (s *standbyService) rowsOrErr(ctx context.Context, sourceNodeId string) ([]*entities.StandbyCamera, error) {
	rows, err := s.rows(ctx, sourceNodeId)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, ErrStandbyNoSuchSet
	}
	return rows, nil
}

func (s *standbyService) set(ctx context.Context, sourceNodeId string) (*StandbySet, error) {
	rows, err := s.rowsOrErr(ctx, sourceNodeId)
	if err != nil {
		return nil, err
	}
	set := buildStandbySet(rows)
	return &set, nil
}

// detailFor rebuilds a camera from a staged row, decrypting the password on the way.
func (s *standbyService) detailFor(row *entities.StandbyCamera) CameraDetail {
	return CameraDetail{
		Camera: entities.Camera{
			Name:          row.Name,
			Description:   row.Description,
			Host:          row.Host,
			Port:          row.Port,
			RTSPUrl:       row.RTSPUrl,
			SnapshotURI:   row.SnapshotURI,
			RTSPTransport: row.RTSPTransport,
			IsActive:      true,
		},
		XAddr:        row.XAddr,
		MediaXAddr:   row.MediaXAddr,
		PTZXAddr:     row.PTZXAddr,
		PTZSupported: row.PTZSupported,
		ProfileToken: row.ProfileToken,
		Username:     row.Username,
		Password:     s.open(row.Password),
	}
}

// seal/open wrap the at-rest cipher for the one secret this table holds. The stored form is
// base64 because the column is TEXT across sqlite/postgres/mariadb; a value that is not
// atrest ciphertext passes through unchanged, so a database written before encryption was
// enabled still reads.
func (s *standbyService) seal(plaintext string) string {
	if s.cipher == nil || plaintext == "" {
		return plaintext
	}
	enc, err := s.cipher.EncryptBytes([]byte(plaintext))
	if err != nil {
		return plaintext
	}
	return base64.StdEncoding.EncodeToString(enc)
}

func (s *standbyService) open(stored string) string {
	if stored == "" {
		return ""
	}
	raw, err := base64.StdEncoding.DecodeString(stored)
	if err != nil || !atrest.IsEncrypted(raw) {
		return stored
	}
	if s.cipher == nil {
		return stored
	}
	pt, err := s.cipher.DecryptBytes(raw)
	if err != nil {
		return stored
	}
	return string(pt)
}

// buildStandbySet renders rows into the set view, INCLUDING the readiness verdict — which
// is the one piece of arithmetic in this file worth reading twice.
//
// A set nobody has drilled is "untested", never "ready": the drill is the only evidence
// this appliance can reach those cameras, and in its absence the honest answer is that
// nobody knows. A set where every camera answered is "ready". Anything in between is
// "partial", and one where nothing answered is "blind" — distinguished because they mean
// different things to whoever has to act: partial is a camera problem, blind is a network
// or a credentials problem, and telling somebody "3 of 40" sends them to look at three
// cameras when the answer is the VLAN.
func buildStandbySet(rows []*entities.StandbyCamera) StandbySet {
	set := StandbySet{Cameras: []StandbyCameraView{}, State: entities.StandbyStateStaged}
	if len(rows) == 0 {
		set.Readiness = StandbyReadinessUntested
		return set
	}
	anyActive, allReleased, drilled := false, true, 0
	for _, row := range rows {
		set.SourceNodeId = row.SourceNodeId
		if row.SourceNodeName != "" {
			set.SourceNodeName = row.SourceNodeName
		}
		if row.StagedAt > set.StagedAt {
			set.StagedAt = row.StagedAt
		}
		if row.ActivatedAt > set.ActivatedAt {
			set.ActivatedAt = row.ActivatedAt
		}
		if row.ReleasedAt > set.ReleasedAt {
			set.ReleasedAt = row.ReleasedAt
		}
		if row.CheckedAt > set.DrilledAt {
			set.DrilledAt = row.CheckedAt
		}
		if row.State == entities.StandbyStateActive {
			anyActive = true
		}
		if row.State != entities.StandbyStateReleased {
			allReleased = false
		}
		if row.CheckStatus != "" {
			drilled++
			if row.CheckStatus == entities.StandbyCheckOK {
				set.Reachable++
			}
		}
		set.Cameras = append(set.Cameras, StandbyCameraView{
			SourceCameraId:  row.SourceCameraId,
			Name:            row.Name,
			Host:            row.Host,
			State:           row.State,
			LocalCameraId:   row.LocalCameraId,
			RecordingWanted: row.RecordingWanted,
			CheckStatus:     row.CheckStatus,
			CheckDetail:     row.CheckDetail,
			CheckedAt:       row.CheckedAt,
		})
	}
	set.Total = len(rows)
	switch {
	case anyActive:
		set.State = entities.StandbyStateActive
	case allReleased:
		set.State = entities.StandbyStateReleased
	}

	switch {
	case anyActive:
		set.Readiness = StandbyReadinessActive
	case drilled == 0:
		set.Readiness = StandbyReadinessUntested
	case set.Reachable == set.Total:
		set.Readiness = StandbyReadinessReady
	case set.Reachable == 0:
		set.Readiness = StandbyReadinessBlind
	default:
		set.Readiness = StandbyReadinessPartial
	}
	return set
}

func attachOutcomes(set *StandbySet, outcomes map[int64]standbyOutcome) {
	for i := range set.Cameras {
		if v, ok := outcomes[set.Cameras[i].SourceCameraId]; ok && v.code != "" {
			set.Cameras[i].Outcome = v.code
			set.Cameras[i].OutcomeDetail = v.detail
		}
	}
}

func standbyErrText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
