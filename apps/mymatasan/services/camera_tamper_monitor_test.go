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

// --- moved ---------------------------------------------------------------------
//
// None of these existed. TamperMoved was never driven to an alert by any test in the
// repo — the only reference to MovedDistance was the settings-normalisation check — which
// is why a verdict that could not fire at all sat in shipped code behind a green suite.

// wallScene is what a re-aimed camera sees: a different place, still sharp, still lit.
//
// The numbers matter and were measured, not guessed. Against busyScene this keeps 65% of
// the baseline edge energy — far above the 0.15 covered ratio, so it cannot pass by
// tripping the wrong verdict — while its luma sits entirely elsewhere, giving a histogram
// distance of 1.0 against the 0.55 threshold.
//
// The block size is load-bearing. An earlier version of this scene used 3px banding, which
// is averaged out of existence by the 32x32 downsample: it arrived at the monitor as a flat
// grey and was correctly reported as a COVERED lens, so the test proved nothing about
// movement. Structure has to survive the grid to count as structure.
func wallScene(seed int64) image.Image {
	rng := rand.New(rand.NewSource(seed))
	img := image.NewRGBA(image.Rect(0, 0, 192, 192))
	for y := 0; y < 192; y++ {
		for x := 0; x < 192; x++ {
			v := 110
			if (x/12+y/12)%2 == 0 {
				v = 240
			}
			v += rng.Intn(10) - 5
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

// personScene is the busy scene with a figure crossing it: a real, local change that must
// NOT read as tampering. This is the false positive that decides whether the feature is
// usable on a site with people in it.
func personScene(seed int64) image.Image {
	img := busyScene(seed).(*image.RGBA)
	for y := 60; y < 150; y++ {
		for x := 80; x < 110; x++ {
			img.Set(x, y, color.Gray{Y: 20})
		}
	}
	return img
}

func feed(t *testing.T, m *CameraTamperMonitor, src *scriptedFrames, frame []byte, n int, from int64) {
	t.Helper()
	ctx := context.Background()
	for i := 0; i < n; i++ {
		src.frames = [][]byte{frame}
		src.i = 0
		m.Sweep(ctx, testTamperCfg(), from+int64(i))
	}
}

// THE regression test. A camera is turned to face somewhere else and LEFT there: it
// differs from its predecessor for exactly one sample and matches it ever after, because
// the new view is as steady as the old one was.
//
// The original implementation compared each sample against the previous one and then
// required FailureThreshold consecutive samples carrying the verdict. Those two are
// incompatible — the streak resets on the sample after the move — so the alert could never
// be raised. It is now measured against the camera's rolling reference, which stays
// different for as long as the camera stays moved.
func TestTamperDetectsACameraTurnedToFaceSomewhereElse(t *testing.T) {
	cfg := testTamperCfg()
	normal := jpegOf(busyScene(11), t)
	wall := jpegOf(wallScene(12), t)

	m, src, notif := newTamperRig(nil, cfg)
	feed(t, m, src, normal, tamperBaselineSize, 100)
	if got := alertsTitled(notif.sent, "Camera view changed"); got != 0 {
		t.Fatalf("a steady view must not alert, got %d", got)
	}

	// Moved once, then STEADY at the new view — which is the whole point. Far more
	// samples than FailureThreshold, so a verdict that only survives the transition
	// cannot pass by accident.
	feed(t, m, src, wall, 10, 300)
	if got := alertsTitled(notif.sent, "Camera view changed"); got != 1 {
		t.Fatalf("a camera left pointing somewhere else must raise exactly one moved alert, got %d (all sent: %d)", got, len(notif.sent))
	}
}

// The other half of the same defect: a moved camera must not fold its new view into its
// own idea of normal and quietly accept it. Without excluding alerting samples from the
// reference, the alert clears itself after half a window and the camera is left pointing
// at a wall with nothing outstanding.
func TestTamperDoesNotAcceptAMovedCameraAsTheNewNormal(t *testing.T) {
	cfg := testTamperCfg()
	normal := jpegOf(busyScene(21), t)
	wall := jpegOf(wallScene(22), t)

	m, src, notif := newTamperRig(nil, cfg)
	feed(t, m, src, normal, tamperBaselineSize, 100)
	feed(t, m, src, wall, 4, 300)
	if got := alertsTitled(notif.sent, "Camera view changed"); got != 1 {
		t.Fatalf("want the moved alert raised, got %d", got)
	}
	// Long enough to refill the reference twice over if alerting samples were being kept.
	feed(t, m, src, wall, tamperBaselineSize*2, 400)
	if got := alertsTitled(notif.sent, "Camera view restored"); got != 0 {
		t.Fatalf("a camera still pointing at the wall must not be reported as restored; got %d recoveries", got)
	}
}

// And it must clear when somebody puts the camera back, or the alert is unclearable and
// the operator learns to ignore it.
func TestTamperClearsWhenAMovedCameraIsPutBack(t *testing.T) {
	cfg := testTamperCfg()
	normal := jpegOf(busyScene(31), t)
	wall := jpegOf(wallScene(32), t)

	m, src, notif := newTamperRig(nil, cfg)
	feed(t, m, src, normal, tamperBaselineSize, 100)
	feed(t, m, src, wall, 4, 300)
	if got := alertsTitled(notif.sent, "Camera view changed"); got != 1 {
		t.Fatalf("want the moved alert raised first, got %d", got)
	}
	feed(t, m, src, normal, 4, 500)
	if got := alertsTitled(notif.sent, "Camera view restored"); got != 1 {
		t.Fatalf("putting the camera back must clear the alert exactly once, got %d", got)
	}
}

// The false positive that would make this unusable on any site with people on it. A figure
// crossing frame is a real change to the picture and must stay well under the threshold —
// which is why the comparison is a coarse 16-bucket histogram over a 32x32 grid rather
// than anything that could notice a person.
func TestTamperIgnoresSomebodyWalkingThroughFrame(t *testing.T) {
	cfg := testTamperCfg()
	normal := jpegOf(busyScene(41), t)
	person := jpegOf(personScene(41), t)

	m, src, notif := newTamperRig(nil, cfg)
	feed(t, m, src, normal, tamperBaselineSize, 100)
	// Someone walks through and stays a while — longer than the debounce, so only the
	// SIZE of the change can be what keeps this quiet.
	feed(t, m, src, person, 6, 300)
	if got := alertsTitled(notif.sent, "Camera view changed"); got != 0 {
		t.Fatalf("a person crossing frame is not a moved camera; got %d alerts", got)
	}
}

// Dusk. A camera whose scene goes dark has lost its histogram legitimately, and a fleet
// that reports every camera as moved at nightfall is a fleet whose tamper alerts are muted
// by morning. Same guard, same reason, as the covered rule.
func TestTamperStaysQuietWhenTheSceneGoesDark(t *testing.T) {
	cfg := testTamperCfg()
	normal := jpegOf(busyScene(51), t)
	dark := jpegOf(darkScene(), t)

	m, src, notif := newTamperRig(nil, cfg)
	feed(t, m, src, normal, tamperBaselineSize, 100)
	feed(t, m, src, dark, 20, 300)
	if got := alertsTitled(notif.sent, "Camera view changed"); got != 0 {
		t.Fatalf("nightfall is not a moved camera; got %d alerts", got)
	}
}

// A covered lens changes the whole histogram too. Reporting both verdicts would raise two
// alarms for one physical event, and the second one is not something the picture can
// support: you cannot tell where a camera is aimed when you cannot see out of it.
func TestTamperReportsACoveredLensAsCoveredOnly(t *testing.T) {
	cfg := testTamperCfg()
	normal := jpegOf(busyScene(61), t)
	covered := jpegOf(coveredScene(), t)

	m, src, notif := newTamperRig(nil, cfg)
	feed(t, m, src, normal, tamperBaselineSize, 100)
	feed(t, m, src, covered, 6, 300)
	if got := alertsTitled(notif.sent, "Camera view blocked"); got != 1 {
		t.Fatalf("want exactly one covered alert, got %d", got)
	}
	if got := alertsTitled(notif.sent, "Camera view changed"); got != 0 {
		t.Fatalf("a covered lens must not also be reported as moved; got %d", got)
	}
}

// Nothing may be judged before the camera has a normal to be judged against. A verdict on
// the first frame would mean every camera alerts the moment it is added.
func TestTamperWithNoReferenceYetSaysNothingAboutMovement(t *testing.T) {
	cfg := testTamperCfg()
	normal := jpegOf(busyScene(71), t)
	wall := jpegOf(wallScene(72), t)

	m, src, notif := newTamperRig(nil, cfg)
	feed(t, m, src, normal, 2, 100)
	feed(t, m, src, wall, 6, 200)
	if got := alertsTitled(notif.sent, "Camera view changed"); got != 0 {
		t.Fatalf("with almost no history there is no normal to differ from; got %d alerts", got)
	}
}

// Uncovering a lens must not immediately be reported as moving the camera.
//
// A lens covered for any length of time fills the window with featureless grey. If those
// frames were folded into the reference, the real scene would be a long way from it the
// moment somebody uncovered the lens — so clearing one alarm would instantly raise
// another, blaming the operator for moving a camera they had just fixed. This is the case
// the edge-energy baseline never had to think about and this one does.
func TestTamperDoesNotCallAnUncoveredLensAMovedCamera(t *testing.T) {
	cfg := testTamperCfg()
	normal := jpegOf(busyScene(81), t)
	covered := jpegOf(coveredScene(), t)

	m, src, notif := newTamperRig(nil, cfg)
	feed(t, m, src, normal, tamperBaselineSize, 100)
	// Covered for well over a full window, so a reference that accepted these frames
	// would be entirely grey by the end of it.
	feed(t, m, src, covered, tamperBaselineSize*2, 300)
	if got := alertsTitled(notif.sent, "Camera view blocked"); got != 1 {
		t.Fatalf("want the covered alert, got %d", got)
	}
	// The lens is cleaned. The camera has not moved.
	feed(t, m, src, normal, 6, 900)
	if got := alertsTitled(notif.sent, "Camera view changed"); got != 0 {
		t.Fatalf("uncovering a lens is not moving the camera; got %d moved alerts", got)
	}
}

// --- the PTZ interlock (W3-5) ------------------------------------------------
//
// A PTZ camera being re-aimed by an operator, an alarm or its own guard tour produces
// EXACTLY the picture this monitor's MOVED verdict exists to catch. Nothing connected the
// two before presets and tours were built, so a fleet that started patrolling would report
// tampering on every camera it patrolled, every few minutes — and the operator's fix is to
// turn tamper detection off, after which it protects nothing.

func newTamperRigWithPTZ(cfg TamperSettings) (*CameraTamperMonitor, *scriptedFrames, *capturedNotifier, *PTZJournal) {
	src := &scriptedFrames{}
	notif := &capturedNotifier{}
	rec := &tamperRecordingStub{configs: []*entities.RecordingConfig{{CameraId: 3, Enabled: true}}}
	journal := NewPTZJournal()
	m := NewCameraTamperMonitor(src, &fakeContinuityCamera{name: "Yard dome"}, rec,
		staticTamperSettings{cfg}, notif, nil).WithPTZ(journal)
	return m, src, notif, journal
}

// A commanded move must not read as tampering — and it must not merely be DEFERRED. The
// monitor forgets what it knew about the camera rather than suppressing the verdict for a
// while, because a suppression window ends with the old reference still in place and the
// new view still a long way from it.
func TestTamperIgnoresACameraWeMovedOurselves(t *testing.T) {
	cfg := testTamperCfg()
	normal := jpegOf(busyScene(31), t)
	wall := jpegOf(wallScene(32), t)

	m, src, notif, journal := newTamperRigWithPTZ(cfg)
	feed(t, m, src, normal, tamperBaselineSize, 100)

	// A preset recall, a jog or a tour step — all of them land here.
	journal.NoteCommandedMove(3)

	// Far more samples at the new view than FailureThreshold, and well past any plausible
	// settling window: if the reference had merely been suppressed, this would alert.
	feed(t, m, src, wall, 40, 300)
	if got := alertsTitled(notif.sent, "Camera view changed"); got != 0 {
		t.Fatalf("a camera we moved ourselves must not be reported as tampered with, got %d", got)
	}

	// The monitor is not blinded for good: the camera is re-aimed again by somebody else,
	// with no command from us, and that IS reported.
	feed(t, m, src, normal, 20, 900)
	if got := alertsTitled(notif.sent, "Camera view changed"); got != 1 {
		t.Fatalf("after settling at its new view the monitor must work again, got %d alerts", got)
	}
}

// A camera re-aimed onto a plain surface has legitimately lost its edge energy too, so the
// edge baseline has to be forgotten alongside the histogram reference. Leaving it in place
// turns every move onto a blank wall into a COVERED alert instead of a MOVED one — the same
// bug wearing a different label.
func TestTamperDoesNotReportACommandedMoveAsACoveredLens(t *testing.T) {
	cfg := testTamperCfg()
	normal := jpegOf(busyScene(33), t)
	blank := jpegOf(coveredScene(), t)

	m, src, notif, journal := newTamperRigWithPTZ(cfg)
	feed(t, m, src, normal, tamperBaselineSize, 100)
	journal.NoteCommandedMove(3)
	feed(t, m, src, blank, 10, 300)

	if got := alertsTitled(notif.sent, "Camera view blocked"); got != 0 {
		t.Fatalf("a commanded move onto a plain surface must not read as a covered lens, got %d", got)
	}
}

// A camera on PATROL is a different fact from one that was moved once. Its view is supposed
// to keep changing, so there is no "normal picture" to measure either scene verdict against
// — and a half-rebuilt reference made of six different stops is worse than none.
func TestTamperDoesNotJudgeTheSceneOfAPatrollingCamera(t *testing.T) {
	cfg := testTamperCfg()
	normal := jpegOf(busyScene(34), t)
	wall := jpegOf(wallScene(35), t)

	m, src, notif, journal := newTamperRigWithPTZ(cfg)
	feed(t, m, src, normal, tamperBaselineSize, 100)

	journal.SetTouring(3, true)
	feed(t, m, src, wall, 40, 300)
	if got := alertsTitled(notif.sent, "Camera view changed"); got != 0 {
		t.Fatalf("a patrolling camera must not be reported as re-aimed, got %d", got)
	}
	if got := alertsTitled(notif.sent, "Camera view blocked"); got != 0 {
		t.Fatalf("a patrolling camera must not be reported as covered, got %d", got)
	}

	// Stopping the patrol restores the verdict: the camera settles at one view, and being
	// re-aimed away from it is reported again.
	journal.SetTouring(3, false)
	feed(t, m, src, wall, tamperBaselineSize, 900)
	feed(t, m, src, normal, 20, 1200)
	if got := alertsTitled(notif.sent, "Camera view changed"); got != 1 {
		t.Fatalf("a camera that has stopped patrolling must be judged again, got %d alerts", got)
	}
}

// The journal is optional. An appliance with no PTZ wiring passes nil and every verdict has
// to behave exactly as it did before this interlock existed.
func TestTamperWithoutAPTZJournalIsUnchanged(t *testing.T) {
	cfg := testTamperCfg()
	normal := jpegOf(busyScene(36), t)
	wall := jpegOf(wallScene(37), t)

	m, src, notif := newTamperRig(nil, cfg)
	feed(t, m, src, normal, tamperBaselineSize, 100)
	feed(t, m, src, wall, 10, 300)
	if got := alertsTitled(notif.sent, "Camera view changed"); got != 1 {
		t.Fatalf("with no journal the moved verdict must still fire, got %d", got)
	}
}
