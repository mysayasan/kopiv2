package app

import (
	"context"
	"fmt"
	"time"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/mymatasan/apis"
	"github.com/mysayasan/kopiv2/apps/mymatasan/services"
	"github.com/mysayasan/kopiv2/domain/notification"
	"github.com/mysayasan/kopiv2/infra/apphost"
	"github.com/mysayasan/kopiv2/infra/stream"
)

// fleet is the node side of the myseliasan control plane: the three node-dialed channels
// this app maintains once it has been adopted.
//
// All three are DIALED BY THE NODE, never accepted from it. That is the whole security
// posture of the fleet: the control plane never needs a route to the node, so the node can
// sit behind NAT on a customer's network with no inbound ports open.
type fleet struct {
	enrollment *services.EnrollmentManager
	control    *services.ControlChannelManager
	media      *services.MediaChannelManager
	// httpsPort is advertised to the control plane in discovery announces and used as the
	// adoption-call origin.
	httpsPort int
}

// buildFleet constructs the enrollment, control and media channels. They are started later,
// with the other background workers, so they share the monitor lifecycle.
func buildFleet(
	api *mux.Router,
	deps apphost.Dependencies,
	appVersion string,
	pairingService services.IPairingService,
	cameraService services.ICameraService,
	streamManager *stream.Manager,
	notificationService *notification.Service,
) fleet {
	httpsPort := 0
	if len(deps.Config.Server.TLSPorts) > 0 {
		httpsPort = deps.Config.Server.TLSPorts[0]
	}

	// After adoption the node enrolls with the control plane's fleet CA, serves the mutual-TLS
	// management listener (heartbeat/release), and renews its certificate before expiry.
	enrollment := services.NewEnrollmentManager(
		pairingService,
		deps.Config.Pairing.MTLSPort,
		time.Duration(deps.Config.Pairing.RenewBeforeHours)*time.Hour,
		func(format string, args ...any) { deps.Logger.Infof("mymatasan.pairing", format, args...) },
	)

	// Once paired and enrolled, the node dials the control plane's control listener over mTLS
	// and maintains a persistent bi-directional channel: commands down, events up.
	//
	// The dispatcher re-injects tunnelled commands into this app's OWN /api router, so the
	// commander reaches the node's exact API surface and is gated by the node's normal
	// authorization — via the synthetic principal the command carries. There is no second,
	// weaker path into the node.
	control := services.NewControlChannelManager(
		pairingService,
		deps.Config.Pairing.ControlPort,
		appVersion,
		apis.NewControlDispatcher(api, deps.AccessRoles),
		func(format string, args ...any) { deps.Logger.Infof("mymatasan.control", format, args...) },
	)
	// Forward this node's notifications up the channel, so the control plane's unified feed
	// receives them live in addition to local delivery.
	notificationService.Register(services.NewControlEventSink(control))

	// A second node-dialed mTLS connection (separate port) that streams a camera's RTP up to
	// the control plane on request, so myseliasan can re-broadcast full-frame-rate live view
	// over WebRTC. It resolves each camera through the same snapshot-source path browser live
	// view uses, and shares the stream manager's RTSP sessions rather than opening its own.
	mediaResolve := func(ctx context.Context, camID uint64) (stream.Source, error) {
		src, err := cameraService.SnapshotSource(ctx, camID)
		if err != nil {
			return stream.Source{}, err
		}
		return stream.Source{ID: fmt.Sprintf("camera-%d", camID), URI: src.RTSPURI}, nil
	}
	media := services.NewMediaChannelManager(
		pairingService,
		deps.Config.Pairing.MediaPort,
		appVersion,
		streamManager,
		mediaResolve,
		func(format string, args ...any) { deps.Logger.Infof("mymatasan.media", format, args...) },
	)

	return fleet{enrollment: enrollment, control: control, media: media, httpsPort: httpsPort}
}
