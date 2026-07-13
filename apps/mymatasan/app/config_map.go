package app

import (
	"path/filepath"
	"strings"
	"time"

	mmconfig "github.com/mysayasan/kopiv2/apps/mymatasan/config"

	"github.com/mysayasan/kopiv2/apps/mymatasan/apis"
	"github.com/mysayasan/kopiv2/apps/mymatasan/services"
	"github.com/mysayasan/kopiv2/domain/notification"
	"github.com/mysayasan/kopiv2/infra/config"
	applog "github.com/mysayasan/kopiv2/infra/logging"
	"github.com/mysayasan/kopiv2/infra/recording"
	"github.com/mysayasan/kopiv2/infra/stream"
	"github.com/mysayasan/kopiv2/infra/telemetry"
)

// Pure mappers from the config.json model into each service's settings struct.
//
// They are deliberately free of I/O and of any dependency on wiring order, which makes
// them unit-testable and keeps the composition root free of forty lines of struct
// copying per subsystem.
//
// NOTE: most of this file only exists because the shared config.AppConfigModel carries
// mymatasan's blocks (Vision, Recording, Decoder, Stream, Health, Notification) and each
// has to be hand-copied into a parallel service-settings struct. Tier 2 phase C (the
// per-app config seam) collapses that triplication — see docs/MYMATASAN_TIER2_PLAN.md.

// loginGuardConfigFromAppConfig maps the config.json loginSecurity block into the
// failed-login guard config. Numeric tunables left at zero are filled with safe
// defaults inside the guard.
func loginGuardConfigFromAppConfig(cfg *config.AppConfigModel) apis.LoginGuardConfig {
	ls := cfg.LoginSecurity
	return apis.LoginGuardConfig{
		Enabled:     ls.Enabled,
		MaxAttempts: ls.MaxAttempts,
		Window:      time.Duration(ls.WindowSeconds) * time.Second,
		BaseLockout: time.Duration(ls.LockoutSeconds) * time.Second,
		MaxLockout:  time.Duration(ls.LockoutMaxSeconds) * time.Second,
		FailedDelay: time.Duration(ls.FailedDelayMs) * time.Millisecond,
	}
}

// notificationOptionsFromAppConfig builds always-on notification options. The
// outbound delivery channels (webhook, telegram) are applied separately from the
// persisted, runtime-editable notification settings.
func notificationOptionsFromAppConfig(cfg *config.AppConfigModel, logger applog.Logger, metrics telemetry.Metrics) notification.Options {
	return notification.Options{
		Logger:          logger,
		SSEClientBuffer: cfg.Notification.SSEClientBuffer,
		Metrics:         metrics,
	}
}

// notificationSettingsDefaultsFromAppConfig maps the config.json notification
// block into the default runtime-editable notification settings. These seed the
// persisted settings on first run; thereafter the UI-edited copy wins.
func notificationSettingsDefaultsFromAppConfig(cfg *config.AppConfigModel) services.NotificationSettings {
	n := cfg.Notification
	retentionInterval := n.PurgeIntervalHours
	if retentionInterval <= 0 {
		retentionInterval = 6
	}
	return services.NotificationSettings{
		Webhook: services.NotificationWebhookSettings{
			Enabled:     boolValue(n.Webhook.Enabled, false),
			URL:         strings.TrimSpace(n.Webhook.URL),
			MinSeverity: n.Webhook.MinSeverity,
		},
		Telegram: services.NotificationTelegramSettings{
			Enabled:     boolValue(n.Telegram.Enabled, false),
			BotToken:    strings.TrimSpace(n.Telegram.BotToken),
			ChatId:      strings.TrimSpace(n.Telegram.ChatId),
			MinSeverity: n.Telegram.MinSeverity,
		},
		Retention: services.NotificationRetentionSettings{
			Days:          n.RetentionDays,
			OnlyRead:      n.PurgeReadOnly,
			IntervalHours: retentionInterval,
		},
	}
}

func runtimeSettingsFromAppConfig(cfg *mmconfig.Config) services.RuntimeSettings {
	ffmpegPath := cfg.Decoder.MJPEG.FFmpegPath
	if ffmpegPath == "" {
		ffmpegPath = cfg.Camera.FFmpegPath
	}
	result := services.RuntimeSettings{
		Decoder: services.DecoderSettings{
			MJPEG: services.MJPEGDecoderSettings{
				FFmpegPath: ffmpegPath,
				Quality:    cfg.Decoder.MJPEG.Quality,
				Threads:    cfg.Decoder.MJPEG.Threads,
			},
			FFmpeg: services.FFmpegDecoderSettings{
				RTSPTransport:   cfg.Decoder.FFmpeg.RTSPTransport,
				HWAccel:         cfg.Decoder.FFmpeg.HWAccel,
				HWAccelDevice:   cfg.Decoder.FFmpeg.HWAccelDevice,
				InitHWDevice:    cfg.Decoder.FFmpeg.InitHWDevice,
				VideoDecoder:    cfg.Decoder.FFmpeg.VideoDecoder,
				ProbeSize:       cfg.Decoder.FFmpeg.ProbeSize,
				AnalyzeDuration: cfg.Decoder.FFmpeg.AnalyzeDuration,
				LowDelay:        cfg.Decoder.FFmpeg.LowDelay,
				NoBuffer:        cfg.Decoder.FFmpeg.NoBuffer,
			},
		},
		Stream: services.StreamSettings{
			WebRTC: services.WebRTCSettings{
				Enabled:    boolValue(cfg.Stream.WebRTC.Enabled, false),
				ICEServers: []stream.ICEServer{},
			},
			MJPEGFallback: services.MJPEGFallbackSettings{
				Enabled: boolValue(cfg.Stream.MJPEGFallback.Enabled, true),
			},
		},
		Recording: services.RecordingSettings{
			Storage: services.RecordingStorageSettings{
				Codec:                cfg.Recording.Storage.Codec,
				Quality:              cfg.Recording.Storage.Quality,
				MaxConcurrentEncodes: cfg.Recording.Storage.MaxConcurrentEncodes,
				FallbackToCopy:       cfg.Recording.Storage.FallbackToCopy,
			},
		},
	}
	for _, server := range cfg.Stream.WebRTC.ICEServers {
		if len(server.URLs) == 0 {
			continue
		}
		result.Stream.WebRTC.ICEServers = append(result.Stream.WebRTC.ICEServers, stream.ICEServer{
			URLs:       server.URLs,
			Username:   server.Username,
			Credential: server.Credential,
		})
	}
	return result
}

