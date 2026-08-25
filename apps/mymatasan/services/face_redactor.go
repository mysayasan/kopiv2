package services

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/mysayasan/kopiv2/infra/procutil"
)

// Face redaction on export (W3-6b).
//
// A privacy zone is a fixed region of a camera's view, so burning it into an export is one
// static filter over the whole clip (see evidence_export.go). A FACE is not fixed: it moves,
// it turns away, it leaves and comes back. Obscuring faces therefore means looking at every
// frame, which is a different order of cost and — more importantly — a different KIND of
// claim.
//
// THE CLAIM THIS FEATURE CAN HONESTLY MAKE, and the one it must not:
//
// A privacy zone is a guarantee. The region was named by a human, it does not move, and the
// export covers it. **Face redaction is not a guarantee and cannot be made into one.** A
// detector misses faces — in profile, at distance, partly occluded, motion-blurred, or simply
// because a frame was hard. Anything in the product that implies "no faces are visible in
// this file" would be a claim nobody can stand behind, so the manifest says what was actually
// done — this many frames scanned, this many faces obscured — and says in words that faces
// may remain. Somebody handing a bundle to a journalist has to know that before they hand it
// over, not after.
//
// WHAT IS DONE ABOUT THE MISSES WE CAN DO SOMETHING ABOUT. Detection flickers: YuNet finds a
// face in frames 100 and 102 and misses 101. That single frame is a full-resolution face in
// the middle of a clip nobody will scrub through, and it is the failure mode that makes naive
// face blurring worthless. So every detection is HELD across the frames either side of it and
// WIDENED beyond the box the detector returned. Both directions err towards covering more,
// which is the only safe direction for a privacy control: too much black is a complaint, too
// little is a disclosure.
//
// That arithmetic lives HERE, in Go, rather than in the Python worker — it is the difference
// between a face being covered and a face being visible, so it belongs where it can be
// unit-tested and mutation-checked. The worker does only the two things that genuinely need
// OpenCV: find faces, and fill rectangles.

const (
	// faceHoldFrames is how many frames either side of a detection are also covered.
	//
	// Three at 15fps is a fifth of a second. It is chosen against the FLICKER, not against
	// motion: a face that the detector drops for a couple of frames is common, and a face
	// that moves far enough in 200ms to escape a box widened by faceMarginFraction is rare.
	faceHoldFrames = 3
	// faceMarginFraction widens each detected box by this fraction of its own size on every
	// side. YuNet returns a tight box around the facial features; a tight box leaves the
	// jaw, the hairline and the ears — which identify somebody perfectly well — outside it.
	faceMarginFraction = 0.28
	// faceScoreThreshold is the detector confidence below which a candidate is ignored.
	//
	// Deliberately LOWER than the 0.7 the face-RECOGNITION path uses. Recognition asks "is
	// this person Ahmad?", where a weak detection produces a wrong name and a false accusation.
	// Redaction asks "might this be a face?", where a weak detection costs a black rectangle
	// over something that was not one. The two questions want opposite errors.
	faceScoreThreshold = 0.4
	// faceDetectTimeout bounds the scanning pass. It is generous because the pass is the
	// expensive half and an export is already an asynchronous job somebody polls.
	faceDetectTimeout = 45 * time.Minute
	// faceRenderTimeout bounds the render+encode pass.
	faceRenderTimeout = 45 * time.Minute
)

// ErrFaceRedactionUnavailable is returned when this appliance cannot obscure faces at all.
//
// It is an ERROR rather than a silent downgrade, and that is the whole point: an export asked
// to hide faces must never come back as a bundle that did not. W3-6's bench found exactly
// that shape one item ago — a redact flag the API accepted and dropped — and the failure is
// invisible precisely when it matters, because nobody scrubs a five-minute clip to check.
var ErrFaceRedactionUnavailable = errors.New("this appliance cannot obscure faces")

