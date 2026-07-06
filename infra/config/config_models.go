package config

import (
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
	"github.com/mysayasan/kopiv2/infra/login"
)

// App config
type AppConfigModel struct {
	Login  *login.OAuthProvidersConfigModel `json:"login"`
	Server struct {
		Hostnames                []string `json:"hostnames"`
		Ports                    []int    `json:"ports"`
		TLSPorts                 []int    `json:"tlsPorts"`
		NonTLSPorts              []int    `json:"nonTlsPorts"`
		EnableTLS                *bool    `json:"enableTls"`
		EnableNonTLS             *bool    `json:"enableNonTls"`
		ReadHeaderTimeoutSeconds *int     `json:"readHeaderTimeoutSeconds"`
		ReadTimeoutSeconds       *int     `json:"readTimeoutSeconds"`
		WriteTimeoutSeconds      *int     `json:"writeTimeoutSeconds"`
		IdleTimeoutSeconds       *int     `json:"idleTimeoutSeconds"`
	} `json:"server"`
	Bootstrap struct {
		Enabled            bool     `json:"enabled"`
		AutoCreateDatabase bool     `json:"autoCreateDatabase"`
		AutoCreateSchema   bool     `json:"autoCreateSchema"`
		AutoMigrate        bool     `json:"autoMigrate"`
		AutoSeed           bool     `json:"autoSeed"`
		AllowReset         bool     `json:"allowReset"`
		SetupPath          string   `json:"setupPath"`
		SeedStatements     []string `json:"seedStatements"`
	} `json:"bootstrap"`
	Jwt struct {
		Secret string `json:"secret" validate:"required"`
	} `json:"jwt"`
	SSO struct {
		Issuer                string `json:"issuer"`
		Audience              string `json:"audience"`
		SessionTTLSeconds     int    `json:"sessionTtlSeconds"`
		PolicyCacheTTLSeconds int    `json:"policyCacheTtlSeconds"`
		InternalToken         string `json:"internalToken"`
		ProviderBaseURL       string `json:"providerBaseUrl"`
		CACertPath            string `json:"caCertPath"`
		ClientID              string `json:"clientId"`
		ClientSecret          string `json:"clientSecret"`
		RedirectBaseURL       string `json:"redirectBaseUrl"`
		RedirectPath          string `json:"redirectPath"`
		AuthCodeTTLSeconds    int    `json:"authCodeTtlSeconds"`
		AccessTokenTTLSeconds int    `json:"accessTokenTtlSeconds"`
	} `json:"sso"`
	LocalAuth struct {
		Enabled  bool   `json:"enabled"`
		Username string `json:"username"`
		Password string `json:"password"`
	} `json:"localAuth"`
	// LoginSecurity throttles failed sign-in attempts to blunt brute-force /
	// credential-stuffing against the standalone local-user login. Failures are
	// tracked per source IP; once MaxAttempts within WindowSeconds is hit, that IP
	// is locked, with the lockout doubling on each repeat up to LockoutMaxSeconds
	// (escalating backoff). FailedDelayMs adds a small constant delay to every
	// failed attempt to slow online guessing.
	LoginSecurity struct {
		Enabled           bool `json:"enabled"`
		MaxAttempts       int  `json:"maxAttempts"`
		WindowSeconds     int  `json:"windowSeconds"`
		LockoutSeconds    int  `json:"lockoutSeconds"`
		LockoutMaxSeconds int  `json:"lockoutMaxSeconds"`
		FailedDelayMs     int  `json:"failedDelayMs"`
		NotifyOnLockout   bool `json:"notifyOnLockout"`
	} `json:"loginSecurity"`
	Camera struct {
		FFmpegPath string `json:"ffmpegPath"`
	} `json:"camera"`
	Decoder struct {
		// BrowseRoots are extra directories the server-side file picker (used to
		// choose the ffmpeg binary in Settings → Runtime) may browse, on top of the
		// built-in defaults (app dir + bin/, user home, common install locations).
		// Use this for site-specific install paths, e.g. "/data/ffmpeg".
		BrowseRoots []string `json:"browseRoots"`
		MJPEG       struct {
			FFmpegPath string `json:"ffmpegPath"`
			Quality    int    `json:"quality"`
			Threads    int    `json:"threads"`
		} `json:"mjpeg"`
		FFmpeg struct {
			RTSPTransport   string `json:"rtspTransport"`
			HWAccel         string `json:"hwaccel"`
			HWAccelDevice   string `json:"hwaccelDevice"`
			InitHWDevice    string `json:"initHwDevice"`
			VideoDecoder    string `json:"videoDecoder"`
			ProbeSize       int    `json:"probeSize"`
			AnalyzeDuration int    `json:"analyzeDuration"`
			LowDelay        *bool  `json:"lowDelay"`
			NoBuffer        *bool  `json:"noBuffer"`
		} `json:"ffmpeg"`
	} `json:"decoder"`
	Stream StreamConfigModel `json:"stream"`
	Vision VisionConfigModel `json:"vision"`
	Health HealthConfigModel `json:"health"`
	// Pairing configures LAN discovery + single-parent adoption between a
	// mymatasan node and a myseliasan control plane. When enabled (default), an
	// unpaired node answers authenticated discovery probes on the multicast group
	// and goes silent once adopted.
	Pairing PairingConfigModel `json:"pairing"`
	// NodeStream (control-plane only) configures the parent's WebRTC re-broadcast of
	// node camera streams to browsers. Empty = host candidates only (same-LAN/local
	// dev). Set publicIps/udpPort for a remote parent with a reachable IP, and/or
	// iceServers (STUN/TURN) when the parent is behind NAT.
	NodeStream   NodeStreamConfigModel   `json:"nodeStream"`
	Notification NotificationConfigModel `json:"notification"`
	Recording    RecordingConfigModel    `json:"recording"`
	// Security configures encryption-at-rest. When enabled (default), recordings,
	// snapshots, alert images, and training/uploaded files are encrypted on disk with
	// a master key, so a factory reset can crypto-erase them by destroying the key.
	Security struct {
		EncryptAtRest *bool  `json:"encryptAtRest"`
		KeyPath       string `json:"keyPath"`
		// KeyProtector selects how the on-disk master key is protected:
		//   "" / "file"          plaintext key file (default; backward compatible)
		//   "auto"               platform default: DPAPI on Windows, systemd-creds on
		//                        a systemd Linux host, else file
		//   "dpapi"              Windows DPAPI, machine-scoped (host-bound)
		//   "systemd-creds"      Linux systemd-creds, TPM2-backed when present (host-bound)
		//   "passphrase"         Argon2id-derived KEK from Passphrase*; portable across
		//                        hosts — the right choice for Docker and the recovery
		//                        escrow for the host-bound protectors
		// Switching protectors re-wraps the same key, so existing encrypted data stays
		// readable. Host-bound protectors cannot be unwrapped on another machine.
		KeyProtector string `json:"keyProtector"`
		// Passphrase sources the KEK for the passphrase protector. Prefer PassphraseFile
		// (a mounted Docker secret) or PassphraseEnv over inlining it here. Resolution
		// order: Passphrase, PassphraseFile, PassphraseEnv, then $ATREST_PASSPHRASE.
		Passphrase     string `json:"passphrase"`
		PassphraseFile string `json:"passphraseFile"`
		PassphraseEnv  string `json:"passphraseEnv"`
		// RecoveryPath is where a disaster-recovery escrow (exported from the UI) is looked
		// for on first start. When no key exists yet but this file does, the app restores
		// the master key from it using the configured passphrase and migrates to the
		// configured protector. Defaults to recovery.atrestkey beside the key file.
		RecoveryPath string `json:"recoveryPath"`
	} `json:"security"`
	FileStorage struct {
		Path    string `json:"path" validate:"required"`
		Cleanup struct {
			Enabled          bool `json:"enabled"`
			FrequencySeconds int  `json:"frequencySeconds"`
			BatchSize        int  `json:"batchSize"`
		} `json:"cleanup"`
	} `json:"fileStorage" validate:"required"`
	Cache struct {
		Provider   string `json:"provider"`
		TTLSeconds int    `json:"ttlSeconds"`
		KeyPrefix  string `json:"keyPrefix"`
		Redis      struct {
			Address            string `json:"address"`
			Password           string `json:"password"`
			DB                 int    `json:"db"`
			UseTLS             bool   `json:"useTls"`
			ConnectTimeoutMs   int    `json:"connectTimeoutMs"`
			OperationTimeoutMs int    `json:"operationTimeoutMs"`
		} `json:"redis"`
	} `json:"cache"`
	RateLimit struct {
		Enabled                 bool                     `json:"enabled"`
		EndpointCacheTTLSeconds int                      `json:"endpointCacheTtlSeconds"`
		DefaultWindowSeconds    int                      `json:"defaultWindowSeconds"`
		DevOnly                 RateLimitTierConfigModel `json:"devOnly"`
		AuthOnly                RateLimitTierConfigModel `json:"authOnly"`
		Public                  RateLimitTierConfigModel `json:"public"`
	} `json:"rateLimit"`
	Transaction struct {
		LockProvider              string `json:"lockProvider"`
		LockWaitTimeoutMs         int    `json:"lockWaitTimeoutMs"`
		LockLeaseMs               int    `json:"lockLeaseMs"`
		OperationTimeoutMs        int    `json:"operationTimeoutMs"`
		StuckTimeoutMs            int    `json:"stuckTimeoutMs"`
		JobWorkerEnabled          bool   `json:"jobWorkerEnabled"`
		JobWorkerFrequencySeconds int    `json:"jobWorkerFrequencySeconds"`
		MaxAttempts               int    `json:"maxAttempts"`
	} `json:"transaction"`
	Logging struct {
		Enabled      bool   `json:"enabled"`
		Path         string `json:"path"`
		MaxLineBytes int    `json:"maxLineBytes"`
		Cleanup      struct {
			Enabled          bool `json:"enabled"`
			MaxRetentionDays int  `json:"maxRetentionDays"`
			FrequencyMinutes int  `json:"frequencyMinutes"`
		} `json:"cleanup"`
	} `json:"logging"`
	ApiLog struct {
		Cleanup struct {
			Enabled          bool `json:"enabled"`
			MaxRetentionDays int  `json:"maxRetentionDays"`
			FrequencyMinutes int  `json:"frequencyMinutes"`
		} `json:"cleanup"`
	} `json:"apiLog"`
	Telemetry struct {
		Enabled    bool `json:"enabled"`
		Prometheus struct {
			Enabled                bool   `json:"enabled"`
			MetricsPath            string `json:"metricsPath"`
			ApiDurationThresholdMs int64  `json:"apiDurationThresholdMs"`
		} `json:"prometheus"`
	} `json:"telemetry"`
	AllowOrigin string `json:"allowOrigins" validate:"required"`
	Tls         struct {
		CertPath string `json:"certPath" validate:"required"`
		KeyPath  string `json:"keyPath" validate:"required"`
	} `json:"tls"`
	Db dbsql.DbConfigModel `json:"db"`
}

