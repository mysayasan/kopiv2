package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/myseliasan/apis"
	appentities "github.com/mysayasan/kopiv2/apps/myseliasan/entities"
	"github.com/mysayasan/kopiv2/apps/myseliasan/services"
	sharedentities "github.com/mysayasan/kopiv2/domain/entities"
	apiaccessenums "github.com/mysayasan/kopiv2/domain/enums/apiaccess"
	"github.com/mysayasan/kopiv2/domain/notification"
	"github.com/mysayasan/kopiv2/infra/apidocs"
	"github.com/mysayasan/kopiv2/infra/apphost"
	"github.com/mysayasan/kopiv2/infra/db/bootstrap"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
	"github.com/mysayasan/kopiv2/infra/mediarelay"
	"github.com/mysayasan/kopiv2/infra/stream"
	"github.com/mysayasan/kopiv2/infra/versioning"
)

type module struct{}

func New() apphost.App {
	return &module{}
}

func (m *module) Name() string {
	return "myseliasan"
}

func (m *module) BaseDir() string {
	return filepath.Join("apps", "myseliasan")
}

func (m *module) SharedAPIs() apphost.SharedAPIConfig {
	cfg := apphost.DefaultSharedAPIConfig()
	cfg.ApiLog = false
	cfg.AppRegistry = false
	cfg.ApiEndpoint = false
	cfg.FileStorage = false
	cfg.CacheService = false
	cfg.RuntimeLog = false
	return cfg
}

func (m *module) Entities() []any {
	return []any{
		sharedentities.ApiEndpoint{},
		sharedentities.ApiLog{},
		sharedentities.UserSession{},
		sharedentities.Notification{},
		appentities.ManagedNode{},
		appentities.ControlSetting{},
		appentities.NodeAccessGrant{},
		appentities.ControlUser{},
		sharedentities.AccessRole{},
		sharedentities.AccessRolePermission{},
	}
}

func (m *module) Seeders(seedStatements []string) []bootstrap.Seeder {
	type endpointSeed struct {
		Title       string
		Description string
		Path        string
		AccessTier  apiaccessenums.AccessTier
	}

	endpoints := []endpointSeed{
		{Title: "API Health", Description: "api namespace health", Path: "/api/health", AccessTier: apiaccessenums.Public},
		{Title: "Runtime Version", Description: "runtime version access", Path: "/api/version", AccessTier: apiaccessenums.Public},
		{Title: "Auth", Description: "relying-app auth start, callback, and logout", Path: "/api/auth", AccessTier: apiaccessenums.Public},
		{Title: "Session", Description: "current relying-app session metadata", Path: "/api/session", AccessTier: apiaccessenums.AuthOnly},
		{Title: "Notifications", Description: "control-plane unified feed of node-pushed events", Path: "/api/notifications", AccessTier: apiaccessenums.AuthOnly},
		{Title: "Nodes", Description: "mymatasan node discovery, adoption, and management", Path: "/api/nodes", AccessTier: apiaccessenums.AuthOnly},
		{Title: "Node Access", Description: "per-node read/write access grants (owner-role managed)", Path: "/api/nodes/access", AccessTier: apiaccessenums.AuthOnly},
		{Title: "Node Self-Drop", Description: "node-initiated unpair notice (fleet-key authenticated)", Path: "/api/nodes/self-dropped", AccessTier: apiaccessenums.Public},
	}

	statements := make([]string, 0, len(endpoints)*2)
	for _, endpoint := range endpoints {
		statements = append(statements,
			fmt.Sprintf(`INSERT INTO api_endpoint (title, description, app_code, host, path, access_tier, is_active, created_by, created_at, updated_by, updated_at)
SELECT '%s', '%s', 'myseliasan', '*', '%s', %d, TRUE, 0, 0, 0, 0
WHERE NOT EXISTS (SELECT 1 FROM api_endpoint WHERE app_code = 'myseliasan' AND host = '*' AND path = '%s');`, endpoint.Title, endpoint.Description, endpoint.Path, endpoint.AccessTier, endpoint.Path),
			fmt.Sprintf(`UPDATE api_endpoint SET app_code = 'myseliasan', access_tier = %d WHERE host = '*' AND path = '%s' AND ((access_tier IS NULL OR access_tier <> %d) OR app_code IS NULL OR app_code = '');`, endpoint.AccessTier, endpoint.Path, endpoint.AccessTier),
		)
	}

	seeders := []bootstrap.Seeder{
		bootstrap.NewSQLSeeder("myseliasan-endpoints", statements),
	}
	if len(seedStatements) > 0 {
		seeders = append(seeders, bootstrap.NewSQLSeeder("config", seedStatements))
	}
	return seeders
}

