package services

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"math/rand"
	"testing"

	"github.com/mysayasan/kopiv2/apps/mymatasan/entities"
	"github.com/mysayasan/kopiv2/domain/notification"
)

// --- scene generators --------------------------------------------------------
//
// Encoded to JPEG so the monitor exercises the same decode path the recorder's siphon
// feeds it, rather than a shortcut that would hide a decoding problem.

func jpegOf(img image.Image, t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("encode: %v", err)
	}
	return buf.Bytes()
}

// busyScene is a normal camera view: structure plus a little noise.
func busyScene(seed int64) image.Image {
	rng := rand.New(rand.NewSource(seed))
	img := image.NewRGBA(image.Rect(0, 0, 192, 192))
	for y := 0; y < 192; y++ {
		for x := 0; x < 192; x++ {
			v := 45
			if (x/12+y/12)%2 == 0 {
				v = 205
			}
			v += rng.Intn(24) - 12
			if v < 0 {
				v = 0
			}
			if v > 255 {
				v = 255
			}
			img.Set(x, y, color.Gray{Y: uint8(v)})
		}
	}
	return img
}

// coveredScene is a lens with something over it: mid-grey, no structure.
func coveredScene() image.Image {
	img := image.NewRGBA(image.Rect(0, 0, 192, 192))
	for y := 0; y < 192; y++ {
		for x := 0; x < 192; x++ {
			img.Set(x, y, color.Gray{Y: 120})
		}
	}
	return img
}

// darkScene is a normal night view: dark, and legitimately almost featureless.
func darkScene() image.Image {
	img := image.NewRGBA(image.Rect(0, 0, 192, 192))
	for y := 0; y < 192; y++ {
		for x := 0; x < 192; x++ {
			img.Set(x, y, color.Gray{Y: 12})
		}
	}
	return img
}

// --- fakes -------------------------------------------------------------------

// scriptedFrames returns a queued frame per sample, with a capture timestamp that always
// advances (so a repeated PICTURE is distinguishable from a stalled siphon).
type scriptedFrames struct {
	frames [][]byte
	i      int
	tick   int64
	// freezeCapturedAt models a siphon that has produced no new frame at all.
	freezeCapturedAt bool
}

func (s *scriptedFrames) LatestFrame(int64) ([]byte, int64, bool) {
	if len(s.frames) == 0 {
		return nil, 0, false
	}
	f := s.frames[s.i]
	if s.i < len(s.frames)-1 {
		s.i++
	}
	// The capture time advances on every call and is deliberately INDEPENDENT of which
	// frame was served: that is what lets a test serve the same picture repeatedly (a
	// frozen camera) and still have the monitor see two distinct captures. Holding it
	// fixed instead models a siphon that produced nothing new, which is a different fault.
	s.tick++
	at := int64(1000 + s.tick)
	if s.freezeCapturedAt {
		at = 1000
	}
	return f, at, true
}

type tamperRecordingStub struct {
	IRecordingService
	configs []*entities.RecordingConfig
}

func (s *tamperRecordingStub) ListConfigs(context.Context) ([]*entities.RecordingConfig, error) {
	return s.configs, nil
}

type staticTamperSettings struct{ cfg TamperSettings }

func (s staticTamperSettings) Get(context.Context) (TamperSettings, error) { return s.cfg, nil }
func (s staticTamperSettings) Save(_ context.Context, in TamperSettings) (TamperSettings, error) {
	return in, nil
}

func newTamperRig(frames [][]byte, cfg TamperSettings) (*CameraTamperMonitor, *scriptedFrames, *capturedNotifier) {
	src := &scriptedFrames{frames: frames}
	notif := &capturedNotifier{}
	rec := &tamperRecordingStub{configs: []*entities.RecordingConfig{{CameraId: 3, Enabled: true}}}
	m := NewCameraTamperMonitor(src, &fakeContinuityCamera{name: "Lobby"}, rec, staticTamperSettings{cfg}, notif, nil)
	return m, src, notif
}

func testTamperCfg() TamperSettings {
	c := DefaultTamperSettings()
	c.FailureThreshold = 2
	c.FrozenSeconds = 60
	return c
}

func alertsTitled(sent []notification.Notification, title string) int {
	n := 0
	for _, s := range sent {
		if s.Title == title {
			n++
		}
	}
	return n
}

// --- covered -----------------------------------------------------------------

