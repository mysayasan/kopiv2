package config

import (
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
	"github.com/mysayasan/kopiv2/infra/login"
)

// App config
type AppConfigModel struct {
	Login  *login.OAuthProvidersConfigModel `json:"login"`
	Server struct {
		Hostnames    []string `json:"hostnames"`
		Ports        []int    `json:"ports"`
		TLSPorts     []int    `json:"tlsPorts"`
		NonTLSPorts  []int    `json:"nonTlsPorts"`
		EnableTLS    *bool    `json:"enableTls"`
		EnableNonTLS *bool    `json:"enableNonTls"`
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
	} `json:"sso"`
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

type RateLimitTierConfigModel struct {
	Enabled       bool `json:"enabled"`
	Requests      int  `json:"requests"`
	WindowSeconds int  `json:"windowSeconds"`
}