func (m *module) RegisterAppRoutes(api *mux.Router, deps apphost.Dependencies) (apphost.ShutdownFunc, error) {
	// Roles, the permission matrix, and the authorization middleware come from
	// apphost's shared accessrbac core (deps.Access*); apphost already seeded the
	// built-in roles + viewer defaults. myseliasan supplies its own user layer (the
	// resolver) and the stock-superadmin bootstrap. The middleware enforces
	// disabled/must-change + the matrix on myseliasan's OWN endpoints (not the node
	// tunnel, which is axis-2).
	roleService := deps.AccessRoles
	userService := services.NewControlUserService(deps.Db, roleService)
	if err := userService.EnsureStockSuperadmin(context.Background(), deps.Config.LocalAuth.Username, deps.Config.LocalAuth.Password); err != nil {
		return nil, fmt.Errorf("seed stock superadmin: %w", err)
	}
	controlSession := deps.Access
	controlSession.SetResolver(userService)

	apis.NewAuthApi(api, deps.Config, deps.Auth, deps.Cache, userService)
	apis.NewSessionApi(api, *deps.Auth, userService, roleService, deps.AccessPerms)
	apis.NewRbacAdminApi(api, *deps.Auth, controlSession, roleService, userService)

	// Node management: discover, adopt, and release mymatasan nodes over the
	// fleet-key-authenticated pairing protocol. ParentBaseURL is recorded on each
	// node so it can call back (enroll / release / self-drop). The control plane is
	// also the fleet CA that issues node certs for the mTLS management channel.
	parentID := deps.Config.SSO.ClientID
	if parentID == "" {
		parentID = "myseliasan"
	}
	p := deps.Config.Pairing
	mtlsPort := p.MTLSPort
	if mtlsPort <= 0 {
		mtlsPort = 49532
	}
	// ParentBaseURL is the address each node records for callbacks (enroll / release /
	// self-drop) AND the host it dials for the persistent control channel. The default,
	// sso.redirectBaseUrl, is correct only when node and parent share a host; a node on
	// its own machine needs the parent's LAN-reachable URL, so pairing.parentBaseUrl
	// overrides it (must not be localhost in that case).
	parentBaseURL := strings.TrimSpace(p.ParentBaseURL)
	if parentBaseURL == "" {
		parentBaseURL = deps.Config.SSO.RedirectBaseURL
	}
	hbInterval := time.Duration(p.HeartbeatIntervalSeconds) * time.Second
	if hbInterval <= 0 {
		hbInterval = 60 * time.Second
	}
	registry := services.NewNodeRegistry(deps.Db, services.NodeRegistryConfig{
		MulticastAddr:     p.MulticastAddr,
		ParentID:          parentID,
		ParentName:        parentID,
		ParentBaseURL:     parentBaseURL,
		MTLSPort:          mtlsPort,
		CertTTL:           time.Duration(p.CertTTLHours) * time.Hour,
		HeartbeatInterval: hbInterval,
	})
	apis.NewNodesApi(api, *deps.Auth, controlSession, registry)

	bgCtx, stopBackground := context.WithCancel(context.Background())

	// Unified notification feed for the control plane: events nodes push up their
	// control channels (alerts, health, system, going-offline) land here so an
	// operator sees fleet activity in one place. Reuses the shared notification
	// engine (persist + log + live SSE).
	notificationRepo := dbsql.NewGenericRepo[sharedentities.Notification](deps.Db)
	notificationService := notification.NewService(notificationRepo, notification.Options{Logger: deps.Logger})
	apis.NewNotificationApi(api, *deps.Auth, controlSession, notificationService)

	// Control channel server: a dedicated fleet-mTLS listener accepting the
	// persistent, node-dialed bi-directional channel. Connection presence bumps a
	// node online (stronger liveness than the heartbeat poll, which still runs as a
	// fallback). It tunnels parent→node commands and ingests node→parent events.
	onNodeEvent := func(nodeID, kind string, body []byte) {
		ingestNodeEvent(notificationService, nodeID, kind, body)
	}
	controlServer := services.NewControlServer(registry, p.ControlPort, onNodeEvent,
		func(format string, args ...any) { deps.Logger.Infof("myseliasan.control", format, args...) })
	// The persistent node-dialed control channel is the authoritative liveness signal:
	// a node holding a live connection is online even when the parent cannot reach its
	// mTLS port directly. Wire its presence into the heartbeat reconciler so the mTLS
	// poll becomes a fallback that can no longer flap a control-connected node offline.
	registry.SetControlPresence(controlServer.IsConnected)
	go controlServer.Run(bgCtx)

	// Heartbeat reconciliation: every interval, reconcile each adopted node's liveness —
	// control-channel presence first, then the mTLS poll as a fallback — converging the
	// registry after self-drops and bounded by a grace window so brief reconnects don't
	// show offline.
	go func() {
		ticker := time.NewTicker(hbInterval)
		defer ticker.Stop()
		for {
			select {
			case <-bgCtx.Done():
				return
			case <-ticker.C:
				registry.Heartbeat(bgCtx)
			}
		}
	}()

	// Per-node access: a role gets full access to nodes it adopted (owner role) and
	// whatever explicit grants it has elsewhere. Drives the tunnel's viewer/admin
	// decision and gates grant management.
	accessService := services.NewNodeAccessService(deps.Db, roleService)
	apis.NewNodeAccessApi(api, *deps.Auth, accessService, controlSession)

	// Node camera media relay: a dedicated fleet-mTLS listener accepts the node-dialed
	// media channel; per browser WebRTC subscription it asks the node to stream that
	// camera's RTP, then re-broadcasts it to the browser (the browser talks only to
	// myseliasan). The WebRTC engine advertises configured public IPs / a fixed UDP
	// port (and offers STUN/TURN) so the browser↔parent leg works across networks;
	// empty config = host candidates (same-LAN/local dev).
	mediaEngine, engErr := stream.NewWebRTCEngine(deps.Config.NodeStream.PublicIPs, deps.Config.NodeStream.UDPPort)
	if engErr != nil {
		stopBackground()
		return nil, fmt.Errorf("webrtc engine: %w", engErr)
	}
	mediaICE := make([]stream.ICEServer, 0, len(deps.Config.NodeStream.ICEServers))
	for _, s := range deps.Config.NodeStream.ICEServers {
		if len(s.URLs) == 0 {
			continue
		}
		mediaICE = append(mediaICE, stream.ICEServer{URLs: s.URLs, Username: s.Username, Credential: s.Credential})
	}
	mediaHub := services.NewMediaRelayHub(func(format string, args ...any) { deps.Logger.Infof("myseliasan.media", format, args...) })
	mediaPort := p.MediaPort
	if mediaPort <= 0 {
		mediaPort = 49534
	}
	go func() {
		tlsCfg, terr := registry.ParentServerTLS(bgCtx)
		if terr != nil {
			deps.Logger.Warnf("myseliasan.media", "media listener TLS unavailable: %v", terr)
			return
		}
		srv := mediarelay.NewServer(fmt.Sprintf(":%d", mediaPort), tlsCfg, mediaHub.HandleConn,
			func(format string, args ...any) { deps.Logger.Infof("myseliasan.media", format, args...) })
		deps.Logger.Infof("myseliasan.media", "media channel listening on :%d", mediaPort)
		if rerr := srv.Run(bgCtx); rerr != nil {
			deps.Logger.Warnf("myseliasan.media", "media server stopped: %v", rerr)
		}
	}()
	// Registered before the proxy catch-all so the specific media-offer path wins.
	apis.NewNodeMediaApi(api, *deps.Auth, mediaHub, accessService, mediaEngine, mediaICE, controlSession)

	// Reverse command tunnel: /api/nodes/{id}/proxy/<node-path> forwards over the
	// control channel to the node's own API, giving the commander the node's exact
	// capability surface. The operator's per-node grant decides viewer vs admin (no
	// read access → 403). Registered after NewNodesApi/NewNodeAccessApi so their
	// specific routes win; mux falls through to the proxy for /nodes/{id}/proxy/...
	// Range-capable recording playback over the tunnel (chunks each byte range under the
	// control-channel message cap). Registered before the generic proxy so its specific
	// /nodes/{id}/recording-stream/{segId} route wins.
	apis.NewRecordingStreamApi(api, *deps.Auth, controlServer, accessService, controlSession)
	apis.NewNodeProxyApi(api, *deps.Auth, controlServer, accessService, controlSession)

	return func(context.Context) error { stopBackground(); return nil }, nil
}