// The headline case. A lens covered on a camera that stays online and keeps recording is
// invisible to every other check in the product.
func TestTamperDetectsACoveredLens(t *testing.T) {
	cfg := testTamperCfg()
	normal := jpegOf(busyScene(1), t)
	covered := jpegOf(coveredScene(), t)

	m, src, notif := newTamperRig(nil, cfg)
	ctx := context.Background()

	// Build a baseline of normal views first — "covered" is a collapse relative to what
	// THIS camera usually looks like, so there has to be a usual.
	for i := 0; i < tamperBaselineSize; i++ {
		src.frames = [][]byte{normal}
		src.i = 0
		m.Sweep(ctx, cfg, int64(100+i))
	}
	if got := alertsTitled(notif.sent, "Camera view blocked"); got != 0 {
		t.Fatalf("a normal view must not alert, got %d", got)
	}

	for i := 0; i < cfg.FailureThreshold; i++ {
		src.frames = [][]byte{covered}
		src.i = 0
		m.Sweep(ctx, cfg, int64(200+i))
	}
	if got := alertsTitled(notif.sent, "Camera view blocked"); got != 1 {
		t.Fatalf("expected one covered-lens alert, got %d (all: %d)", got, len(notif.sent))
	}
}

// THE guard that decides whether this feature survives a real site. At night a scene loses
// its edge energy legitimately, and under infrared it loses contrast too. Without the
// low-light suppression every camera in the fleet reports a covered lens at dusk and the
// whole thing is muted by morning — after which it protects nothing.
func TestTamperStaysQuietOnADarkNightScene(t *testing.T) {
	cfg := testTamperCfg()
	normal := jpegOf(busyScene(2), t)
	dark := jpegOf(darkScene(), t)

	m, src, notif := newTamperRig(nil, cfg)
	ctx := context.Background()
	for i := 0; i < tamperBaselineSize; i++ {
		src.frames = [][]byte{normal}
		src.i = 0
		m.Sweep(ctx, cfg, int64(100+i))
	}
	// Night falls and stays fallen.
	for i := 0; i < 20; i++ {
		src.frames = [][]byte{dark}
		src.i = 0
		m.Sweep(ctx, cfg, int64(300+i))
	}
	if got := alertsTitled(notif.sent, "Camera view blocked"); got != 0 {
		t.Fatalf("a dark night scene must not read as a covered lens, got %d alerts", got)
	}
}

// A covered lens left in place must not become the camera's new normal. If alerting
// samples fed the baseline, the median would drift down to the covered reading and the
// alert would clear itself while the lens was still covered.
func TestTamperDoesNotLearnACoveredLensAsNormal(t *testing.T) {
	cfg := testTamperCfg()
	normal := jpegOf(busyScene(3), t)
	covered := jpegOf(coveredScene(), t)

	m, src, notif := newTamperRig(nil, cfg)
	ctx := context.Background()
	for i := 0; i < tamperBaselineSize; i++ {
		src.frames = [][]byte{normal}
		src.i = 0
		m.Sweep(ctx, cfg, int64(100+i))
	}
	// Covered, and left that way for far longer than the baseline window.
	for i := 0; i < tamperBaselineSize*2; i++ {
		src.frames = [][]byte{covered}
		src.i = 0
		m.Sweep(ctx, cfg, int64(400+i))
	}
	if got := alertsTitled(notif.sent, "Camera view restored"); got != 0 {
		t.Fatalf("the alert cleared itself while the lens was still covered (%d recovery notices)", got)
	}
}

func TestTamperClearsWhenTheViewComesBack(t *testing.T) {
	cfg := testTamperCfg()
	normal := jpegOf(busyScene(4), t)
	covered := jpegOf(coveredScene(), t)

	m, src, notif := newTamperRig(nil, cfg)
	ctx := context.Background()
	for i := 0; i < tamperBaselineSize; i++ {
		src.frames = [][]byte{normal}
		src.i = 0
		m.Sweep(ctx, cfg, int64(100+i))
	}
	for i := 0; i < cfg.FailureThreshold; i++ {
		src.frames = [][]byte{covered}
		src.i = 0
		m.Sweep(ctx, cfg, int64(200+i))
	}
	src.frames = [][]byte{normal}
	src.i = 0
	m.Sweep(ctx, cfg, 300)

	if got := alertsTitled(notif.sent, "Camera view restored"); got != 1 {
		t.Fatalf("expected one recovery notification, got %d", got)
	}
}

// --- frozen ------------------------------------------------------------------

// A frozen stream keeps the camera online and the recorder writing — of one still picture.
// The rule is expressed in seconds because a few identical frames happen and a minute of
// them does not.
func TestTamperDetectsAFrozenPicture(t *testing.T) {
	cfg := testTamperCfg()
	still := jpegOf(busyScene(5), t)
	m, src, notif := newTamperRig(nil, cfg)
	ctx := context.Background()

	// The same picture, with the siphon reporting new frames each time.
	for i := 0; i < 6; i++ {
		src.frames = [][]byte{still}
		src.i = 0
		// Advance well past FrozenSeconds across the sweeps.
		m.Sweep(ctx, cfg, int64(1000+i*40))
	}
	if got := alertsTitled(notif.sent, "Camera picture frozen"); got != 1 {
		t.Fatalf("expected one frozen alert, got %d (all: %d)", got, len(notif.sent))
	}
}