// PairingConfigModel holds discovery + adoption settings shared by the node
// responder (mymatasan) and the control-plane prober (myseliasan).
type PairingConfigModel struct {
	// Enabled turns the node-side discovery responder on/off. Defaults to true.
	Enabled *bool `json:"enabled"`
	// MulticastAddr is the IPv4 group+port for discovery. Empty = package default.
	MulticastAddr string `json:"multicastAddr"`
	// ReplayWindowSeconds bounds probe/announce freshness. 0 = package default.
	ReplayWindowSeconds int `json:"replayWindowSeconds"`
	// MTLSPort is the node's mutual-TLS management listener port (release, heartbeat)
	// that comes up once a node is adopted and enrolled. 0 = default (49532).
	MTLSPort int `json:"mtlsPort"`
	// CertTTLHours is the lifetime of an issued node certificate. 0 = default (168 / 7d).
	CertTTLHours int `json:"certTtlHours"`
	// RenewBeforeHours makes the node re-enroll when its cert is within this many
	// hours of expiry. 0 = default (48).
	RenewBeforeHours int `json:"renewBeforeHours"`
	// HeartbeatIntervalSeconds is how often the control plane probes each adopted
	// node over mTLS to reconcile liveness/self-drop. 0 = default (60).
	HeartbeatIntervalSeconds int `json:"heartbeatIntervalSeconds"`
	// ControlPort is the parent's node-dialed control-channel listener port (the
	// persistent bi-directional command/event channel). The node derives the parent
	// host from its stored ParentBaseURL and dials this shared port. 0 = default
	// (49533). Distinct from MTLSPort (the node's own management listener).
	ControlPort int `json:"controlPort"`
	// MediaPort is the node-dialed media-channel listener port (the dedicated RTP
	// relay carrying camera video from node to control plane for WebRTC re-broadcast).
	// Separate from ControlPort so high-rate media never competes with control traffic.
	// 0 = default (49534). The node derives the parent host from its stored
	// ParentBaseURL (same as ControlPort) and dials this port.
	MediaPort int `json:"mediaPort"`
	// ParentBaseURL (parent-side only) overrides the base URL recorded on each adopted
	// node for callbacks (enroll / release / self-drop) AND as the host the node dials
	// for the control channel. The control plane otherwise advertises sso.redirectBaseUrl,
	// which is correct only when the node and parent share a host; for a node on a
	// separate machine this MUST be the parent's LAN-reachable URL (e.g.
	// https://192.168.1.10:3002) — never localhost. Empty = fall back to
	// sso.redirectBaseUrl.
	ParentBaseURL string `json:"parentBaseUrl"`
}

