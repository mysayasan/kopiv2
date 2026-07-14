package services

import (
	"context"
	"testing"

	"github.com/mysayasan/kopiv2/apps/mymatasan/entities"
	"github.com/mysayasan/kopiv2/infra/atrest"
)

// Two methods, not forty. This is the payoff of declaring the interfaces at the consumer.
type fakeCameraCreds struct{ src SnapshotSource }

func (f fakeCameraCreds) SnapshotSource(context.Context, uint64) (SnapshotSource, error) {
	return f.src, nil
}

type fakeRecordingConfigs struct{ cfg *entities.RecordingConfig }

func (f fakeRecordingConfigs) GetConfig(context.Context, int64) (*entities.RecordingConfig, error) {
	return f.cfg, nil
}

// fakeSettings serves the live decoder/storage settings the builder reads on every build.
type fakeSettings struct {
	decoder   DecoderSettings
	recording RecordingSettings
}

func (f *fakeSettings) Get(context.Context) (RuntimeSettings, error) { return RuntimeSettings{}, nil }
func (f *fakeSettings) Save(context.Context, RuntimeSettings) (RuntimeSettings, error) {
	return RuntimeSettings{}, nil
}
func (f *fakeSettings) Reset(context.Context) (RuntimeSettings, error) {
	return RuntimeSettings{}, nil
}
func (f *fakeSettings) Stream(context.Context) (StreamSettings, error) { return StreamSettings{}, nil }
func (f *fakeSettings) Decoder(context.Context) (DecoderSettings, error) {
	return f.decoder, nil
}
func (f *fakeSettings) Recording(context.Context) (RecordingSettings, error) {
	return f.recording, nil
}

func testSettings() *fakeSettings {
	s := &fakeSettings{}
	s.decoder.MJPEG.FFmpegPath = "/opt/ffmpeg"
	s.decoder.FFmpeg.RTSPTransport = "tcp"
	s.recording.Storage.Codec = "hevc"
	s.recording.Storage.Quality = 26
	return s
}

func testBuilder(t *testing.T, settings *fakeSettings) (*RecorderConfigBuilder, *atrest.Cipher) {
	t.Helper()
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	cipher, err := atrest.NewCipher(key)
	if err != nil {
		t.Fatalf("NewCipher: %v", err)
	}
	camera := fakeCameraCreds{src: SnapshotSource{
		RTSPURI:  "rtsp://discovered/stream",
		Username: "admin",
		Password: "secret",
	}}
	return NewRecorderConfigBuilder(camera, fakeRecordingConfigs{}, settings, cipher, 3, nil), cipher
}

// THE regression test for this whole change. Every field that a recorder needs must
// survive the build — the bug this builder exists to prevent is a field being silently
// dropped, which already cost secure-shred once (ShredPasses was missing from the
// settings-save path, so shred degraded to a plain unlink).
func TestRecorderConfigBuilder_CarriesEveryRecorderField(t *testing.T) {
	settings := testSettings()
	builder, cipher := testBuilder(t, settings)

	cfg := &entities.RecordingConfig{
		CameraId:       7,
		Enabled:        true,
		PreRollSec:     20,
		PostRollSec:    15,
		StoragePath:    "/data/rec",
		SegmentMinutes: 10,
		RetentionDays:  30,
	}

	got, warning := builder.ForRecording(context.Background(), cfg)
	if warning != "" {
		t.Fatalf("unexpected warning: %s", warning)
	}

	if got.CameraId != 7 || !got.Enabled {
		t.Fatalf("identity fields lost: %+v", got)
	}
	if got.PreRollSec != 20 || got.PostRollSec != 15 || got.SegmentMinutes != 10 || got.RetentionDays != 30 {
		t.Fatalf("stored config fields lost: %+v", got)
	}
	if got.StoragePath != "/data/rec" {
		t.Fatalf("StoragePath = %q", got.StoragePath)
	}
	// The two fields that were actually lost in production, and the one that was stale.
	if got.ShredPasses != 3 {
		t.Fatalf("ShredPasses = %d, want 3 — secure shred would degrade to a plain unlink", got.ShredPasses)
	}
	if got.Cipher != cipher {
		t.Fatal("Cipher lost — segments would be written unencrypted")
	}
	if got.RecordCodec != "hevc" || got.RecordQuality != 26 {
		t.Fatalf("storage codec not read live: codec=%q quality=%d", got.RecordCodec, got.RecordQuality)
	}
	// Decoder settings, read live.
	if got.FFmpegPath != "/opt/ffmpeg" || got.RTSPTransport != "tcp" {
		t.Fatalf("decoder settings lost: %+v", got)
	}
	// FallbackToCopy is nil in settings, which must mean "on".
	if !got.RecordFallbackCopy {
		t.Fatal("RecordFallbackCopy should default on when unset")
	}
}