// ingestNodeEvent maps an event a node pushed up its control channel into the
// control plane's notification feed. "notification" events are re-published as-is
// (re-tagged with the node so the feed shows their origin and the parent assigns a
// fresh id); "going-offline" becomes a system warning.
func ingestNodeEvent(svc *notification.Service, nodeID, kind string, body []byte) {
	switch kind {
	case "notification":
		var n notification.Notification
		if err := json.Unmarshal(body, &n); err != nil {
			return
		}
		n.ID = "" // parent assigns its own id in its own feed
		n.Source = "node:" + nodeID
		if n.Data == nil {
			n.Data = map[string]any{}
		}
		n.Data["nodeId"] = nodeID
		svc.Publish(context.Background(), n)
	case "going-offline":
		svc.Publish(context.Background(), notification.Notification{
			Category: notification.CategorySystem,
			Severity: notification.Warning,
			Title:    "Node going offline",
			Body:     "Node " + nodeID + " reported it is going offline.",
			Source:   "node:" + nodeID,
			Data:     map[string]any{"nodeId": nodeID},
		})
	}
}

func (m *module) RegisterWebRoutes(router *mux.Router, deps apphost.Dependencies) error {
	// Always serve the SPA; the app renders its own login screen (SSO button + local
	// stock-superadmin form) when /api/session/me reports no session. This replaces
	// the old straight-to-SSO redirect so the local bootstrap login is reachable.
	staticIndex := filepath.Join(m.BaseDir(), "static", "index.html")
	serveIndex := func(w http.ResponseWriter, r *http.Request) {
		// index.html references content-hashed chunk files, so it MUST NOT be cached:
		// a stale index.html keeps the browser on an old bundle even after a rebuild
		// (which is exactly what left a fixed request-storm still running client-side).
		// The hashed .js/.css can still be cached immutably — only this entry points to them.
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		http.ServeFile(w, r, staticIndex)
	}
	router.HandleFunc("/", serveIndex).Methods("GET")
	router.HandleFunc("/index.html", serveIndex).Methods("GET")
	return nil
}