// NodeStreamConfigModel (control-plane only) configures how the parent re-broadcasts
// relayed node camera RTP to browsers over WebRTC across networks.
type NodeStreamConfigModel struct {
	// PublicIPs are the parent's externally reachable IPs, advertised as host
	// candidates (NAT 1:1) so a browser on another network reaches the parent directly.
	PublicIPs []string `json:"publicIps"`
	// UDPPort, when >0, binds a single shared WebRTC UDP port for all browser peers
	// (one firewall rule). 0 = pion's default ephemeral ports.
	UDPPort int `json:"udpPort"`
	// ICEServers are STUN/TURN servers offered to the browser; a TURN server lets the
	// browser↔parent media leg relay when the parent is itself behind NAT.
	ICEServers []WebRTCICEServerModel `json:"iceServers"`
}

type StreamConfigModel struct {
	WebRTC        WebRTCConfigModel        `json:"webrtc"`
	MJPEGFallback MJPEGFallbackConfigModel `json:"mjpegFallback"`
}

type WebRTCConfigModel struct {
	Enabled    *bool                  `json:"enabled"`
	ICEServers []WebRTCICEServerModel `json:"iceServers"`
}

type WebRTCICEServerModel struct {
	URLs       []string `json:"urls"`
	Username   string   `json:"username"`
	Credential string   `json:"credential"`
}