// The bug this fixes as a side effect: the startup path captured the at-rest codec ONCE at
// boot, so changing it in Settings never applied until a restart — despite the code
// claiming otherwise. Reading live means the next (re)configure picks it up.
func TestRecorderConfigBuilder_ReadsStorageCodecLive(t *testing.T) {
	settings := testSettings()
	builder, _ := testBuilder(t, settings)
	cfg := &entities.RecordingConfig{CameraId: 1, Enabled: true}

	if got, _ := builder.ForRecording(context.Background(), cfg); got.RecordCodec != "hevc" {
		t.Fatalf("initial codec = %q", got.RecordCodec)
	}

	// Operator changes the codec in Settings. No restart.
	settings.recording.Storage.Codec = "h264"

	if got, _ := builder.ForRecording(context.Background(), cfg); got.RecordCodec != "h264" {
		t.Fatalf("codec change did not apply without a restart: got %q", got.RecordCodec)
	}
}

// A bare stream override must get the camera's credentials injected; with no override the
// discovered ONVIF URI is used as-is.
func TestRecorderConfigBuilder_ResolvesStreamURLAndCredentials(t *testing.T) {
	builder, _ := testBuilder(t, testSettings())

	got, _ := builder.ForRecording(context.Background(), &entities.RecordingConfig{
		CameraId: 1, Enabled: true,
	})
	if got.RTSPURI != "rtsp://discovered/stream" {
		t.Fatalf("with no override the discovered URI should be used, got %q", got.RTSPURI)
	}

	got, _ = builder.ForRecording(context.Background(), &entities.RecordingConfig{
		CameraId: 1, Enabled: true, StreamURL: "rtsp://cam/main",
	})
	if got.RTSPURI != "rtsp://admin:secret@cam/main" {
		t.Fatalf("credentials were not injected into the override URL: %q", got.RTSPURI)
	}
}

// An enabled camera with no stream at all must warn — otherwise recording silently never
// happens and nobody is told.
func TestRecorderConfigBuilder_WarnsWhenEnabledWithNoStream(t *testing.T) {
	settings := testSettings()
	builder := NewRecorderConfigBuilder(
		fakeCameraCreds{src: SnapshotSource{}}, // no discovered URI, no credentials
		fakeRecordingConfigs{}, settings, nil, 0, nil,
	)

	_, warning := builder.ForRecording(context.Background(), &entities.RecordingConfig{
		CameraId: 1, Enabled: true,
	})
	if warning == "" {
		t.Fatal("an enabled camera with no stream must warn, or recording silently never starts")
	}

	// A DISABLED camera with no stream is perfectly normal — it must not warn.
	_, warning = builder.ForRecording(context.Background(), &entities.RecordingConfig{
		CameraId: 1, Enabled: false,
	})
	if warning != "" {
		t.Fatalf("a disabled camera must not warn: %s", warning)
	}
}

// The detect-only stream writes no segments, so it must carry no storage/retention/cipher
// settings — only the stream and the siphon tee. Handing it a cipher or a retention would
// be meaningless at best.
func TestRecorderConfigBuilder_DetectOnlyCarriesNoStorageSettings(t *testing.T) {
	settings := testSettings()
	camera := fakeCameraCreds{src: SnapshotSource{RTSPURI: "rtsp://discovered/stream"}}
	recordings := fakeRecordingConfigs{cfg: &entities.RecordingConfig{
		CameraId: 1, RetentionDays: 30, StoragePath: "/data/rec",
	}}
	builder := NewRecorderConfigBuilder(camera, recordings, settings, nil, 3, nil)

	got, ok := builder.ForDetectOnly(context.Background(), 1)
	if !ok {
		t.Fatal("expected a detect-only config")
	}
	if !got.DetectOnly || !got.Enabled {
		t.Fatalf("detect-only flags wrong: %+v", got)
	}
	if got.RetentionDays != 0 || got.StoragePath != "" || got.Cipher != nil || got.ShredPasses != 0 {
		t.Fatalf("detect-only must carry no storage settings: %+v", got)
	}
	if got.RTSPURI == "" {
		t.Fatal("detect-only needs a stream")
	}
}

// Detect-only deliberately accepts the LIVE-view URL as a fallback (it only needs pixels,
// not the recording substream) — a difference from ForRecording that must be preserved.
func TestRecorderConfigBuilder_DetectOnlyFallsBackToLiveStreamURL(t *testing.T) {
	settings := testSettings()
	camera := fakeCameraCreds{src: SnapshotSource{Username: "u", Password: "p"}} // no discovered URI
	recordings := fakeRecordingConfigs{cfg: &entities.RecordingConfig{
		CameraId: 1, LiveStreamUrl: "rtsp://cam/sub",
	}}
	builder := NewRecorderConfigBuilder(camera, recordings, settings, nil, 0, nil)

	got, ok := builder.ForDetectOnly(context.Background(), 1)
	if !ok {
		t.Fatal("live-view URL should be usable as a detect-only source")
	}
	if got.RTSPURI != "rtsp://u:p@cam/sub" {
		t.Fatalf("live URL not used/credentialed: %q", got.RTSPURI)
	}
}

// No stream anywhere means no detection stream can run.
func TestRecorderConfigBuilder_DetectOnlyFailsWithNoStream(t *testing.T) {
	builder := NewRecorderConfigBuilder(
		fakeCameraCreds{src: SnapshotSource{}}, fakeRecordingConfigs{}, testSettings(), nil, 0, nil,
	)
	if _, ok := builder.ForDetectOnly(context.Background(), 1); ok {
		t.Fatal("a camera with no stream must not yield a detect-only config")
	}
}
