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
	// Kerberos configures SPNEGO single sign-on (myidsan): a domain-joined machine's
	// browser presents a service ticket for ServicePrincipal (e.g.
	// "HTTP/myidsan.corp.local") verified against the exported keytab — no password
	// prompt. File-based on purpose: the keytab is an ops-provisioned artifact, and
	// this config decides whether the login endpoint exists at all.
	Kerberos struct {
		Enabled    bool   `json:"enabled"`
		KeytabPath string `json:"keytabPath"`
		// ServicePrincipal must match the SPN the keytab was exported for.
		ServicePrincipal string `json:"servicePrincipal"`
		// OnlyRealms optionally allow-lists accepted realms (case-insensitive);
		// empty accepts any realm the keytab can decrypt tickets for.
		OnlyRealms []string `json:"onlyRealms"`
		// DisplayLabel names the SSO button on the login pages
		// (default "Windows (SSO)").
		DisplayLabel string `json:"displayLabel"`
	} `json:"kerberos"`
	// Smtp is the OPTIONAL internal mail relay for self-service account recovery
	// (myidsan's forgot-password email link). Disabled by default so an air-gapped
	// install never reaches for a network — the operator reset queue covers recovery
	// either way. When enabled, it must point at an internal relay only.
	Smtp struct {
		Enabled bool   `json:"enabled"`
		Host    string `json:"host"`
		Port    int    `json:"port"`
		// From is the sender address on recovery mail (falls back to Username).
		From string `json:"from"`
		// Username/Password authenticate to the relay; when set, UseStartTls must be
		// on (the sender refuses to transmit credentials over a cleartext link).
		Username    string `json:"username"`
		Password    string `json:"password"`
		UseStartTls bool   `json:"useStartTls"`
	} `json:"smtp"`
	// LoginSecurity throttles failed sign-in attempts to blunt brute-force /
	// credential-stuffing against the standalone local-user login. Failures are
	// tracked per source IP; once MaxAttempts within WindowSeconds is hit, that IP
	// is locked, with the lockout doubling on each repeat up to LockoutMaxSeconds
	// (escalating backoff). FailedDelayMs adds a small constant delay to every
	// failed attempt to slow online guessing.
	//
	// Read this through Effective(), never field-by-field — an omitted block must
	// resolve to ON. See LoginSecurityConfigModel.
	LoginSecurity LoginSecurityConfigModel `json:"loginSecurity"`
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
		Enabled                 bool `json:"enabled"`
		EndpointCacheTTLSeconds int  `json:"endpointCacheTtlSeconds"`
		DefaultWindowSeconds    int  `json:"defaultWindowSeconds"`
		// TrustedProxies lists IPs/CIDRs of reverse proxies allowed to set the
		// client's real address via X-Forwarded-For / X-Real-IP. When empty (the
		// default) those headers are ignored and the direct peer address is used,
		// so a directly-exposed instance can't be rate-limit/lockout-bypassed by
		// spoofing the header. Set this to your proxy's address(es) when deploying
		// behind one so per-client rate limiting works.
		TrustedProxies []string                 `json:"trustedProxies"`
		DevOnly        RateLimitTierConfigModel `json:"devOnly"`
		AuthOnly       RateLimitTierConfigModel `json:"authOnly"`
		Public         RateLimitTierConfigModel `json:"public"`
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
	// SecurityHeaders configures the hardening response headers applied by
	// middlewares.SecurityHeaders. Every field is optional: when the block (or a
	// field) is omitted, hardened defaults apply (nosniff, X-Frame-Options
	// SAMEORIGIN, Referrer-Policy, HSTS on TLS, and the Server header stripped).
	// ContentSecurityPolicy is the exception — it is only sent when set, so an
	// app opts in after verifying its own front-end against the policy.
	SecurityHeaders struct {
		Disabled              bool    `json:"disabled"`
		ContentSecurityPolicy string  `json:"contentSecurityPolicy"`
		FrameOptions          *string `json:"frameOptions"`
		ReferrerPolicy        *string `json:"referrerPolicy"`
		ContentTypeOptions    *bool   `json:"contentTypeOptions"`
		ServerHeader          *string `json:"serverHeader"`
		Hsts                  struct {
			Enabled           *bool `json:"enabled"`
			MaxAgeSeconds     int   `json:"maxAgeSeconds"`
			IncludeSubDomains *bool `json:"includeSubDomains"`
			Preload           bool  `json:"preload"`
		} `json:"hsts"`
	} `json:"securityHeaders"`
	Tls struct {
		CertPath string `json:"certPath" validate:"required"`
		KeyPath  string `json:"keyPath" validate:"required"`
	} `json:"tls"`
	Db dbsql.DbConfigModel `json:"db"`

	// raw is the config document this model was decoded from, retained so an app can
	// decode its OWN blocks (mymatasan's camera/vision config) out of the same bytes. It
	// is unexported and untagged: it is not part of the config schema, and marshalling the
	// model back out must not emit it. Read it via Raw().
	raw []byte
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
	// CertTTLHours is the lifetime of an issued node certificate. 0 = default (2160 / 90d).
	// Node renewal is operator-gated per node (auto-renew toggle in the control plane), so
	// the lifetime is long enough that an un-renewed node stays in the fleet for a while.
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

