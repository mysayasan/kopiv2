package services

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/mysayasan/kopiv2/domain/entities"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// This file is the suite's answer to "can I run this behind a load balancer?".
//
// The honest answer differs per app, and until now nothing said so. Two apps
// (myseliasan, myidsan) are stateless over Postgres and can genuinely be
// replicated. The other three are appliances bound to hardware a second process
// cannot share — an OSDP bus opens its serial port once and holds it for the
// bus's lifetime, a Modbus RTU poller does the same, and an NVR owns its RTSP
// pipelines, its recordings on local disk and its GPU. Replicating those is not
// hard, it is meaningless, so the answer for them is a reason rather than a knob.
//
// Two things live here. DeploymentModeService persists what the operator DECLARED
// (see the comment on it for why that is not redundant with configuration), and
// Preflight turns the surrounding configuration into a checklist an operator can
// act on. Everything Preflight needs arrives as plain values in DeploymentEnv so
// this package keeps its current dependency surface — apphost already imports it,
// so it can never import apphost back.

// DeploymentModeKey is the RuntimeSetting key the declared deployment mode is
// stored under, alongside SetupStateKey.
const DeploymentModeKey = "deployment.mode"

// Deployment modes. Standalone is the default and the zero value's meaning: an
// install that never answered the question is single-instance, which is what
// every install was before this existed.
const (
	ModeStandalone = "standalone"
	ModeClustered  = "clustered"
)

// Preflight severities. A failed blocker means the deployment is wrong and will
// misbehave; a failed warning means it will work but wastes resources or will
// bite under load.
const (
	SeverityBlocker = "blocker"
	SeverityWarning = "warning"
)

// Preflight check ids. Exported because the UI maps them to translated labels and
// the manual anchors them — a typo'd id silently renders an untranslated row.
const (
	CheckDbEngine    = "dbEngine"
	CheckSharedCache = "sharedCache"
	CheckSharedLock  = "sharedLock"
	CheckAtRestKey   = "atrestKey"
	CheckJwtSecret   = "jwtSecret"
	CheckDbPool      = "dbPool"
)

// DeploymentState is the operator's declared intent, persisted as a single
// key-value row so it survives restarts and is the same answer for every browser.
type DeploymentState struct {
	Mode string `json:"mode"`
	// Acknowledged records that the operator was shown, and accepted, the list of
	// features that are not yet cluster-safe. It is stored rather than inferred so
	// a later build can tell "agreed to the caveats of the day" from "never asked".
	Acknowledged bool  `json:"acknowledged"`
	UpdatedAt    int64 `json:"updatedAt"`
}

// Clustered reports whether the operator declared a multi-instance deployment.
func (s DeploymentState) Clustered() bool { return s.Mode == ModeClustered }

// IDeploymentModeService reads and records the declared deployment mode.
type IDeploymentModeService interface {
	Get(ctx context.Context) (DeploymentState, error)
	Set(ctx context.Context, mode string, acknowledged bool) (DeploymentState, error)
}

type deploymentModeService struct {
	repo dbsql.IGenericRepo[entities.RuntimeSetting]
}

// NewDeploymentModeService stores the declared mode in the shared runtime-setting
// table under a dedicated key.
func NewDeploymentModeService(repo dbsql.IGenericRepo[entities.RuntimeSetting]) IDeploymentModeService {
	return &deploymentModeService{repo: repo}
}

func (s *deploymentModeService) Get(ctx context.Context) (DeploymentState, error) {
	row, err := s.repo.GetByUnique(ctx, "", "key", DeploymentModeKey)
	if err != nil {
		if isNoResultFoundErr(err) {
			return DeploymentState{Mode: ModeStandalone}, nil
		}
		return DeploymentState{}, err
	}
	if row == nil || row.Value == "" {
		return DeploymentState{Mode: ModeStandalone}, nil
	}
	var state DeploymentState
	if err := json.Unmarshal([]byte(row.Value), &state); err != nil {
		// A corrupt value reads as standalone rather than wedging on a parse error.
		// Standalone is the safe direction to fail: it keeps every singleton running
		// on this instance, which is exactly what a lone process needs.
		return DeploymentState{Mode: ModeStandalone}, nil
	}
	if state.Mode == "" {
		state.Mode = ModeStandalone
	}
	return state, nil
}