// FaceRedactionReport is what actually happened, for the manifest and the screen.
type FaceRedactionReport struct {
	// FramesScanned is every frame the detector looked at. It equals the frames written, or
	// the export fails — see the check in Render.
	FramesScanned int `json:"framesScanned"`
	// FacesFound is the number of detections, summed over frames. It is NOT a count of
	// people: one person present for 900 frames is up to 900 detections, and the manifest
	// says so rather than inviting the reader to infer a crowd.
	FacesFound int `json:"facesFound"`
	// FramesObscured is how many frames had at least one rectangle filled, AFTER the hold
	// either side of each detection.
	FramesObscured int `json:"framesObscured"`
	// HoldFrames / MarginPercent record the two safety margins, so a recipient can see that
	// more than the detected box was covered.
	HoldFrames    int `json:"holdFrames"`
	MarginPercent int `json:"marginPercent"`
}

// faceCoverOptions are the two safety margins, as parameters so a test can vary them.
type faceCoverOptions struct {
	HoldFrames int
	Margin     float64
}

func defaultFaceCoverOptions() faceCoverOptions {
	return faceCoverOptions{HoldFrames: faceHoldFrames, Margin: faceMarginFraction}
}

// faceDetectionFrame is one frame's detections as the worker reports them: normalised
// [x, y, w, h, score].
type faceDetectionFrame struct {
	Frame int          `json:"f"`
	Boxes [][5]float64 `json:"b"`
}

// faceDetectResult is the whole scanning pass.
type faceDetectResult struct {
	Width        int                  `json:"width"`
	Height       int                  `json:"height"`
	FPS          float64              `json:"fps"`
	Frames       int                  `json:"frames"`
	FailedFrames int                  `json:"failedFrames"`
	Detections   []faceDetectionFrame `json:"detections"`
	Error        string               `json:"error"`
}

// faceCoverDoc is what the renderer reads: per-frame boxes to fill, normalised [x,y,w,h].
type faceCoverDoc struct {
	Frames map[string][][4]float64 `json:"frames"`
}

// planFaceCover turns detections into the rectangles that will actually be filled.
//
// TWO EXPANSIONS, and each exists because of a specific way a face escapes:
//
//   - IN SPACE, by Margin: the detector's box is tight around the features and leaves the
//     jaw, hairline and ears outside it, which identify a person perfectly well.
//   - IN TIME, by HoldFrames: detection flickers. A face found at frame 100 and 102 but not
//     101 leaves one full-resolution frame in the middle of the clip, and that is the failure
//     that makes naive face blurring worthless.
//
// Both round outwards and clamp to the frame. Nothing here ever shrinks a box.
func planFaceCover(res faceDetectResult, opts faceCoverOptions) (map[int][][4]float64, FaceRedactionReport) {
	cover := map[int][][4]float64{}
	report := FaceRedactionReport{
		FramesScanned: res.Frames,
		HoldFrames:    opts.HoldFrames,
		MarginPercent: int(math.Round(opts.Margin * 100)),
	}
	hold := opts.HoldFrames
	if hold < 0 {
		hold = 0
	}
	for _, frame := range res.Detections {
		for _, raw := range frame.Boxes {
			report.FacesFound++
			box := widenFaceBox(raw, opts.Margin)
			if box[2] <= 0 || box[3] <= 0 {
				continue
			}
			from := frame.Frame - hold
			if from < 0 {
				from = 0
			}
			to := frame.Frame + hold
			// A detection on the last frame must not create cover for frames that do not
			// exist: the renderer would never reach them, and the report would count a
			// frame it did not obscure.
			if res.Frames > 0 && to > res.Frames-1 {
				to = res.Frames - 1
			}
			for f := from; f <= to; f++ {
				cover[f] = append(cover[f], box)
			}
		}
	}
	report.FramesObscured = len(cover)
	return cover, report
}

