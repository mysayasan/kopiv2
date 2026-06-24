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
	Stream       StreamConfigModel       `json:"stream"`
	Vision       VisionConfigModel       `json:"vision"`
	Health       HealthConfigModel       `json:"health"`
	Notification NotificationConfigModel `json:"notification"`
	Recording    RecordingConfigModel    `json:"recording"`
	// Security configures encryption-at-rest. When enabled (default), recordings,
	// snapshots, alert images, and training/uploaded files are encrypted on disk with
	// a master key, so a factory reset can crypto-erase them by destroying the key.
	Security struct {
		EncryptAtRest *bool  `json:"encryptAtRest"`
		KeyPath       string `json:"keyPath"`
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
	} `json:"storage"`
}

type VisionConfigModel struct {
	Enabled                   *bool                     `json:"enabled"`
	IntervalMs                int                       `json:"intervalMs"`
	CaptureTimeoutMs          int                       `json:"captureTimeoutMs"`
	DiagnosticCooldownSeconds int                       `json:"diagnosticCooldownSeconds"`
	// PersistSampledDiagnostics writes a "sampled" diagnostic alert (frame
	// captured; nothing detected) to the alert log on the diagnostic cooldown.
	// Off by default: it is a noisy heartbeat that bloats the alert_event table,
	// and capture/detect FAILURES are still logged regardless. Turn on only to
	// confirm the monitor is alive and sampling while troubleshooting.
	PersistSampledDiagnostics bool                      `json:"persistSampledDiagnostics"`
	// Alert-log retention. The background purge runs every AlertPurgeIntervalHours
	// (default 6). DiagnosticRetentionDays deletes Vision-monitor diagnostics older
	// than N days (default 3 — keeps the noisy heartbeat/failure rows from piling
	// up). AlertRetentionDays deletes ALL alert events (real detections included)
	// older than N days; 0 disables it so real detections are kept indefinitely.
	DiagnosticRetentionDays   int                       `json:"diagnosticRetentionDays"`
	AlertRetentionDays        int                       `json:"alertRetentionDays"`
	AlertPurgeIntervalHours   int                       `json:"alertPurgeIntervalHours"`
	SnapshotDir               string                    `json:"snapshotDir"`
	Detector                  VisionDetectorConfigModel `json:"detector"`
	Training                  VisionTrainingConfigModel `json:"training"`
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