func (s *deploymentModeService) Set(ctx context.Context, mode string, acknowledged bool) (DeploymentState, error) {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode != ModeStandalone && mode != ModeClustered {
		return DeploymentState{}, fmt.Errorf("deployment mode must be %q or %q", ModeStandalone, ModeClustered)
	}
	state := DeploymentState{Mode: mode, Acknowledged: acknowledged, UpdatedAt: time.Now().UTC().Unix()}
	payload, err := json.Marshal(state)
	if err != nil {
		return DeploymentState{}, err
	}
	row, err := s.repo.GetByUnique(ctx, "", "key", DeploymentModeKey)
	if err != nil {
		if isNoResultFoundErr(err) {
			return state, s.insert(ctx, string(payload), state.UpdatedAt)
		}
		return DeploymentState{}, err
	}
	// A repo that signals "missing" with a zero row rather than an error would
	// otherwise UPDATE id 0 and silently persist nothing — the same trap
	// SetupStateService guards against.
	if row == nil || row.Id == 0 {
		return state, s.insert(ctx, string(payload), state.UpdatedAt)
	}
	row.Value = string(payload)
	row.UpdatedAt = state.UpdatedAt
	if _, err := s.repo.UpdateById(ctx, "", *row); err != nil {
		return DeploymentState{}, err
	}
	return state, nil
}

func (s *deploymentModeService) insert(ctx context.Context, payload string, ts int64) error {
	_, err := s.repo.Create(ctx, "", entities.RuntimeSetting{
		Key:       DeploymentModeKey,
		Value:     payload,
		CreatedAt: ts,
		UpdatedAt: ts,
	})
	return err
}

// PreflightCheck is one row of the readiness checklist.
type PreflightCheck struct {
	Id       string `json:"id"`
	Severity string `json:"severity"`
	Ok       bool   `json:"ok"`
	// Detail carries the observed value (an engine name, a key id, a connection
	// count) rather than advice. The UI owns the wording — it has to translate it —
	// and a value the operator can compare between two instances is the part a
	// translation cannot supply.
	Detail string `json:"detail,omitempty"`
}

// Appliance reason codes. These are CODES, not prose: the UI ships in four
// languages, so the sentence an operator reads has to come from the translation
// dictionaries. The server's job is to say which constraint applies.
const (
	// ApplianceSerialBus — the app drives field hardware over a serial port that
	// exactly one process on one host can hold open (OSDP reader bus, Modbus RTU).
	ApplianceSerialBus = "serial-bus"
	// ApplianceLocalMedia — the app owns capture pipelines and writes media to local
	// disk, optionally pinned to this host's GPU.
	ApplianceLocalMedia = "local-media"
)

// PreflightReport answers "may this app be clustered, and is this instance
// configured for it?".
type PreflightReport struct {
	// Clusterable is false for the appliance apps. When false the checklist is
	// empty and ApplianceReason carries the reason CODE (see the constants above).
	Clusterable     bool   `json:"clusterable"`
	ApplianceReason string `json:"applianceReason,omitempty"`
	// Mode is the declared intent, echoed so the SPA needs one round trip.
	Mode string `json:"mode"`
	// Acknowledged echoes whether the operator already accepted the known limitations,
	// so the UI can show that as settled rather than presenting an unticked box to
	// somebody who ticked it last week.
	Acknowledged bool             `json:"acknowledged"`
	Checks       []PreflightCheck `json:"checks,omitempty"`
	// Ready is true when no BLOCKER failed. Warnings do not clear it, because a
	// deployment that merely wastes memory still works. Meaningful only when
	// Clusterable — an appliance reports false because there is nothing to be ready
	// FOR, not because anything is wrong with it, so read Clusterable first.
	Ready bool `json:"ready"`
}

