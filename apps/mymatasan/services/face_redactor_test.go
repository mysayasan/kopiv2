package services

import (
	"context"
	"strings"
	"testing"
)

// det builds a detection result with faces on the given frames.
func det(frames int, at map[int][][5]float64) faceDetectResult {
	res := faceDetectResult{Width: 640, Height: 480, FPS: 15, Frames: frames}
	for f := 0; f < frames; f++ {
		if boxes, ok := at[f]; ok {
			res.Detections = append(res.Detections, faceDetectionFrame{Frame: f, Boxes: boxes})
		}
	}
	return res
}

// THE ASSERTION THE WHOLE FEATURE RESTS ON.
//
// Detection flickers: YuNet finds a face in frames 100 and 102 and misses 101. Without the
// hold, that one frame is a full-resolution face in the middle of a clip nobody will scrub
// through — and it is the failure that makes naive face blurring worthless, because it looks
// like it worked.
func TestFaceCoverHoldsAcrossTheFramesDetectionMissed(t *testing.T) {
	res := det(20, map[int][][5]float64{
		10: {{0.4, 0.4, 0.1, 0.1, 0.9}},
		12: {{0.4, 0.4, 0.1, 0.1, 0.9}},
	})
	cover, report := planFaceCover(res, faceCoverOptions{HoldFrames: 3, Margin: 0})

	// Frame 11 has no detection of its own and MUST still be covered.
	if len(cover[11]) == 0 {
		t.Fatal("the frame between two detections was left uncovered — one visible frame is a disclosure")
	}
	for f := 7; f <= 15; f++ {
		if len(cover[f]) == 0 {
			t.Fatalf("frame %d should be covered by the hold either side of frames 10 and 12", f)
		}
	}
	if len(cover[6]) != 0 || len(cover[16]) != 0 {
		t.Fatal("the hold reached further than it should have")
	}
	if report.FacesFound != 2 {
		t.Fatalf("two detections, got %d", report.FacesFound)
	}
	if report.FramesObscured != 9 {
		t.Fatalf("frames 7..15 is nine frames, got %d", report.FramesObscured)
	}
}

// The detector returns a tight box around the features. A tight box leaves the jaw, the
// hairline and the ears — which identify somebody perfectly well — outside it.
func TestFaceCoverWidensTheDetectedBox(t *testing.T) {
	res := det(1, map[int][][5]float64{0: {{0.40, 0.40, 0.10, 0.20, 0.9}}})
	cover, _ := planFaceCover(res, faceCoverOptions{HoldFrames: 0, Margin: 0.25})
	box := cover[0][0]

	// 25% on EVERY side: the box grows by half its width in total and starts a quarter earlier.
	if box[0] > 0.3751 || box[0] < 0.3749 {
		t.Fatalf("x should move to 0.375, got %.4f", box[0])
	}
	if box[2] < 0.1499 || box[2] > 0.1501 {
		t.Fatalf("width should grow to 0.15, got %.4f", box[2])
	}
	if box[3] < 0.2999 || box[3] > 0.3001 {
		t.Fatalf("height should grow to 0.30, got %.4f", box[3])
	}
	// Nothing here may ever SHRINK a box: every expansion errs towards covering more,
	// because too much black is a complaint and too little is a disclosure.
	if box[0] > 0.40 || box[1] > 0.40 || box[2] < 0.10 || box[3] < 0.20 {
		t.Fatalf("the covered box is smaller than the detection: %+v", box)
	}
}

// A face half out of shot is still a face, and the visible half is the half that matters.
func TestFaceCoverClampsToTheFrameWithoutDroppingTheBox(t *testing.T) {
	res := det(1, map[int][][5]float64{0: {
		{-0.05, -0.05, 0.20, 0.20, 0.9}, // off the top-left corner
		{0.90, 0.90, 0.20, 0.20, 0.9},   // off the bottom-right corner
	}})
	cover, _ := planFaceCover(res, faceCoverOptions{HoldFrames: 0, Margin: 0.2})
	if len(cover[0]) != 2 {
		t.Fatalf("an off-edge face must still be covered, got %d box(es)", len(cover[0]))
	}
	for _, box := range cover[0] {
		if box[0] < 0 || box[1] < 0 {
			t.Fatalf("box starts outside the frame: %+v", box)
		}
		if box[0]+box[2] > 1.0001 || box[1]+box[3] > 1.0001 {
			t.Fatalf("box runs past the frame: %+v", box)
		}
		if box[2] <= 0 || box[3] <= 0 {
			t.Fatalf("box was clamped out of existence: %+v", box)
		}
	}
}