// widenFaceBox grows a normalised box by margin on every side and clamps it to the frame.
func widenFaceBox(raw [5]float64, margin float64) [4]float64 {
	x, y, w, h := raw[0], raw[1], raw[2], raw[3]
	if margin < 0 {
		margin = 0
	}
	x -= w * margin
	y -= h * margin
	w += w * margin * 2
	h += h * margin * 2
	// Clamp to the frame rather than dropping an off-edge box: a face half out of shot is
	// still a face, and the visible half is the half that matters.
	if x < 0 {
		w += x
		x = 0
	}
	if y < 0 {
		h += y
		y = 0
	}
	if x+w > 1 {
		w = 1 - x
	}
	if y+h > 1 {
		h = 1 - y
	}
	return [4]float64{x, y, w, h}
}

// FaceRedactor runs the two worker passes and the encode.
type FaceRedactor struct {
	python     string
	script     string
	yunetPath  string
	ffmpegPath func() string
	opts       faceCoverOptions
	logf       func(string, ...any)
}

// NewFaceRedactor builds the redactor. It constructs even when nothing is installed; the
// refusal happens in Available, with a message that says what to install.
func NewFaceRedactor(python, script, yunetPath string, ffmpegPath func() string, logf func(string, ...any)) *FaceRedactor {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &FaceRedactor{
		python: defaultStr(python, "python"), script: script, yunetPath: yunetPath,
		ffmpegPath: ffmpegPath, opts: defaultFaceCoverOptions(), logf: logf,
	}
}

// Available reports whether this appliance can obscure faces, and says what is missing when
// it cannot.
//
// Called BEFORE an export starts, so the refusal reaches the operator at the moment they ask
// rather than as a failed job ten minutes later — and so that no path exists by which a
// bundle is marked face-redacted without the detector having run.
func (r *FaceRedactor) Available() error {
	if r == nil {
		return fmt.Errorf("%w: face redaction is not wired up on this build", ErrFaceRedactionUnavailable)
	}
	if strings.TrimSpace(r.script) == "" || !faceFileExists(r.script) {
		return fmt.Errorf("%w: the face redaction worker is missing from this install", ErrFaceRedactionUnavailable)
	}
	if !faceFileExists(r.yunetPath) {
		return fmt.Errorf("%w: the face detection model is not installed — run the face-recognition setup", ErrFaceRedactionUnavailable)
	}
	if r.ffmpegPath == nil || strings.TrimSpace(r.ffmpegPath()) == "" {
		return fmt.Errorf("%w: no ffmpeg is configured — set it in Settings", ErrFaceRedactionUnavailable)
	}
	return nil
}

// Render scans in for faces, then writes out with every face — and every privacy zone — filled.
//
// The zones ride along in the SAME encode rather than getting a pass of their own: a second
// re-encode would degrade the picture twice for no benefit, and an export that is already a
// declared derivative does not need to become one twice over.
func (r *FaceRedactor) Render(ctx context.Context, in, out, workDir string, zones []PrivacyRegion) (FaceRedactionReport, error) {
	var report FaceRedactionReport
	if err := r.Available(); err != nil {
		return report, err
	}

	det, err := r.detect(ctx, in, workDir)
	if err != nil {
		return report, err
	}
	// A FRAME THE DETECTOR COULD NOT SCAN IS NOT A FRAME WITH NO FACES. A partial scan that
	// produces a bundle labelled face-redacted is the worst outcome available here: it looks
	// complete, and the frames nobody scanned are exactly the ones nobody will check.
	if det.FailedFrames > 0 {
		return report, fmt.Errorf("face detection could not scan %d of %d frames, so this export cannot honestly be called face-redacted", det.FailedFrames, det.Frames)
	}
	if det.Frames == 0 {
		return report, errors.New("face detection found no frames in the footage")
	}

	cover, report := planFaceCover(det, r.opts)
	coverPath := filepath.Join(workDir, "face-cover.json")
	doc := faceCoverDoc{Frames: make(map[string][][4]float64, len(cover))}
	for frame, boxes := range cover {
		doc.Frames[fmt.Sprintf("%d", frame)] = boxes
	}
	blob, err := json.Marshal(doc)
	if err != nil {
		return report, err
	}
	if err := os.WriteFile(coverPath, blob, 0o600); err != nil {
		return report, err
	}
	defer os.Remove(coverPath)

	written, err := r.render(ctx, in, out, coverPath, zones)
	if err != nil {
		return report, err
	}
	// THE TRUNCATION CHECK. A render that stopped early produces a shorter file that plays
	// perfectly and simply ends — and the frames it never wrote are the ones nobody sees are
	// missing. The scanning pass counted the frames; the render must have written the same
	// number.
	if written != det.Frames {
		return report, fmt.Errorf("the redacted video is %d frames but the footage is %d — the export was truncated and has not been kept", written, det.Frames)
	}
	return report, nil
}