// DeploymentEnv is everything Preflight inspects, passed as plain values and
// narrow callbacks. Deliberately not the config struct or the cache/lock types:
// this package is imported by apphost, so taking those would risk an import
// cycle, and the checks only ever need the values below.
type DeploymentEnv struct {
	// Appliance marks an app that is single-instance by design. ApplianceReason is
	// one of the reason CODES above, which the UI turns into a translated sentence.
	Appliance       bool
	ApplianceReason string

	DbEngine      string
	CacheProvider string
	LockProvider  string
	JwtSecret     string
	// JwtSecretGenerated reports that the host had to invent this instance's signing
	// secret at boot because none was configured. It is the signal that actually
	// matters — see checkJwtSecret — because by the time anything can be asked, the
	// in-memory secret is always populated.
	JwtSecretGenerated bool
	MaxOpenConns       int

	// AtRestEnabled/AtRestFingerprint surface the encryption key's identity. The
	// fingerprint is the whole point of the row: two instances that cannot decrypt
	// each other's sealed columns look completely healthy until something reads one.
	// Supply atrest.KeyStore.Fingerprint() — a function of the key material — not the
	// install marker's KeyId, which differs between hosts that share one key.
	AtRestEnabled     bool
	AtRestFingerprint string

	// CachePing / LockPing prove the shared services are actually reachable from
	// THIS instance, which a provider name alone does not. Optional — a nil ping
	// means the provider is not configured as shared, and the name check already
	// failed the row.
	CachePing func(context.Context) error
	LockPing  func(context.Context) error

	// ExtraChecks contributes app-specific rows (myseliasan's LLM sidecar). Run
	// after the shared rows so app concerns read as an addendum.
	ExtraChecks func(context.Context) []PreflightCheck
}

// Pinger is anything that can prove it is reachable from this process. Both
// cache.Store and coordination.Locker satisfy it, which is why the preflight can
// take one helper for the two of them without importing either package.
type Pinger interface {
	Ping(ctx context.Context) error
}

// PingFunc adapts a Pinger to the callback DeploymentEnv expects, returning nil
// for a nil Pinger so an app that has not wired one reports "unreachable" rather
// than panicking during setup — the moment an operator is least able to diagnose it.
func PingFunc(p Pinger) func(context.Context) error {
	if p == nil {
		return nil
	}
	return p.Ping
}

// IsSharedCacheProvider reports whether the cache is visible to other processes.
// It is a name check rather than a capability on the store because the provider
// string is what the operator actually wrote in config.json, and that is what the
// checklist and the boot warning have to talk about. Exported so apphost's
// startup boundary warning and this checklist can never disagree.
func IsSharedCacheProvider(provider string) bool {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "redis", "rediscluster", "redis-cluster":
		return true
	default:
		return false
	}
}

// IsDistributedLockProvider reports whether the transaction lock coordinates
// across processes.
func IsDistributedLockProvider(provider string) bool {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "redis", "rediscluster", "redis-cluster":
		return true
	default:
		return false
	}
}

// Preflight evaluates cluster readiness for this instance. It never returns an
// error: every check that cannot be proven is reported as a failed row, because
// an operator reading a checklist needs a verdict per line, not one failure that
// hides the other six.
func Preflight(ctx context.Context, env DeploymentEnv, state DeploymentState) PreflightReport {
	mode := state.Mode
	if mode == "" {
		mode = ModeStandalone
	}
	if env.Appliance {
		return PreflightReport{Clusterable: false, ApplianceReason: env.ApplianceReason, Mode: ModeStandalone}
	}

	checks := []PreflightCheck{
		checkDbEngine(env.DbEngine),
		pingCheck(ctx, CheckSharedCache, env.CacheProvider, IsSharedCacheProvider(env.CacheProvider), env.CachePing,
			"cache.provider"),
		pingCheck(ctx, CheckSharedLock, env.LockProvider, IsDistributedLockProvider(env.LockProvider), env.LockPing,
			"transaction.lockProvider"),
		checkAtRestKey(env),
		checkJwtSecret(env.JwtSecret, env.JwtSecretGenerated),
		checkDbPool(env.MaxOpenConns),
	}
	if env.ExtraChecks != nil {
		checks = append(checks, env.ExtraChecks(ctx)...)
	}

	ready := true
	for _, c := range checks {
		if c.Severity == SeverityBlocker && !c.Ok {
			ready = false
		}
	}
	return PreflightReport{
		Clusterable:  true,
		Mode:         mode,
		Acknowledged: state.Acknowledged,
		Checks:       checks,
		Ready:        ready,
	}
}

// checkDbEngine rejects sqlite. This is the one check that can never be fixed by
// configuration: sqlite is pinned to a single connection because concurrent
// writers to one file deadlock, and the file is local to one host besides. An
// install on sqlite cannot be clustered at all, so it is reported first.
func checkDbEngine(engine string) PreflightCheck {
	engine = strings.ToLower(strings.TrimSpace(engine))
	if engine == "" {
		engine = "postgres"
	}
	return PreflightCheck{
		Id:       CheckDbEngine,
		Severity: SeverityBlocker,
		Ok:       engine != "sqlite",
		Detail:   engine,
	}
}