// The hold must not invent frames the renderer will never reach: the report would then count
// frames it did not obscure, and the manifest would overstate what happened.
func TestFaceCoverDoesNotRunPastTheEndOfTheFootage(t *testing.T) {
	res := det(5, map[int][][5]float64{4: {{0.1, 0.1, 0.1, 0.1, 0.9}}})
	cover, report := planFaceCover(res, faceCoverOptions{HoldFrames: 3, Margin: 0})
	for f := range cover {
		if f < 0 || f > 4 {
			t.Fatalf("cover created for frame %d, which does not exist", f)
		}
	}
	if report.FramesObscured != 4 {
		t.Fatalf("frames 1..4 is four frames, got %d", report.FramesObscured)
	}
}

// A clip with no faces produces no cover — and that is a true statement worth making, not an
// error. What must never happen is the opposite.
func TestFaceCoverOfAClipWithNoFaces(t *testing.T) {
	cover, report := planFaceCover(det(30, nil), defaultFaceCoverOptions())
	if len(cover) != 0 || report.FacesFound != 0 || report.FramesObscured != 0 {
		t.Fatalf("want nothing covered, got %d frame(s) / %+v", len(cover), report)
	}
	if report.FramesScanned != 30 {
		t.Fatalf("the report must still say what was scanned, got %d", report.FramesScanned)
	}
}

// An appliance that cannot obscure faces must REFUSE, not quietly export a bundle that did
// not. This is the shape W3-6's bench found one item ago — a redact flag accepted and
// dropped — and it is invisible exactly when it matters.
func TestFaceRedactionRefusesWhenItCannotRun(t *testing.T) {
	cases := map[string]*FaceRedactor{
		"no worker script": NewFaceRedactor("python", "", "yunet.onnx", func() string { return "ffmpeg" }, nil),
		"no detector model": NewFaceRedactor("python", "face_redactor_test.go", "/nope/yunet.onnx",
			func() string { return "ffmpeg" }, nil),
		"no ffmpeg": NewFaceRedactor("python", "face_redactor_test.go", "face_redactor_test.go",
			func() string { return "" }, nil),
	}
	for name, r := range cases {
		err := r.Available()
		if err == nil {
			t.Fatalf("%s: expected a refusal", name)
		}
		if !strings.Contains(err.Error(), "cannot obscure faces") {
			t.Fatalf("%s: the refusal must say what it is about: %v", name, err)
		}
	}
	// ...and an appliance that has everything says so.
	ok := NewFaceRedactor("python", "face_redactor_test.go", "face_redactor_test.go",
		func() string { return "ffmpeg" }, nil)
	if err := ok.Available(); err != nil {
		t.Fatalf("a complete install refused: %v", err)
	}
}

// The export must refuse at REQUEST time, while the operator is still looking at the form —
// not ten minutes into a job, and above all not by handing back a bundle that did nothing.
func TestExportRefusesFaceRedactionUpFront(t *testing.T) {
	svc := &evidenceExportService{faces: nil}
	_, err := svc.Create(context.Background(), ExportRequest{
		CameraId: 1, From: 1, To: 2, Reason: "disclosure", BlurFaces: true,
	})
	if err == nil {
		t.Fatal("an appliance with no face redactor accepted a face-redacted export")
	}
	if !strings.Contains(err.Error(), "cannot obscure faces") {
		t.Fatalf("the refusal must say why: %v", err)
	}
}

