package entities

// StandbyCamera is one camera belonging to ANOTHER appliance that this one is prepared to
// record if that appliance stops (W3-7, N+1 failover).
//
// The row is deliberately a COPY of everything needed to open the camera, not a reference
// to anything. That is the entire point: at the moment failover matters, the appliance that
// held the original is unreachable — it has been stolen, its power supply has died, the
// switch it hangs off has failed — and nothing can be fetched from it any more. Whatever
// this node did not already have, it will never get. So the copy is taken while the other
// appliance is HEALTHY, and the staging date is reported next to it, because a camera set
// copied three months ago is a promise about a building that may since have gained cameras.
//
// It is NOT a camera row. A staged camera does not appear in the camera list, is not
// probed by the health monitor, is not recorded and is not visible on the wall — a spare
// appliance covering four recorders would otherwise show four sites' worth of cameras that
// it is not watching, which is exactly the display a control room must not be given. The
// camera row is created at ACTIVATION and only then (see services/standby.go).
type StandbyCamera struct {
	Id int64 `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	// SourceNodeId is the appliance this camera actually belongs to; SourceCameraId is
	// its id THERE. Together they are the identity of a staged camera, so re-staging an
	// unchanged fleet updates rows instead of accumulating duplicates.
	SourceNodeId   string `json:"sourceNodeId" form:"sourceNodeId" query:"sourceNodeId" ukey:"standby_src" idx:"src" validate:"required"`
	SourceCameraId int64  `json:"sourceCameraId" form:"sourceCameraId" query:"sourceCameraId" ukey:"standby_src" validate:"required"`
	// SourceNodeName is what the other appliance calls itself, carried so this node can
	// name it on screen without asking a control plane it may not be able to reach.
	SourceNodeName string `json:"sourceNodeName" form:"sourceNodeName" query:"sourceNodeName"`

	Name        string `json:"name" form:"name" query:"name"`
	Description string `json:"description" form:"description" query:"description"`
	Host        string `json:"host" form:"host" query:"host"`
	Port        int    `json:"port" form:"port" query:"port"`

	RTSPUrl       string `json:"rtspUrl" form:"rtspUrl" query:"rtspUrl"`
	SnapshotURI   string `json:"snapshotUri" form:"snapshotUri" query:"snapshotUri"`
	RTSPTransport string `json:"rtspTransport" form:"rtspTransport" query:"rtspTransport"`

	XAddr        string `json:"xAddr" form:"xAddr" query:"xAddr"`
	MediaXAddr   string `json:"mediaXAddr" form:"mediaXAddr" query:"mediaXAddr"`
	PTZXAddr     string `json:"ptzXAddr" form:"ptzXAddr" query:"ptzXAddr"`
	PTZSupported bool   `json:"ptzSupported" form:"ptzSupported" query:"ptzSupported"`
	ProfileToken string `json:"profileToken" form:"profileToken" query:"profileToken"`

	Username string `json:"username" form:"username" query:"username"`
	// Password is the camera login, encrypted at rest with this appliance's own key and
	// NEVER serialized to a client (`json:"-"`, exactly as the camera table's own password
	// is). A staged set is somebody else's credentials held on our disk; it gets the
	// stronger treatment of the two, not the weaker.
	Password string `json:"-" form:"password" query:"password"`

	// The recording intent copied from the source, so a camera that comes up here comes up
	// recording the way it was recording there. StoragePath is deliberately NOT copied:
	// the other appliance's disk layout is not ours, and inheriting a path that does not
	// exist here is how a takeover records nothing while reporting success.
	RetentionDays  int `json:"retentionDays" form:"retentionDays" query:"retentionDays"`
	SegmentMinutes int `json:"segmentMinutes" form:"segmentMinutes" query:"segmentMinutes"`
	PreRollSec     int `json:"preRollSec" form:"preRollSec" query:"preRollSec"`
	PostRollSec    int `json:"postRollSec" form:"postRollSec" query:"postRollSec"`
	// RecordingWanted is whether the source was actually recording this camera. A camera
	// the other appliance had switched off must not come up recording here — taking over
	// is meant to continue what was happening, not to change it.
	RecordingWanted bool `json:"recordingWanted" form:"recordingWanted" query:"recordingWanted"`

	// LocalCameraId is the camera row this appliance created when it took over, and 0
	// before that ever happened. It is kept after a fail-back on purpose: the footage
	// recorded during the outage belongs to that row, and deleting the row would take the
	// footage with it. See StandbyStateReleased.
	LocalCameraId int64 `json:"localCameraId" form:"localCameraId" query:"localCameraId"`

	// State is one of the StandbyState* values.
	State       string `json:"state" form:"state" query:"state" idx:"src"`
	StagedAt    int64  `json:"stagedAt" form:"stagedAt" query:"stagedAt"`
	ActivatedAt int64  `json:"activatedAt" form:"activatedAt" query:"activatedAt"`
	ReleasedAt  int64  `json:"releasedAt" form:"releasedAt" query:"releasedAt"`

	// The last drill result for this camera: whether THIS appliance could actually reach
	// it and log in. A staged set that has never been drilled is a filing cabinet, not a
	// failover — the spare may be on a different VLAN, the camera may reject a second
	// concurrent login, the credentials may have been rotated on the camera and only
	// updated on the appliance that owns it.
	CheckStatus string `json:"checkStatus" form:"checkStatus" query:"checkStatus"`
	CheckDetail string `json:"checkDetail" form:"checkDetail" query:"checkDetail"`
	CheckedAt   int64  `json:"checkedAt" form:"checkedAt" query:"checkedAt"`
}

// Standby camera states.
const (
	// StandbyStateStaged means the copy is held and nothing is running from it.
	StandbyStateStaged = "staged"
	// StandbyStateActive means this appliance created the camera and is recording it.
	StandbyStateActive = "active"
	// StandbyStateReleased means it was taken over and has since been handed back. The
	// camera row and its footage stay; recording is off.
	StandbyStateReleased = "released"
)

// Drill outcomes for one staged camera. They mirror the camera service's credential
// verdicts, and keep the same distinction: a camera that REJECTED the login is a fact
// about the camera, and one that could not be reached is a fact about the network. Rolling
// the second into the first sends an engineer to re-type a password that was always right.
const (
	StandbyCheckOK           = "ok"
	StandbyCheckUnauthorized = "unauthorized"
	StandbyCheckUnreachable  = "unreachable"
)