// detect runs the scanning pass and reads back what it found.
func (r *FaceRedactor) detect(ctx context.Context, in, workDir string) (faceDetectResult, error) {
	var res faceDetectResult
	outPath := filepath.Join(workDir, "face-detections.json")
	defer os.Remove(outPath)

	cctx, cancel := context.WithTimeout(ctx, faceDetectTimeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, r.python, r.script,
		"--detect", in, "--yunet", r.yunetPath, "--out", outPath,
		"--score", fmt.Sprintf("%.2f", faceScoreThreshold))
	procutil.HideWindow(cmd)
	stdout, err := cmd.Output()
	if err != nil {
		return res, fmt.Errorf("scanning the footage for faces failed: %w", err)
	}
	// The worker prints a summary and writes the bulk to a file, so a long clip's detections
	// never have to survive a pipe.
	var summary struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(stdout, &summary) == nil && summary.Error != "" {
		return res, fmt.Errorf("scanning the footage for faces failed: %s", summary.Error)
	}
	blob, err := os.ReadFile(outPath)
	if err != nil {
		return res, fmt.Errorf("the face detector produced no result: %w", err)
	}
	if err := json.Unmarshal(blob, &res); err != nil {
		return res, fmt.Errorf("the face detector's result was unreadable: %w", err)
	}
	if res.Error != "" {
		return res, fmt.Errorf("scanning the footage for faces failed: %s", res.Error)
	}
	return res, nil
}

// parseWorkerTail pulls the worker's own JSON report out of a stderr stream that may also
// carry a library's warnings. It reads BACKWARDS: the report is written last, after the final
// frame, so the last decodable line is the one that describes the run.
func parseWorkerTail(stderr string, into any) bool {
	lines := strings.Split(strings.TrimSpace(stderr), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(line, "{") {
			continue
		}
		if json.Unmarshal([]byte(line), into) == nil {
			return true
		}
	}
	return false
}

