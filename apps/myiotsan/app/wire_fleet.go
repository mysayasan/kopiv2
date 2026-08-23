package app

import (
	"context"
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
)

// The node side of the fleet: myiotsan is adopted by myseliasan exactly the way mymatasan is,
// on the same shared stack (domain/shared/fleetnode).
//
// TWO CHANNELS, NOT THREE. mymatasan dials a third — the media channel — to stream camera RTP
// up for live view. A sensor hub has no video, so it does not open that port at all. An unused
// listener is an unused attack surface, and the honest thing is not to have one.
//
// EVERYTHING IS NODE-DIALED. The control plane never needs a route back to the node: myiotsan
// reaches out, and that is what lets it sit behind NAT on a building's LAN with no inbound
// firewall rule. It is also why adoption is a deliberate act rather than something a scanner
// can do to you.
//
// The node reports KindIot, so the control plane knows what it adopted. A camera node has
// recordings and live views; a sensor node has telemetry and relays. They are not
// interchangeable, and a fleet UI that cannot tell them apart is a fleet UI that will offer to
// play back the footage from a door sensor.
type fleet struct {
	enrollment *fleetnode.EnrollmentManager
	control    *fleetnode.ControlChannelManager
	pairing    fleetnode.IPairingService
	httpsPort  int
}

// buildFleet constructs the node's pairing service and its two channels. They are started by
// the caller, alongside the other background workers.
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
		"myiotsan",
		"", // node name: defaults to the hostname
		appVersion,
		// What this node IS. It travels to the control plane twice: as an advisory hint in the
		// discovery announce (unsigned, so a mixed-version fleet keeps working), and
		// authoritatively in the adopt reply, which is fleet-key-signed and claim-code-gated.
		fleetnode.KindIot,
	)

	enrollment := fleetnode.NewEnrollmentManager(
		pairingService,
		deps.Config.Pairing.MTLSPort,
		time.Duration(deps.Config.Pairing.RenewBeforeHours)*time.Hour,
		func(format string, args ...any) { deps.Logger.Infof("myiotsan.pairing", format, args...) },
	)

	// Commands down, events up. The dispatcher re-injects a tunnelled command into THIS app's
	// own /api router, so a control-plane operator reaches exactly the surface a local operator
	// would — gated by this node's own authorization, against this node's own roles. There is no
	// second, weaker path in.
	//
	// That matters more here than it does for a camera. A tunnelled command that reached an
	// unguarded path on a sensor hub could switch a relay, and the node — not the parent — is
	// the thing that must decide who may do that.
	control := fleetnode.NewControlChannelManager(
		pairingService,
		deps.Config.Pairing.ControlPort,
		appVersion,
		sharedapis.NewControlDispatcher(api, deps.AccessRoles),
		func(format string, args ...any) { deps.Logger.Infof("myiotsan.control", format, args...) },
	)
	// Count the events that could NOT be forwarded. Both failure paths inside ForwardEvent
	// used to return silently, so a node whose events were vanishing and a node with
	// nothing to say produced identical telemetry.
	control.SetMetrics(deps.Metrics)

	// Every notification this node raises — a rule alert, a device gone silent, a relay command,
	// a sign-in lockout — also flows up the channel into the control plane's unified feed.
	//
	// This is the line that makes the whole fourth app worth building. Once myseliasan receives
	// events from BOTH camera nodes and sensor nodes, it can correlate across them: motion on a
	// camera AND a door contact opening AND no badge swipe. Neither node can see that on its own.
	notificationService.Register(fleetnode.NewControlEventSink(control))

	return fleet{enrollment: enrollment, control: control, pairing: pairingService, httpsPort: httpsPort}
}

// start runs the node's discovery responder and its two channels until ctx is cancelled.
func (f fleet) start(ctx context.Context, deps apphost.Dependencies) {
	// Answers authenticated discovery probes while the node is unpaired and a fleet key is set,
	// then goes SILENT once adopted. A node that keeps announcing itself after adoption is a
	// node advertising itself to whoever else is listening.
	responder := pairing.NewResponder(pairing.ResponderConfig{
		FleetKey:      func() []byte { k, _ := f.pairing.FleetKey(ctx); return k },
		Discoverable:  func() bool { return f.pairing.Discoverable(ctx) },
		AnnounceInfo:  func() pairing.AnnounceInfo { return f.pairing.AnnounceInfo(ctx, f.httpsPort) },
		MulticastAddr: deps.Config.Pairing.MulticastAddr,
		ReplayWindow:  time.Duration(deps.Config.Pairing.ReplayWindowSeconds) * time.Second,
		Logf:          func(format string, args ...any) { deps.Logger.Infof("myiotsan.pairing", format, args...) },
	})
	safego.Supervise(ctx, "myiotsan.pairing.responder", func(ctx context.Context) {
		if err := responder.Run(ctx); err != nil && ctx.Err() == nil {
			deps.Logger.Warnf("myiotsan.pairing", "discovery responder stopped: %v", err)
		}
	})

	// Supervised, not bare `go`: a panic in either of these would otherwise be silent, and the
	// node would simply stop enrolling or stop answering the parent with nothing to say why.
	safego.Supervise(ctx, "myiotsan.enrollment", func(ctx context.Context) { f.enrollment.Run(ctx) })
	safego.Supervise(ctx, "myiotsan.control", func(ctx context.Context) { f.control.Run(ctx) })
}