// pingCheck combines "is this provider shared at all?" with "can this instance
// actually reach it?". The name check alone passes for a Redis that is
// unreachable, which is the failure an operator most needs to catch during setup
// rather than at 3am.
func pingCheck(ctx context.Context, id, provider string, shared bool, ping func(context.Context) error, key string) PreflightCheck {
	provider = strings.TrimSpace(provider)
	if provider == "" {
		provider = "default"
	}
	if !shared {
		return PreflightCheck{Id: id, Severity: SeverityBlocker, Ok: false,
			Detail: fmt.Sprintf("%s=%s", key, provider)}
	}
	if ping == nil {
		return PreflightCheck{Id: id, Severity: SeverityBlocker, Ok: false,
			Detail: fmt.Sprintf("%s=%s (unreachable)", key, provider)}
	}
	if err := ping(ctx); err != nil {
		return PreflightCheck{Id: id, Severity: SeverityBlocker, Ok: false,
			Detail: fmt.Sprintf("%s=%s: %v", key, provider, err)}
	}
	return PreflightCheck{Id: id, Severity: SeverityBlocker, Ok: true,
		Detail: fmt.Sprintf("%s=%s", key, provider)}
}

// checkAtRestKey surfaces the encryption key's FINGERPRINT so an operator can
// compare it across instances. Nothing else in the product exposes it, and a
// mismatch is silent: the second instance mints its own key on first boot, then
// fails to decrypt sealed columns written by the first — for myseliasan that is
// the fleet CA key and PSK, i.e. the whole fleet's trust.
//
// Only the human comparing two screens can complete this check, so the row passes
// whenever a key exists and the detail carries the value to compare. It is a
// fingerprint of the key material rather than the install marker's id precisely so
// that the comparison means what an operator will assume it means — see
// atrest.Cipher.Fingerprint.
func checkAtRestKey(env DeploymentEnv) PreflightCheck {
	if !env.AtRestEnabled {
		// Encryption off means there is nothing sealed to disagree about.
		return PreflightCheck{Id: CheckAtRestKey, Severity: SeverityBlocker, Ok: true, Detail: "disabled"}
	}
	if strings.TrimSpace(env.AtRestFingerprint) == "" {
		return PreflightCheck{Id: CheckAtRestKey, Severity: SeverityBlocker, Ok: false}
	}
	return PreflightCheck{Id: CheckAtRestKey, Severity: SeverityBlocker, Ok: true, Detail: env.AtRestFingerprint}
}

// checkJwtSecret requires an EXPLICITLY CONFIGURED signing secret.
//
// Testing for a blank secret would be useless: when none is configured the host
// invents one at boot and writes it back into this instance's own config file, so
// by the time anything can ask, the value is always populated. What it is not is
// the same value the instance next to it invented — and a token minted by one is
// then rejected by the other, which presents as users being randomly signed out
// rather than as a configuration error anybody would go looking for.
//
// So the signal is provenance, not emptiness. Unlike the at-rest row this reports
// no fingerprint: an operator-chosen secret only has to clear a 16-character floor,
// and publishing a fingerprint of a possibly-weak secret to every signed-in user
// would hand them an offline oracle for forging tokens. The at-rest key is always
// 32 random bytes, which is why that one is safe to show and this one is not.
func checkJwtSecret(secret string, generated bool) PreflightCheck {
	configured := strings.TrimSpace(secret) != "" && !generated
	detail := "configured"
	if generated {
		detail = "generated"
	} else if strings.TrimSpace(secret) == "" {
		detail = "unset"
	}
	return PreflightCheck{
		Id:       CheckJwtSecret,
		Severity: SeverityBlocker,
		Ok:       configured,
		Detail:   detail,
	}
}

// checkDbPool is a warning, not a blocker: each instance opens its own pool
// against the same server, so N instances claim N times the budget. The default
// of 25 is a considerate slice for one process and an inconsiderate one for six.
func checkDbPool(maxOpen int) PreflightCheck {
	if maxOpen <= 0 {
		maxOpen = 25 // defaultMaxOpenConns in infra/db/sql
	}
	return PreflightCheck{
		Id:       CheckDbPool,
		Severity: SeverityWarning,
		Ok:       true,
		Detail:   fmt.Sprintf("%d per instance", maxOpen),
	}
}