type MJPEGFallbackConfigModel struct {
	Enabled *bool `json:"enabled"`
}

// NotificationConfigModel holds startup defaults for the unified notification
// feed. These seed the runtime-editable notification settings persisted in the
// database (the UI edits that persisted copy, like decoder/vision settings).
type NotificationConfigModel struct {
	Webhook         NotificationWebhookConfigModel  `json:"webhook"`
	Telegram        NotificationTelegramConfigModel `json:"telegram"`
	SSEClientBuffer int                             `json:"sseClientBuffer"`
	// RetentionDays purges notifications older than this many days (0 disables
	// the periodic purge). Defaults applied by the app when unset.
	RetentionDays int `json:"retentionDays"`
	// PurgeIntervalHours is how often the periodic purge runs (defaults to 6h).
	PurgeIntervalHours int `json:"purgeIntervalHours"`
	// PurgeReadOnly keeps unread notifications regardless of age when true.
	PurgeReadOnly bool `json:"purgeReadOnly"`
}

type NotificationWebhookConfigModel struct {
	Enabled     *bool             `json:"enabled"`
	URL         string            `json:"url"`
	MinSeverity string            `json:"minSeverity"`
	QueueSize   int               `json:"queueSize"`
	Headers     map[string]string `json:"headers"`
}

type NotificationTelegramConfigModel struct {
	Enabled     *bool  `json:"enabled"`
	BotToken    string `json:"botToken"`
	ChatId      string `json:"chatId"`
	MinSeverity string `json:"minSeverity"`
	QueueSize   int    `json:"queueSize"`
}

// HealthConfigModel holds startup settings for the camera health monitor, which
// probes camera reachability and raises online/offline notifications.
type HealthConfigModel struct {
	Enabled *bool `json:"enabled"`
	// IntervalMs is the gap between full reachability sweeps.
	IntervalMs int `json:"intervalMs"`
	// TimeoutMs is the per-probe deadline (TCP dial and RTSP deep-check).
	TimeoutMs int `json:"timeoutMs"`
	// FailureThreshold is the consecutive failed probes before declaring offline.
	FailureThreshold int `json:"failureThreshold"`
	// RecoveryThreshold is the consecutive successful probes before declaring online.
	RecoveryThreshold int `json:"recoveryThreshold"`
}