// render streams redacted frames from the worker into ffmpeg, which encodes them and muxes
// the ORIGINAL audio back in.
//
// The audio comes from the source rather than through the worker because the worker only
// deals in pictures — and because an evidence bundle that silently lost its audio would be a
// worse derivative than one that says it was re-encoded.
func (r *FaceRedactor) render(ctx context.Context, in, out, coverPath string, zones []PrivacyRegion) (int, error) {
	cctx, cancel := context.WithTimeout(ctx, faceRenderTimeout)
	defer cancel()

	worker := exec.CommandContext(cctx, r.python, r.script, "--render", in, "--cover", coverPath)
	procutil.HideWindow(worker)
	var workerErr strings.Builder
	worker.Stderr = &workerErr
	pipe, err := worker.StdoutPipe()
	if err != nil {
		return 0, err
	}
	if err := worker.Start(); err != nil {
		return 0, fmt.Errorf("starting the face redaction worker failed: %w", err)
	}
	defer func() { _ = worker.Process.Kill() }()

	// ONE header line, then raw frames. Reading it here is what lets the encoder be started
	// with the right size and rate without ffprobe, which this appliance's ffmpeg install
	// does not guarantee is present.
	reader := bufio.NewReaderSize(pipe, 1<<20)
	line, err := reader.ReadString('\n')
	if err != nil {
		_ = worker.Wait()
		return 0, fmt.Errorf("the face redaction worker produced nothing: %s", strings.TrimSpace(workerErr.String()))
	}
	var header struct {
		Width  int     `json:"width"`
		Height int     `json:"height"`
		FPS    float64 `json:"fps"`
		Error  string  `json:"error"`
	}
	if err := json.Unmarshal([]byte(line), &header); err != nil || header.Error != "" || header.Width <= 0 || header.Height <= 0 {
		_ = worker.Wait()
		return 0, fmt.Errorf("the face redaction worker could not open the footage: %s", strings.TrimSpace(line))
	}

	args := []string{
		"-hide_banner", "-loglevel", "error",
		"-f", "rawvideo", "-pix_fmt", "bgr24",
		"-s", fmt.Sprintf("%dx%d", header.Width, header.Height),
		"-r", fmt.Sprintf("%.4f", header.FPS),
		"-i", "pipe:0",
		// The source again, for its audio only.
		"-i", filepath.ToSlash(in),
	}
	if filter := redactionFilter(zones); filter != "" {
		args = append(args, "-vf", filter)
	}
	args = append(args,
		"-map", "0:v:0",
		// Optional: a camera with no audio track must not fail the export.
		"-map", "1:a:0?",
		"-c:v", "libx264", "-preset", "veryfast", "-crf", "18",
		"-c:a", "copy",
		"-movflags", "+faststart", "-f", "mp4", "-y", filepath.ToSlash(out))

	enc := exec.CommandContext(cctx, strings.TrimSpace(r.ffmpegPath()), args...)
	procutil.HideWindow(enc)
	var encErr strings.Builder
	enc.Stderr = &encErr
	encIn, err := enc.StdinPipe()
	if err != nil {
		return 0, err
	}
	if err := enc.Start(); err != nil {
		return 0, fmt.Errorf("starting the encoder failed: %w", err)
	}

	copyErr := make(chan error, 1)
	go func() {
		_, cerr := io.Copy(encIn, reader)
		_ = encIn.Close()
		copyErr <- cerr
	}()

	workerWait := worker.Wait()
	pipeErr := <-copyErr
	encWait := enc.Wait()

	if workerWait != nil {
		return 0, fmt.Errorf("the face redaction worker failed: %v: %s", workerWait, strings.TrimSpace(workerErr.String()))
	}
	if encWait != nil {
		return 0, fmt.Errorf("encoding the redacted footage failed: %v: %s", encWait, strings.TrimSpace(encErr.String()))
	}
	if pipeErr != nil {
		return 0, fmt.Errorf("streaming the redacted frames to the encoder failed: %w", pipeErr)
	}

	// The worker reports what it actually wrote, on stderr, after the last frame. This is the
	// number the truncation check compares against the scan.
	var tail struct {
		Frames int    `json:"frames"`
		Error  string `json:"error"`
	}
	// The LAST JSON line, not the whole buffer. OpenCV writes its own warnings to stderr —
	// "[ WARN:0@0.07] global net_impl_backend.cpp ... Targets are not supported" is one this
	// bench image emits every run — and treating stderr as pure JSON turns a successful
	// export into "the worker did not report what it wrote". Found by running the worker in
	// the bench image and reading its output rather than only its exit code.
	if !parseWorkerTail(workerErr.String(), &tail) {
		return 0, fmt.Errorf("the face redaction worker did not report what it wrote: %s", strings.TrimSpace(workerErr.String()))
	}
	if tail.Error != "" {
		return 0, fmt.Errorf("the face redaction worker failed: %s", tail.Error)
	}
	return tail.Frames, nil
}