// THE MANIFEST IS WHERE THE HONESTY LIVES. A face pass is not a guarantee and the block must
// never be readable as one.
func TestFaceManifestStatesItsLimits(t *testing.T) {
	plan := &redactionPlan{faces: true}
	man := plan.manifestWith(&FaceRedactionReport{
		FramesScanned: 900, FacesFound: 412, FramesObscured: 380, HoldFrames: 3, MarginPercent: 28,
	})
	if man.Faces == nil || !man.Faces.Applied {
		t.Fatal("want a faces block")
	}
	if man.Faces.FramesScanned != 900 || man.Faces.FacesObscured != 412 || man.Faces.FramesObscured != 380 {
		t.Fatalf("the counts must be what happened: %+v", man.Faces)
	}
	lim := man.Faces.Limitation
	for _, want := range []string{"NOT a guarantee", "miss faces", "not the number of people"} {
		if !strings.Contains(lim, want) {
			t.Fatalf("the limitation must say %q: %s", want, lim)
		}
	}
	// The safety margins are reported, so a recipient can see that MORE than the detected
	// box was covered rather than having to take the word "obscured" on trust.
	if man.Faces.HoldFrames != 3 || man.Faces.MarginPercent != 28 {
		t.Fatalf("the margins must be in the manifest: %+v", man.Faces)
	}
	if !strings.Contains(man.Method, "every face the detector found") {
		t.Fatalf("the method must say what was covered: %q", man.Method)
	}
	if !strings.Contains(man.Note, "DERIVATIVE") {
		t.Fatalf("a face-redacted bundle is still a derivative: %q", man.Note)
	}
}

// Zones and faces in one bundle: both are named, and neither claim is folded into the other.
func TestManifestKeepsZonesAndFacesApart(t *testing.T) {
	plan := &redactionPlan{names: []string{"Window"}, faces: true}
	man := plan.manifestWith(&FaceRedactionReport{FramesScanned: 10})
	if len(man.Regions) != 1 || man.Regions[0] != "Window" {
		t.Fatalf("the zone must still be named: %+v", man.Regions)
	}
	if man.Faces == nil {
		t.Fatal("the face pass must be reported separately from the zones")
	}
	if !strings.Contains(man.Method, "the listed regions") || !strings.Contains(man.Method, "every face") {
		t.Fatalf("the method must mention both: %q", man.Method)
	}
}

// THE BUG FOUND BY RUNNING THE WORKER IN THE BENCH IMAGE AND READING ITS OUTPUT.
//
// OpenCV writes its own warnings to stderr — the bench image emits
// "[ WARN:0@0.07] global net_impl_backend.cpp ... Targets are not supported" on every run —
// and the worker's report is written to the same stream. Treating stderr as pure JSON turned
// a completely successful export into "the worker did not report what it wrote".
//
// SCOPE, stated so nobody reads more into this than it proves: this exercises the PARSER,
// not the call site. Reverting render() to a naive whole-buffer unmarshal leaves this test
// green. What guards the wiring is the live bench, which runs the real worker in the image
// that emits the warning — and that is the only thing that can guard it, short of spawning
// processes from a unit test.
func TestWorkerReportSurvivesLibraryNoiseOnStderr(t *testing.T) {
	var tail struct {
		Frames int    `json:"frames"`
		Error  string `json:"error"`
	}
	noisy := "[ WARN:0@0.072] global net_impl_backend.cpp:345 setPreferableTarget not supported" +
		LF + `{"frames": 45, "boxesDrawn": 303}` + LF
	if !parseWorkerTail(noisy, &tail) {
		t.Fatal("a library warning on stderr hid the worker's report")
	}
	if tail.Frames != 45 {
		t.Fatalf("wrong report parsed: %+v", tail)
	}
	// A stream with no report at all must still be recognised as having none, rather than
	// silently yielding a zero frame count that the truncation check would then blame on the
	// video.
	if parseWorkerTail("[ WARN ] something went wrong"+LF+"not json either"+LF, &tail) {
		t.Fatal("a stream with no report was accepted as one")
	}
}

// LF keeps the newline out of the literals above, where an escape sequence has already been
// mangled once by the tooling that wrote this file.
const LF = "\n"