// A live camera watching an empty room is NOT frozen: sensor noise means successive
// frames differ slightly even when nothing happens. Getting this wrong would alarm on
// every quiet camera every night.
func TestTamperDoesNotCallAQuietSceneFrozen(t *testing.T) {
	cfg := testTamperCfg()
	m, src, notif := newTamperRig(nil, cfg)
	ctx := context.Background()

	for i := 0; i < 10; i++ {
		// Same scene, fresh noise — an empty room on a live camera.
		src.frames = [][]byte{jpegOf(busyScene(int64(100+i)), t)}
		src.i = 0
		m.Sweep(ctx, cfg, int64(1000+i*40))
	}
	if got := alertsTitled(notif.sent, "Camera picture frozen"); got != 0 {
		t.Fatalf("a quiet but live scene must not read as frozen, got %d alerts", got)
	}
}

// A siphon that has not produced a new frame is not evidence that the CAMERA froze — the
// capture timestamp has not moved, so there is nothing to compare.
func TestTamperIgnoresARepeatedFrameWithNoNewCaptureTime(t *testing.T) {
	cfg := testTamperCfg()
	still := jpegOf(busyScene(6), t)
	src := &scriptedFrames{frames: [][]byte{still}, freezeCapturedAt: true}
	notif := &capturedNotifier{}
	rec := &tamperRecordingStub{configs: []*entities.RecordingConfig{{CameraId: 3, Enabled: true}}}
	m := NewCameraTamperMonitor(src, &fakeContinuityCamera{}, rec, staticTamperSettings{cfg}, notif, nil)

	for i := 0; i < 10; i++ {
		m.Sweep(context.Background(), cfg, int64(1000+i*40))
	}
	if got := alertsTitled(notif.sent, "Camera picture frozen"); got != 0 {
		t.Fatalf("a stalled siphon is not a frozen camera, got %d alerts", got)
	}
}

// --- general -----------------------------------------------------------------

// Edge-triggered: an alert must be raised once, not repeated every 30 seconds for as long
// as the lens stays covered.
func TestTamperAlertsOnceNotEverySweep(t *testing.T) {
	cfg := testTamperCfg()
	normal := jpegOf(busyScene(7), t)
	covered := jpegOf(coveredScene(), t)
	m, src, notif := newTamperRig(nil, cfg)
	ctx := context.Background()

	for i := 0; i < tamperBaselineSize; i++ {
		src.frames = [][]byte{normal}
		src.i = 0
		m.Sweep(ctx, cfg, int64(100+i))
	}
	for i := 0; i < 15; i++ {
		src.frames = [][]byte{covered}
		src.i = 0
		m.Sweep(ctx, cfg, int64(500+i))
	}
	if got := alertsTitled(notif.sent, "Camera view blocked"); got != 1 {
		t.Fatalf("expected exactly one alert across a sustained cover, got %d", got)
	}
}

// A camera with recording off has no siphon frame, and is not something this monitor can
// speak about. Silence is more honest than reporting it healthy.
func TestTamperSkipsCamerasWithRecordingDisabled(t *testing.T) {
	cfg := testTamperCfg()
	src := &scriptedFrames{frames: [][]byte{jpegOf(coveredScene(), t)}}
	notif := &capturedNotifier{}
	rec := &tamperRecordingStub{configs: []*entities.RecordingConfig{{CameraId: 3, Enabled: false}}}
	m := NewCameraTamperMonitor(src, &fakeContinuityCamera{}, rec, staticTamperSettings{cfg}, notif, nil)

	for i := 0; i < 10; i++ {
		m.Sweep(context.Background(), cfg, int64(100+i))
	}
	if len(notif.sent) != 0 {
		t.Fatalf("a camera with recording off must not be judged, got %+v", notif.sent)
	}
}

func TestTamperSettingsRejectNonsenseValues(t *testing.T) {
	got := normalizeTamperSettings(TamperSettings{CoveredRatio: 1.5, MovedDistance: 0, FrozenSeconds: -1})
	def := DefaultTamperSettings()
	if got.CoveredRatio != def.CoveredRatio {
		t.Errorf("a ratio >= 1 would alarm on every normal sample; got %v", got.CoveredRatio)
	}
	if got.MovedDistance != def.MovedDistance {
		t.Errorf("a zero distance would alarm on everything; got %v", got.MovedDistance)
	}
	if got.FrozenSeconds != def.FrozenSeconds {
		t.Errorf("FrozenSeconds = %v", got.FrozenSeconds)
	}
}