// RecordingConfigModel holds global NVR recording options that aren't per-camera.
type RecordingConfigModel struct {
	// Shred securely overwrites recorded segments before deleting them, so footage
	// can't be trivially recovered from disk. Enabled by default (see resolution in
	// the app wiring); set Enabled to false for plain, faster deletes.
	Shred struct {
		Enabled *bool `json:"enabled"`
		Passes  int   `json:"passes"`
	} `json:"shred"`
	// Storage controls the on-disk video codec for finalized segments. Default
	// (Codec "" / "copy") stores the camera's native codec with no re-encode, so
	// existing installs are unchanged. "h264"/"hevc" re-encode each segment once at
	// remux time on the GPU to shrink it; live capture and event clips always stay
	// stream-copy.
	Storage struct {
		// Codec: "copy" (default), "h264", or "hevc".
		Codec string `json:"codec"`
		// Quality is the NVENC constant-quality (CQ) target when re-encoding
		// (lower = better/larger; ~23-28 typical). 0 = default.
		Quality int `json:"quality"`
		// MaxConcurrentEncodes caps simultaneous NVENC sessions shared by remux-time
		// re-encoding and playback transcode, matching the GPU's session limit. 0 =
		// default.
		MaxConcurrentEncodes int `json:"maxConcurrentEncodes"`
		// FallbackToCopy stores a segment as plain stream-copy when the GPU re-encode
		// can't run (no usable NVENC encoder, or a runtime encode failure) instead of
		// dropping it. Pointer so an omitted value defaults to enabled; ignored in copy
		// mode.
		FallbackToCopy *bool `json:"fallbackToCopy"`
	} `json:"storage"`
}

type VisionConfigModel struct {
	Enabled                   *bool `json:"enabled"`
	IntervalMs                int   `json:"intervalMs"`
	CaptureTimeoutMs          int   `json:"captureTimeoutMs"`
	DiagnosticCooldownSeconds int   `json:"diagnosticCooldownSeconds"`
	// PersistSampledDiagnostics writes a "sampled" diagnostic alert (frame
	// captured; nothing detected) to the alert log on the diagnostic cooldown.
	// Off by default: it is a noisy heartbeat that bloats the alert_event table,
	// and capture/detect FAILURES are still logged regardless. Turn on only to
	// confirm the monitor is alive and sampling while troubleshooting.
	PersistSampledDiagnostics bool `json:"persistSampledDiagnostics"`
	// Alert-log retention. The background purge runs every AlertPurgeIntervalHours
	// (default 6). DiagnosticRetentionDays deletes Vision-monitor diagnostics older
	// than N days (default 3 — keeps the noisy heartbeat/failure rows from piling
	// up). AlertRetentionDays deletes ALL alert events (real detections included)
	// older than N days; 0 disables it so real detections are kept indefinitely.
	DiagnosticRetentionDays int                       `json:"diagnosticRetentionDays"`
	AlertRetentionDays      int                       `json:"alertRetentionDays"`
	AlertPurgeIntervalHours int                       `json:"alertPurgeIntervalHours"`
	SnapshotDir             string                    `json:"snapshotDir"`
	Detector                VisionDetectorConfigModel `json:"detector"`
	Training                VisionTrainingConfigModel `json:"training"`
}

// VisionTrainingConfigModel configures the custom-model training subsystem
// (datasets, labeled images, trained models). DataDir is the on-disk root for
// dataset images, exported YOLO datasets, and trained model weights.
type VisionTrainingConfigModel struct {
	DataDir string `json:"dataDir"`
}

type VisionDetectorConfigModel struct {
	Mode                string              `json:"mode"`
	Command             string              `json:"command"`
	Args                []string            `json:"args"`
	TimeoutMs           int                 `json:"timeoutMs"`
	UseMotionFallback   *bool               `json:"useMotionFallback"`
	UseMotionIntrusion  *bool               `json:"useMotionIntrusion"`
	MinObjectConfidence float64             `json:"minObjectConfidence"`
	ClassMap            map[string][]string `json:"classMap"`
}

type RateLimitTierConfigModel struct {
	Enabled       bool `json:"enabled"`
	Requests      int  `json:"requests"`
	WindowSeconds int  `json:"windowSeconds"`
}