// visionMonitorSettingsFromAppConfig builds the monitor settings WITHOUT the
// detector; the caller assigns Detector from the shared object backend via
// wrapMonitorDetector so the same backend serves live detection and auto-label.
func visionMonitorSettingsFromAppConfig(cfg *mmconfig.Config) services.VisionMonitorSettings {
	snapshotDir := cfg.Vision.SnapshotDir
	if snapshotDir == "" {
		snapshotDir = "recordings"
	}
	return services.VisionMonitorSettings{
		Enabled:                   boolValue(cfg.Vision.Enabled, true),
		Interval:                  int64(cfg.Vision.IntervalMs),
		CaptureTimeout:            int64(cfg.Vision.CaptureTimeoutMs),
		DiagnosticCooldownSeconds: int64(cfg.Vision.DiagnosticCooldownSeconds),
		PersistSampledDiagnostics: cfg.Vision.PersistSampledDiagnostics,
		SnapshotDir:               snapshotDir,
	}
}

// healthSettingsDefaultsFromAppConfig maps the config.json health block into the
// default runtime-editable health settings. These seed the persisted settings on
// first run; thereafter the UI-edited copy wins.
func healthSettingsDefaultsFromAppConfig(cfg *mmconfig.Config) services.HealthSettings {
	h := cfg.Health
	return services.HealthSettings{
		Enabled:           boolValue(h.Enabled, true),
		IntervalMs:        h.IntervalMs,
		TimeoutMs:         h.TimeoutMs,
		FailureThreshold:  h.FailureThreshold,
		RecoveryThreshold: h.RecoveryThreshold,
	}
}

func resolveShredPasses(cfg *mmconfig.Config) int {
	s := cfg.Recording.Shred
	if s.Enabled != nil && !*s.Enabled {
		return 0
	}
	if s.Passes > 0 {
		return s.Passes
	}
	return recording.DefaultShredPasses
}

// trainingDataDir resolves the on-disk root for training datasets and models.
// It defaults to a "training" sibling of the snapshot dir so all AI artifacts
// live together under the same volume the machine health monitor watches.
func trainingDataDir(cfg *mmconfig.Config) string {
	if dir := strings.TrimSpace(cfg.Vision.Training.DataDir); dir != "" {
		return dir
	}
	base := strings.TrimSpace(cfg.Vision.SnapshotDir)
	if base == "" {
		base = "recordings"
	}
	return filepath.Join(base, "training")
}

// trainingRunConfigFromAppConfig derives the in-app trainer config: the Python
// command (shared with the detector) and the train_worker.py / base weights that
// sit next to the configured YOLO worker script.
// trainingRunConfigFromAppConfig derives the training runner's script and base-model
// paths from the detector's worker-script argument.
//
// detectorArgs is passed in RATHER than read from cfg because it must be the RESOLVED
// arguments (absolute paths). This function used to read cfg.Vision.Detector.Args
// directly, which only worked because the composition root had mutated the shared config
// in place a few lines earlier — an ordering contract enforced by a comment. Now the
// compiler enforces it.
func trainingRunConfigFromAppConfig(cfg *mmconfig.Config, configPath string, detectorArgs []string) services.TrainingRunConfig {
	detectorCfg := cfg.Vision.Detector
	workerScript := ""
	for _, arg := range detectorArgs {
		if strings.HasSuffix(strings.ToLower(strings.TrimSpace(arg)), ".py") {
			workerScript = strings.TrimSpace(arg)
			break
		}
	}
	cfgOut := services.TrainingRunConfig{PythonCmd: detectorCfg.Command, ConfigFile: configPath}
	if workerScript != "" {
		dir := filepath.Dir(workerScript)
		cfgOut.TrainScript = filepath.Join(dir, "train_worker.py")
		cfgOut.BaseModel = filepath.Join(dir, "yolo11n.pt")
	}
	return cfgOut
}

// visionToolSettingsFromAppConfig builds the on-demand vision tool's settings.
// detectorArgs must be the RESOLVED worker-script arguments — see
// trainingRunConfigFromAppConfig for why they are a parameter rather than read from cfg.
func visionToolSettingsFromAppConfig(cfg *mmconfig.Config, detectorArgs []string) services.VisionToolSettings {
	detectorCfg := cfg.Vision.Detector
	return services.VisionToolSettings{
		Mode:              detectorCfg.Mode,
		Command:           detectorCfg.Command,
		Args:              detectorArgs,
		TimeoutMs:         detectorCfg.TimeoutMs,
		UseMotionFallback: boolValue(detectorCfg.UseMotionFallback, true),
	}
}

func boolValue(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}