type WebRTCICEServerModel struct {
	URLs       []string `json:"urls"`
	Username   string   `json:"username"`
	Credential string   `json:"credential"`
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

type RateLimitTierConfigModel struct {
	Enabled       bool `json:"enabled"`
	Requests      int  `json:"requests"`
	WindowSeconds int  `json:"windowSeconds"`
}

// LoginSecurityConfigModel is the failed-login lockout block.
//
// Enabled is a pointer on purpose: an ABSENT block must be distinguishable from an
// explicit "enabled": false. It used to be a plain bool, so any config.json without a
// loginSecurity block silently resolved to Enabled=false and the guard became a no-op —
// which is what shipped in deploy/dist/myseliasan-config.json and in the myidsan and
// myseliasan dev configs. An identity provider with no brute-force protection is not a
// defensible default, so an absent block now means ON with the tuned defaults below.
// Turning the lockout off is still possible; it just takes a deliberate "enabled": false.
type LoginSecurityConfigModel struct {
	Enabled           *bool `json:"enabled"`
	MaxAttempts       int   `json:"maxAttempts"`
	WindowSeconds     int   `json:"windowSeconds"`
	LockoutSeconds    int   `json:"lockoutSeconds"`
	LockoutMaxSeconds int   `json:"lockoutMaxSeconds"`
	FailedDelayMs     int   `json:"failedDelayMs"`
	NotifyOnLockout   bool  `json:"notifyOnLockout"`
}

// EffectiveLoginSecurity is LoginSecurityConfigModel with every unset field resolved.
type EffectiveLoginSecurity struct {
	Enabled           bool
	MaxAttempts       int
	WindowSeconds     int
	LockoutSeconds    int
	LockoutMaxSeconds int
	FailedDelayMs     int
	NotifyOnLockout   bool
}

// Default lockout tuning, matching what deploy/dist/myidsan-config.json already ships:
// eight attempts in five minutes, then a 60s lockout doubling to at most an hour, plus a
// small constant delay on every failure to slow online guessing.
const (
	defaultLoginMaxAttempts       = 8
	defaultLoginWindowSeconds     = 300
	defaultLoginLockoutSeconds    = 60
	defaultLoginLockoutMaxSeconds = 3600
	defaultLoginFailedDelayMs     = 400
)

// Effective resolves the block: an absent Enabled means on, and any tunable left at zero
// is filled from the defaults above rather than taking Go's zero value — a zero
// MaxAttempts would otherwise lock a user out on their first failed attempt.
func (l LoginSecurityConfigModel) Effective() EffectiveLoginSecurity {
	eff := EffectiveLoginSecurity{
		Enabled:           l.Enabled == nil || *l.Enabled,
		MaxAttempts:       l.MaxAttempts,
		WindowSeconds:     l.WindowSeconds,
		LockoutSeconds:    l.LockoutSeconds,
		LockoutMaxSeconds: l.LockoutMaxSeconds,
		FailedDelayMs:     l.FailedDelayMs,
		NotifyOnLockout:   l.NotifyOnLockout,
	}
	if eff.MaxAttempts <= 0 {
		eff.MaxAttempts = defaultLoginMaxAttempts
	}
	if eff.WindowSeconds <= 0 {
		eff.WindowSeconds = defaultLoginWindowSeconds
	}
	if eff.LockoutSeconds <= 0 {
		eff.LockoutSeconds = defaultLoginLockoutSeconds
	}
	if eff.LockoutMaxSeconds <= 0 {
		eff.LockoutMaxSeconds = defaultLoginLockoutMaxSeconds
	}
	if eff.FailedDelayMs < 0 {
		eff.FailedDelayMs = 0
	} else if eff.FailedDelayMs == 0 {
		eff.FailedDelayMs = defaultLoginFailedDelayMs
	}
	return eff
}
