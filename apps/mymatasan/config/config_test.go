package config

import (
	"path/filepath"
	"testing"
)

// The whole point of the seam: the blocks stay at the TOP LEVEL of config.json, exactly
// where every deployed appliance already has them. Ownership moved from the shared model
// to the app; the file format did not. If this test ever needs a nested "app" key, the
// change has broken every config file in the field.
func TestLoad_DecodesTopLevelBlocksFromTheSharedDocument(t *testing.T) {
	// A realistic slice of config.json — including blocks that belong to OTHER owners
	// (server, db, security), which this app must simply ignore.
	raw := []byte(`{
	  "server":   { "tlsPorts": [3000] },
	  "db":       { "engine": "sqlite" },
	  "security": { "encryptAtRest": true },
	  "camera":   { "ffmpegPath": "/usr/bin/ffmpeg" },
	  "decoder":  { "browseRoots": ["/data/ffmpeg"], "mjpeg": { "quality": 7 }, "ffmpeg": { "rtspTransport": "tcp" } },
	  "vision":   { "snapshotDir": "recordings", "detector": { "mode": "persistent", "command": "python" } },
	  "health":   { "intervalMs": 30000 },
	  "recording":{ "storage": { "codec": "hevc" } },
	  "stream":   { "webrtc": { "enabled": true } }
	}`)

	cfg, err := Load(raw)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Camera.FFmpegPath != "/usr/bin/ffmpeg" {
		t.Fatalf("camera block lost: %+v", cfg.Camera)
	}
	if cfg.Decoder.FFmpeg.RTSPTransport != "tcp" || cfg.Decoder.MJPEG.Quality != 7 {
		t.Fatalf("decoder block lost: %+v", cfg.Decoder)
	}
	if len(cfg.Decoder.BrowseRoots) != 1 || cfg.Decoder.BrowseRoots[0] != "/data/ffmpeg" {
		t.Fatalf("decoder.browseRoots lost: %+v", cfg.Decoder.BrowseRoots)
	}
	if cfg.Vision.Detector.Mode != "persistent" || cfg.Vision.Detector.Command != "python" {
		t.Fatalf("vision block lost: %+v", cfg.Vision.Detector)
	}
	if cfg.Recording.Storage.Codec != "hevc" {
		t.Fatalf("recording block lost: %+v", cfg.Recording)
	}
	if cfg.Stream.WebRTC.Enabled == nil || !*cfg.Stream.WebRTC.Enabled {
		t.Fatalf("stream block lost: %+v", cfg.Stream)
	}
	if cfg.Health.IntervalMs != 30000 {
		t.Fatalf("health block lost: %+v", cfg.Health)
	}
}

// A malformed config must be an ERROR. The shared loader used to Decode and DISCARD the
// error, so one trailing comma produced a silently all-zero config: the app booted on
// defaults, ignored everything the operator had set, and said nothing.
func TestLoad_MalformedConfigIsAnError(t *testing.T) {
	if _, err := Load([]byte(`{"vision": {,}}`)); err == nil {
		t.Fatal("a malformed config must fail loudly, not boot on silent defaults")
	}
}

// An app with no config blocks at all is legitimate — it gets zero values, which are the
// documented defaults.
func TestLoad_EmptyAndAbsentAreZeroValues(t *testing.T) {
	for _, raw := range [][]byte{nil, []byte(`{}`), []byte(`{"server":{"tlsPorts":[3000]}}`)} {
		cfg, err := Load(raw)
		if err != nil {
			t.Fatalf("Load(%q): %v", raw, err)
		}
		if cfg == nil || cfg.Vision.Detector.Mode != "" {
			t.Fatalf("expected zero-value config for %q, got %+v", raw, cfg)
		}
	}
}

// Normalize resolves this app's data-relative paths against the writable data dir. This is
// the code that used to live in infra/apphost — the generic host resolving YOLO training
// directories — and it is why a fourth app could not be added without dragging vision
// config along.
func TestNormalize_ResolvesAppPathsAgainstDataDir(t *testing.T) {
	dataDir := t.TempDir()

	cfg, err := Load([]byte(`{"vision": {"snapshotDir": "recordings", "training": {"dataDir": "training"}}}`))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	cfg.Normalize(dataDir)

	if want := filepath.Join(dataDir, "recordings"); cfg.Vision.SnapshotDir != want {
		t.Fatalf("snapshotDir = %q, want %q", cfg.Vision.SnapshotDir, want)
	}
	if want := filepath.Join(dataDir, "training"); cfg.Vision.Training.DataDir != want {
		t.Fatalf("training.dataDir = %q, want %q", cfg.Vision.Training.DataDir, want)
	}
}

// An unset snapshotDir must still land under the data dir, not the process working
// directory — a packaged Windows service runs with CWD=C:\Windows\System32.
func TestNormalize_UnsetSnapshotDirDefaultsUnderDataDir(t *testing.T) {
	dataDir := t.TempDir()

	cfg, _ := Load([]byte(`{}`))
	cfg.Normalize(dataDir)

	if want := filepath.Join(dataDir, "recordings"); cfg.Vision.SnapshotDir != want {
		t.Fatalf("snapshotDir = %q, want %q", cfg.Vision.SnapshotDir, want)
	}
}

// An empty training dir means "not configured" and must stay empty rather than silently
// becoming the data dir itself.
func TestNormalize_EmptyTrainingDirStaysEmpty(t *testing.T) {
	cfg, _ := Load([]byte(`{}`))
	cfg.Normalize(t.TempDir())

	if cfg.Vision.Training.DataDir != "" {
		t.Fatalf("unset training dir became %q", cfg.Vision.Training.DataDir)
	}
}
