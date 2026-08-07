package app

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/gorilla/mux"
	sharedentities "github.com/mysayasan/kopiv2/domain/entities"
	"github.com/mysayasan/kopiv2/domain/notification"
	sharedapis "github.com/mysayasan/kopiv2/domain/shared/apis"
	"github.com/mysayasan/kopiv2/domain/shared/fleetnode"
	"github.com/mysayasan/kopiv2/infra/apphost"
	"github.com/mysayasan/kopiv2/infra/atrest"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
	"github.com/mysayasan/kopiv2/infra/pairing"
	"github.com/mysayasan/kopiv2/infra/safego"
	"github.com/mysayasan/kopiv2/infra/versioning"
)

// The node side of the fleet: mypintusan is adopted by myseliasan exactly the way mymatasan and
// myiotsan are, on the same shared stack (domain/shared/fleetnode).
//
// TWO CHANNELS, NOT THREE. mymatasan dials a third — the media channel — to stream camera RTP up
// for live view. A door controller has no video, so it does not open that port at all. An unused
// listener is an unused attack surface, and the honest thing is not to have one.
//
// EVERYTHING IS NODE-DIALED. The control plane never needs a route back to the node: mypintusan
// reaches out, and that is what lets it sit behind NAT on a building's LAN with no inbound
// firewall rule. It is also why adoption is a deliberate act rather than something a scanner can
// do to you.
//
// The node reports KindDoor, so the control plane knows what it adopted. A camera node has
// recordings and live views; a sensor node has telemetry and relays; a door node has doors, badge
// decisions and an access log. They are not interchangeable, and a fleet UI that cannot tell them
// apart is a fleet UI that will offer to play back the footage from a door.
type fleet struct {
	enrollment *fleetnode.EnrollmentManager
	control    *fleetnode.ControlChannelManager
	pairing    fleetnode.IPairingService
	httpsPort  int
}

// buildFleet constructs the node's pairing service and its two channels. They are started by the
// caller, alongside the other background workers.
func buildFleet(
	api *mux.Router,
	deps apphost.Dependencies,
	appVersion string,
	cipher *atrest.Cipher,
	notificationService *notification.Service,
) fleet {
	httpsPort := 0
	if len(deps.Config.Server.TLSPorts) > 0 {
		httpsPort = deps.Config.Server.TLSPorts[0]
	}

	settingsRepo := dbsql.NewGenericRepo[sharedentities.RuntimeSetting](deps.Db)
	pairingService := fleetnode.NewPairingService(
		settingsRepo, cipher,
		"mypintusan",
		"", // node name: defaults to the hostname
		appVersion,
		// What this node IS. It travels to the control plane twice: as an advisory hint in the
		// discovery announce (unsigned, so a mixed-version fleet keeps working), and
		// authoritatively in the adopt reply, which is fleet-key-signed and claim-code-gated.
		fleetnode.KindDoor,
	)

	enrollment := fleetnode.NewEnrollmentManager(
		pairingService,
		deps.Config.Pairing.MTLSPort,
		time.Duration(deps.Config.Pairing.RenewBeforeHours)*time.Hour,
		func(format string, args ...any) { deps.Logger.Infof("mypintusan.pairing", format, args...) },
	)

	// Commands down, events up. The dispatcher re-injects a tunnelled command into THIS app's own
	// /api router, so a control-plane operator reaches exactly the surface a local operator would —
	// gated by this node's own authorization, against this node's own roles. There is no second,
	// weaker path in.
	//
	// That matters more here than anywhere else in the suite. A tunnelled command that reached an
	// unguarded path on a door controller could open a door, and the node — not the parent — is the
	// thing that must decide who may do that.
	control := fleetnode.NewControlChannelManager(
		pairingService,
		deps.Config.Pairing.ControlPort,
		appVersion,
		sharedapis.NewControlDispatcher(api, deps.AccessRoles),
		func(format string, args ...any) { deps.Logger.Infof("mypintusan.control", format, args...) },
	)

	// Every notification this node raises — a forced door, a duress alarm, a badge decision, a
	// sign-in lockout — also flows up the channel into the control plane's unified feed.
	//
	// The badge DECISIONS are the reason the fifth app joins the fleet: once myseliasan holds
	// events from camera nodes, sensor nodes AND door nodes, the flagship rule becomes real —
	// motion on a camera AND a door contact opening AND no badge accepted. Neither node can see
	// that on its own.
	notificationService.Register(fleetnode.NewControlEventSink(control))

	return fleet{enrollment: enrollment, control: control, pairing: pairingService, httpsPort: httpsPort}
}