func (m *module) APIDocs() apidocs.SpecConfig {
	docVersion := "1.0.0"
	if manifest, err := versioning.LoadDefault(); err == nil {
		if info, err := manifest.InfoForApp(m.Name()); err == nil {
			docVersion = info.AppVersion
		}
	}

	return apidocs.SpecConfig{
		Metadata: apidocs.Metadata{
			Title:       "myseliasan API",
			Version:     docVersion,
			Description: "Control plane app for mymatasan using MyIDSan federated SSO.",
		},
		Endpoints: map[string]apidocs.EndpointDoc{
			"GET /api/auth/start": {
				Summary:     "Start MyIDSan login",
				Description: "Redirects to MyIDSan authorization endpoint.",
				Tags:        []string{"auth"},
			},
			"GET /api/auth/callback": {
				Summary:     "Handle MyIDSan callback",
				Description: "Exchanges authorization code and creates the myseliasan session.",
				Tags:        []string{"auth"},
			},
			"POST /api/auth/logout": {
				Summary:     "Logout",
				Description: "Clears the myseliasan session cookie.",
				Tags:        []string{"auth"},
			},
			"GET /api/session/me": {
				Summary:     "Current session",
				Description: "Returns current authenticated user claims for the dashboard.",
				Tags:        []string{"session"},
			},
		},
	}
}