// start runs the node's discovery responder and its two channels until ctx is cancelled.
func (f fleet) start(ctx context.Context, deps apphost.Dependencies) {
	// Answers authenticated discovery probes while the node is unpaired and a fleet key is set,
	// then goes SILENT once adopted. A node that keeps announcing itself after adoption is a node
	// advertising itself to whoever else is listening.
	responder := pairing.NewResponder(pairing.ResponderConfig{
		FleetKey:      func() []byte { k, _ := f.pairing.FleetKey(ctx); return k },
		Discoverable:  func() bool { return f.pairing.Discoverable(ctx) },
		AnnounceInfo:  func() pairing.AnnounceInfo { return f.pairing.AnnounceInfo(ctx, f.httpsPort) },
		MulticastAddr: deps.Config.Pairing.MulticastAddr,
		ReplayWindow:  time.Duration(deps.Config.Pairing.ReplayWindowSeconds) * time.Second,
		Logf:          func(format string, args ...any) { deps.Logger.Infof("mypintusan.pairing", format, args...) },
	})
	safego.Supervise(ctx, "mypintusan.pairing.responder", func(ctx context.Context) {
		if err := responder.Run(ctx); err != nil && ctx.Err() == nil {
			deps.Logger.Warnf("mypintusan.pairing", "discovery responder stopped: %v", err)
		}
	})

	// Supervised, not bare `go`: a panic in either of these would otherwise be silent, and the
	// node would simply stop enrolling or stop answering the parent with nothing to say why.
	safego.Supervise(ctx, "mypintusan.enrollment", func(ctx context.Context) { f.enrollment.Run(ctx) })
	safego.Supervise(ctx, "mypintusan.control", func(ctx context.Context) { f.control.Run(ctx) })
}

// appVersion resolves this build's version for the fleet handshake. mypintusan has no entry in the
// version manifest's apps map yet (its changes ship under the core scope), so this reads "0.0.0"
// until one is added — cosmetic in the fleet UI, and the honest value until the app cuts releases.
func appVersion(m *module) string {
	if manifest, err := versioning.LoadDefault(); err == nil {
		if info, err := manifest.InfoForApp(m.Name()); err == nil {
			return info.AppVersion
		}
	}
	return "0.0.0"
}

// boolValue dereferences an optional bool config flag.
func boolValue(v *bool, fallback bool) bool {
	if v == nil {
		return fallback
	}
	return *v
}

// openFleetSecretCipher resolves the at-rest key that protects the node's fleet secrets — the
// fleet key, the pairing token, the mTLS private key. Without it they sit in the database in
// plaintext, and anyone who can read the file can impersonate the node to its control plane.
//
// It FAILS CLOSED when a key that existed before is now missing: minting a new one would silently
// orphan the encrypted enrollment and quietly un-adopt the node from its fleet.
func openFleetSecretCipher(deps apphost.Dependencies) (*atrest.Cipher, error) {
	if !boolValue(deps.Config.Security.EncryptAtRest, true) {
		return nil, nil
	}
	keyPath := strings.TrimSpace(deps.Config.Security.KeyPath)
	if keyPath == "" {
		keyPath, _ = filepath.Abs(apphost.ResolveWritablePath(deps.DataDir, filepath.Join("secret", "atrest.key")))
	}
	recoveryPath := strings.TrimSpace(deps.Config.Security.RecoveryPath)
	if recoveryPath == "" {
		recoveryPath = filepath.Join(filepath.Dir(keyPath), "recovery.atrestkey")
	}
	outcome, err := atrest.OpenForStartup(keyPath, recoveryPath, atrest.ProtectorConfig{
		Name:           deps.Config.Security.KeyProtector,
		Passphrase:     deps.Config.Security.Passphrase,
		PassphraseFile: deps.Config.Security.PassphraseFile,
		PassphraseEnv:  deps.Config.Security.PassphraseEnv,
	})
	if err != nil {
		return nil, fmt.Errorf("fleet-secret encryption key: %w", err)
	}
	if outcome.Mode == atrest.ModeRecoveryPending {
		return nil, fmt.Errorf("fleet-secret encryption key missing (id %s): restore %s or set security.recoveryPath, then restart — refusing to orphan this node's fleet enrollment", outcome.KeyId, keyPath)
	}
	return outcome.KeyStore.Cipher(), nil
}
