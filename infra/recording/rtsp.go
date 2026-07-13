package recording

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mysayasan/kopiv2/infra/atrest"
	"github.com/mysayasan/kopiv2/infra/procutil"
	"github.com/mysayasan/kopiv2/infra/safego"
	"github.com/mysayasan/kopiv2/infra/telemetry"
)

// fileIsEncrypted reports whether path begins with the atrest header magic.
func fileIsEncrypted(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	head := make([]byte, atrest.HeaderLen)
	n, _ := io.ReadFull(f, head)
	return atrest.IsEncrypted(head[:n])
}

// segTimeFormat is the strftime/Go time layout used for live segment filenames.
const segTimeFormat = "20060102_150405"

// partSuffix is appended to a segment's MP4 while it is being built. The finished
// file only ever appears at its real name via an atomic rename, and only after it
// has been encrypted, so a bare <stem>.mp4 on disk is ALWAYS a complete, encrypted
// segment. That invariant is what makes crash recovery unambiguous: if a <stem>.ts
// still exists, the stem was never finalized and the TS is the source of truth.
const partSuffix = ".part"

// maxRemuxAttempts bounds how many times a segment is re-remuxed before its source
// TS is quarantined. Without a cap, a permanently unreadable TS would respawn an
// ffmpeg process on every 5 s watch tick forever.
const maxRemuxAttempts = 3

// remuxDrainTimeout bounds how long Close waits for in-flight segment finalization.
// Cancelling the recorder context kills the child ffmpeg, so the real wait is
// sub-second; this is only a backstop against a wedged child hanging shutdown.
const remuxDrainTimeout = 10 * time.Second

// remuxOutcome is the result of finalizing one segment.
type remuxOutcome int

const (
	remuxSaved     remuxOutcome = iota // finalized on disk and persisted to the sink
	remuxDiscarded                     // empty/absent segment, intentionally dropped
	remuxFailed                        // could not finalize; source TS intact, retry later
	remuxUnsaved                       // MP4 finalized on disk but the sink save failed;
	                                   // the adoption path retries persistence, so this is
	                                   // not a segment fault and burns no retry attempt
)

// String is the metric label for a finalize outcome. Keep these values stable — they are
// a bounded label set on kopiv2_recording_segment_finalize_total.
func (o remuxOutcome) String() string {
	switch o {
	case remuxSaved:
		return "saved"
	case remuxDiscarded:
		return "discarded"
	case remuxUnsaved:
		return "unsaved"
	default:
		return "failed"
	}
}

// liveSegInfo describes one live segment.  path is the best available file for
// that segment: .mp4 if the TS has been remuxed, otherwise .ts.  tsPath is set
// while the raw TS file still exists on disk.
type liveSegInfo struct {
	stem      string
	path      string // .mp4 preferred; .ts while being written or waiting for remux
	tsPath    string // non-empty while the .ts file exists
	startedAt int64
}

// rtspRecorder continuously records an RTSP stream into rolling live segments
// (MPEG-TS while recording, remuxed to MP4 once complete) and slices event
// clips on demand.
type rtspRecorder struct {
	cfg     RecorderConfig
	sink    SegmentSink
	liveDir string
	clipDir string

	mu              sync.Mutex
	cancel          context.CancelFunc
	ffmpegRunning   bool
	lastErrMsg      string
	activeStreamURL string // primary or fallback, whichever is currently in use
	usingFallback   bool
	// hwAccelOff is set at runtime after the hardware decoder fails repeatedly (most
	// often the GPU is out of NVDEC decode sessions); the tee then falls back to
	// software decode for this camera so detection keeps working.
	hwAccelOff bool

	// Siphon latest-frame buffer: the most recent decoded JPEG teed off the
	// recording ffmpeg, for the AI detector to read off the recording stream.
	frameMu     sync.RWMutex
	lastFrame   []byte
	lastFrameAt int64 // unix seconds the frame was decoded

	// Segment finalization bookkeeping. segDone marks stems already persisted (or
	// deliberately discarded); segBusy marks stems with finalization in flight, so the
	// 5 s watch tick never launches a second remux for a stem already being remuxed;
	// segFails counts consecutive failures per stem to drive the quarantine cap.
	// remuxWG lets Close wait for in-flight remuxes — Manager.Configure closes the old
	// recorder and immediately starts a new one on the SAME live directory, so without
	// the wait two ffmpeg processes could write the same output file concurrently.
	segMu    sync.Mutex
	segDone  map[string]bool
	segBusy  map[string]bool
	segFails map[string]int
	remuxWG  sync.WaitGroup
}

func newRTSPRecorder(cfg RecorderConfig, sink SegmentSink) *rtspRecorder {
	return &rtspRecorder{
		cfg:      cfg,
		sink:     sink,
		segDone:  map[string]bool{},
		segBusy:  map[string]bool{},
		segFails: map[string]int{},
	}
}

func (r *rtspRecorder) WriteFrame(_ []byte, _ int64) {}

// readSiphonFrames drains the MJPEG image2pipe output, splitting the concatenated
// JPEG stream on SOI (FF D8) / EOI (FF D9) markers and keeping only the most
// recent complete frame. It always reads to completion so the pipe never blocks
// the recording ffmpeg.
func (r *rtspRecorder) readSiphonFrames(stdout io.ReadCloser) {
	defer stdout.Close()
	br := bufio.NewReaderSize(stdout, 1<<16)
	const maxFrame = 4 << 20
	var buf []byte
	inFrame := false
	var prev byte
	for {
		b, err := br.ReadByte()
		if err != nil {
			return
		}
		if inFrame {
			buf = append(buf, b)
			if prev == 0xFF && b == 0xD9 { // EOI: complete frame
				frame := make([]byte, len(buf))
				copy(frame, buf)
				r.frameMu.Lock()
				r.lastFrame = frame
				r.lastFrameAt = time.Now().Unix()
				r.frameMu.Unlock()
				inFrame, prev = false, 0
				buf = buf[:0]
				continue
			}
			if len(buf) > maxFrame { // runaway guard
				inFrame, prev = false, 0
				buf = buf[:0]
				continue
			}
		} else if prev == 0xFF && b == 0xD8 { // SOI: frame start
			inFrame = true
			buf = append(buf[:0], 0xFF, 0xD8)
		}
		prev = b
	}
}

// latestFrame returns a copy of the most recent siphoned JPEG frame and the unix
// second it was decoded, or ok=false when no frame has been captured yet.
func (r *rtspRecorder) latestFrame() ([]byte, int64, bool) {
	r.frameMu.RLock()
	defer r.frameMu.RUnlock()
	if len(r.lastFrame) == 0 {
		return nil, 0, false
	}
	out := make([]byte, len(r.lastFrame))
	copy(out, r.lastFrame)
	return out, r.lastFrameAt, true
}

// recordingSource returns the recording stream URI, fallback URI, and transport so
// the detector can grab a standalone frame from the same stream it records.
func (r *rtspRecorder) recordingSource() (string, string, string) {
	return r.cfg.RTSPURI, r.cfg.FallbackRTSPURI, r.cfg.RTSPTransport
}

// TriggerEvent waits for the post-roll window then slices an event clip from
// the already-recorded live segments. frameCapturedAt is the Unix second
// timestamp of the frame that triggered the alert; the clip window is anchored
// to that moment so YOLO inference latency does not shift the recording.
func (r *rtspRecorder) TriggerEvent(alertId int64, frameCapturedAt int64) {
	// Detect-only recorders keep no segments, so there is nothing to slice into an
	// event clip — the alert simply has no recorded footage (expected when NVR
	// recording is off).
	if r.cfg.DetectOnly {
		return
	}
	post := postRoll(r.cfg)
	triggerAt := time.Now().UTC()
	if frameCapturedAt > 0 {
		triggerAt = time.Unix(frameCapturedAt, 0).UTC()
	}
	go r.extractClip(alertId, triggerAt, time.Duration(post)*time.Second)
}

// Close stops the recorder and waits for in-flight segment finalization to unwind.
//
// The wait is load-bearing: Manager.Configure closes the old recorder and immediately
// starts a new one on the SAME live directory, so without it a remux from the old
// generation would still be writing (and deleting) files the new recorder now owns —
// two ffmpeg processes racing the same segment. Cancelling the context kills the child
// ffmpeg, so in practice this returns in milliseconds; the timeout is only a backstop.
func (r *rtspRecorder) Close() {
	r.mu.Lock()
	cancel := r.cancel
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}

	done := make(chan struct{})
	go func() {
		r.remuxWG.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(remuxDrainTimeout):
		log.Printf("recording rtsp cam%d: timed out waiting for segment finalization to stop", r.cfg.CameraId)
	}
}

// cleanStaleParts removes partial MP4s left by an interrupted remux. Their source TS
// files are still on disk and will be re-remuxed, so the partials are pure garbage.
func (r *rtspRecorder) cleanStaleParts() {
	entries, err := os.ReadDir(r.liveDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), partSuffix) {
			_ = os.Remove(filepath.Join(r.liveDir, e.Name()))
		}
	}
}

func (r *rtspRecorder) cameraStatus() CameraStatus {
	// Detect-only recorders write no segments, so skip the live-dir scan and report
	// zero files with a distinct mode so the recordings UI shows no phantom segments.
	var segs []liveSegInfo
	if !r.cfg.DetectOnly {
		segs = r.listLiveSegments()
	}
	r.mu.Lock()
	running := r.ffmpegRunning
	errMsg := r.lastErrMsg
	activeURL := r.activeStreamURL
	usingFallback := r.usingFallback
	r.mu.Unlock()

	state := "stopped"
	if running {
		state = "streaming"
	} else if errMsg != "" {
		state = "error"
	}

	mode := "rtsp"
	if r.cfg.DetectOnly {
		mode = "detect"
	}
	return CameraStatus{
		CameraId:           r.cfg.CameraId,
		Mode:               mode,
		State:              state,
		FFmpegRunning:      running,
		LiveFiles:          len(segs),
		LiveDir:            r.liveDir,
		LastError:          errMsg,
		ActiveStreamURL:    activeURL,
		UsingFallback:      usingFallback,
		RingBufferFrames:   len(segs),
		RingBufferCapacity: -1,
	}
}

// Start launches the continuous ffmpeg recording process and background workers.
func (r *rtspRecorder) Start(ctx context.Context) error {
	ffmpegPath, err := resolveFFmpeg(r.cfg.FFmpegPath)
	if err != nil {
		return err
	}

	absStorage, err := filepath.Abs(r.cfg.StoragePath)
	if err != nil {
		absStorage = r.cfg.StoragePath
	}
	r.liveDir = filepath.Join(absStorage, fmt.Sprintf("cam%d", r.cfg.CameraId), "live")
	r.clipDir = filepath.Join(absStorage, fmt.Sprintf("cam%d", r.cfg.CameraId), "clips")

	// Detect-only recorders never write segments or clips, so there is no need to
	// create the storage directories.
	if !r.cfg.DetectOnly {
		for _, d := range []string{r.liveDir, r.clipDir} {
			if err := os.MkdirAll(d, 0755); err != nil {
				return fmt.Errorf("recording: mkdir %s: %w", d, err)
			}
		}
		r.cleanStaleParts()
	}

	recCtx, cancel := context.WithCancel(ctx)
	r.mu.Lock()
	r.cancel = cancel
	r.mu.Unlock()

	segSec := segMinutes(r.cfg) * 60
	transport := strings.TrimSpace(r.cfg.RTSPTransport)
	if transport == "" {
		transport = "tcp"
	}

	// All three are supervised: a panic in any of them used to take the whole process
	// down, and merely recovering would be no better — if the segment watcher dies the
	// camera keeps writing .ts files that are never finalized, and if the purge loop
	// dies the disk fills. Nothing else would notice, so they must restart.
	cam := fmt.Sprintf("recording.cam%d", r.cfg.CameraId)
	safego.Supervise(recCtx, cam+".ffmpeg", func(ctx context.Context) {
		r.runFFmpeg(ctx, ffmpegPath, transport, segSec)
	})
	// Detect-only recorders produce no segment files, so the segment watcher and
	// retention purge have nothing to do.
	if !r.cfg.DetectOnly {
		safego.Supervise(recCtx, cam+".segments", r.watchSegments)
		if r.cfg.RetentionDays > 0 {
			safego.Supervise(recCtx, cam+".purge", r.purgeOldFiles)
		}
	}
	return nil
}

// buildFFmpegArgs assembles the ffmpeg argument list for one recorder run. A normal
// recorder emits the siphon MJPEG tee (when siphonFPS>0) followed by the copy-mux
// segment output; a detect-only recorder emits ONLY the tee — no segment muxer, no
// audio transcode, no segment files. Pure (no I/O) so the arg shape is unit-testable.
func (r *rtspRecorder) buildFFmpegArgs(uri, transport string, segSec int, pattern string, siphonFPS, siphonWidth int) []string {
	// Detect-only recorders exist solely to feed the siphon tee, so force it on even
	// when no explicit fps was configured.
	if r.cfg.DetectOnly && siphonFPS <= 0 {
		siphonFPS = 1
	}
	args := []string{
		"-hide_banner", "-loglevel", "warning",
		"-rtsp_transport", transport,
		"-fflags", "+genpts",
	}
	// Hardware-accelerate the tee's decode when configured. These are input options
	// (before -i) so they only affect outputs that decode — the MJPEG tee — while the
	// -c:v copy segment output keeps using the raw packets. Only emitted when the tee
	// is active; a copy-only run never decodes, so paying hwaccel init would be waste.
	if siphonFPS > 0 {
		args = append(args, r.hwAccelInputArgs()...)
	}
	args = append(args, "-i", uri)
	// Siphon tee: a second output decodes the video to downscaled MJPEG frames
	// at SiphonFPS on stdout, which a reader goroutine drains into the latest-
	// frame buffer. Placed before the segment output so the segment output stays
	// the process's primary concern; the segment output still uses stream copy.
	if siphonFPS > 0 {
		args = append(args,
			"-map", "0:v:0",
			"-an",
			"-r", fmt.Sprintf("%d", siphonFPS),
			"-vf", fmt.Sprintf("scale=%d:-2", siphonWidth),
			"-q:v", "7",
			"-f", "image2pipe",
			"-vcodec", "mjpeg",
			"pipe:1",
		)
	}
	// Detect-only: the MJPEG tee is the only output — no NVR segment muxer, no
	// audio transcode, no segment files.
	if r.cfg.DetectOnly {
		return args
	}
	args = append(args,
		"-c:v", "copy",
		// Transcode audio to AAC so all codecs (pcm_alaw, G.726, etc.)
		// are muxed as a native MPEG-TS stream type. aresample=async=1
		// compensates for cameras that send non-monotonic audio timestamps,
		// which would otherwise cause "Queue input is backward in time" errors.
		//
		// osr=48000 resamples to a standard 48 kHz: cameras that send
		// low-rate audio (e.g. 8 kHz G.711) otherwise exceed the AAC
		// per-frame bit budget ("Too many bits > 6144 per frame requested,
		// clamping to max"), which clamps the audio and surfaces as a
		// spurious recorder error in the UI. A bounded -b:a keeps the
		// requested bitrate within the per-frame budget at 48 kHz.
		"-c:a", "aac",
		"-b:a", "96k",
		"-af", "aresample=async=1:osr=48000",
		"-f", "segment",
		"-segment_time", fmt.Sprintf("%d", segSec),
		"-strftime", "1",
		"-segment_format", "mpegts",
		"-reset_timestamps", "1",
		pattern,
	)
	return args
}

// hwAccelInputArgs returns the ffmpeg input-side hardware-decode options for the
// siphon tee, mirroring infra/rtsp's baseFFmpegArgs so the recorder accelerates its
// decode the same way the live/standalone paths do. Empty / "none" HWAccel yields no
// args (software decode). Order matches ffmpeg's expectations: device init, hwaccel,
// hwaccel device, then explicit decoder — all before -i.
func (r *rtspRecorder) hwAccelInputArgs() []string {
	// Software-decode fallback engaged after repeated hardware-decode failures.
	r.mu.Lock()
	off := r.hwAccelOff
	r.mu.Unlock()
	if off {
		return nil
	}
	var args []string
	if initDevice := strings.TrimSpace(r.cfg.InitHWDevice); initDevice != "" {
		args = append(args, "-init_hw_device", initDevice)
	}
	hwaccel := strings.TrimSpace(r.cfg.HWAccel)
	useHWAccel := hwaccel != "" && !strings.EqualFold(hwaccel, "none")
	if useHWAccel {
		args = append(args, "-hwaccel", strings.ToLower(hwaccel))
		if device := strings.TrimSpace(r.cfg.HWAccelDevice); device != "" {
			args = append(args, "-hwaccel_device", device)
		}
	}
	if decoder := strings.TrimSpace(r.cfg.VideoDecoder); decoder != "" {
		args = append(args, "-c:v", decoder)
	}
	return args
}

// usesHWAccel reports whether the config requests hardware decode (an hwaccel other
// than none, or an explicit decoder), i.e. whether a software fallback is meaningful.
func (r *rtspRecorder) usesHWAccel() bool {
	hwaccel := strings.TrimSpace(r.cfg.HWAccel)
	if hwaccel != "" && !strings.EqualFold(hwaccel, "none") {
		return true
	}
	return strings.TrimSpace(r.cfg.VideoDecoder) != ""
}

func (r *rtspRecorder) runFFmpeg(ctx context.Context, ffmpegPath, transport string, segSec int) {
	pattern := filepath.ToSlash(filepath.Join(r.liveDir, "%Y%m%d_%H%M%S.ts"))

	siphonFPS := r.cfg.SiphonFPS
	siphonWidth := r.cfg.SiphonWidth
	if siphonWidth <= 0 {
		siphonWidth = defaultSiphonWidth
	}
	// Detect-only recorders feed the siphon tee unconditionally; force the fps on so
	// the stdout-pipe gating below opens the pipe and the drain goroutine runs.
	// buildFFmpegArgs applies the same default, so the arg list stays consistent.
	if r.cfg.DetectOnly && siphonFPS <= 0 {
		siphonFPS = 1
	}

	buildArgs := func(uri string) []string {
		return r.buildFFmpegArgs(uri, transport, segSec, pattern, siphonFPS, siphonWidth)
	}

	// Track which URI is active and switch to the fallback after repeated quick failures.
	currentURI := r.cfg.RTSPURI
	usingFallback := false
	failCount := 0
	// quickFailStreak counts consecutive quick failures across stream switches
	// (failCount is reset by the fallback switch, so it cannot drive backoff).
	// It only resets after a healthy run, and governs the restart backoff.
	quickFailStreak := 0

	for {
		if ctx.Err() != nil {
			return
		}
		cmd := exec.CommandContext(ctx, ffmpegPath, buildArgs(currentURI)...)
		procutil.HideWindow(cmd)
		stderr, _ := cmd.StderrPipe()
		var stdout io.ReadCloser
		if siphonFPS > 0 {
			stdout, _ = cmd.StdoutPipe()
		}
		if err := cmd.Start(); err != nil {
			r.mu.Lock()
			r.ffmpegRunning = false
			r.lastErrMsg = fmt.Sprintf("start failed: %v", err)
			r.mu.Unlock()
			log.Printf("recording rtsp cam%d: ffmpeg start: %v", r.cfg.CameraId, err)
			time.Sleep(5 * time.Second)
			continue
		}
		r.mu.Lock()
		r.ffmpegRunning = true
		r.activeStreamURL = currentURI
		r.usingFallback = usingFallback
		r.lastErrMsg = ""
		r.mu.Unlock()
		log.Printf("recording rtsp cam%d: ffmpeg started (pid %d)", r.cfg.CameraId, cmd.Process.Pid)
		if stderr != nil {
			go func() {
				sc := bufio.NewScanner(stderr)
				for sc.Scan() {
					line := strings.TrimSpace(sc.Text())
					if line == "" || isNoisyFFmpegWarning(line) {
						continue
					}
					log.Printf("recording rtsp cam%d ffmpeg: %s", r.cfg.CameraId, line)
					r.mu.Lock()
					r.lastErrMsg = line
					r.mu.Unlock()
				}
			}()
		}
		// Continuously drain the siphon MJPEG pipe so ffmpeg never blocks on it,
		// keeping only the latest decoded frame for the AI detector.
		if stdout != nil {
			go r.readSiphonFrames(stdout)
		}

		started := time.Now()
		_ = cmd.Wait()
		r.mu.Lock()
		r.ffmpegRunning = false
		r.mu.Unlock()
		if ctx.Err() != nil {
			return
		}

		// Only count as an unstable failure when the process ran for less than 10 s.
		if time.Since(started) < 10*time.Second {
			failCount++
			quickFailStreak++
		} else {
			failCount = 0
			quickFailStreak = 0
		}

		// After repeated quick failures with hardware decode active, fall back to
		// software decode for this camera. The GPU running out of NVDEC decode sessions
		// (consumer cards cap concurrent sessions) is the most likely cause when many
		// cameras siphon with hwaccel on; a restart loop would otherwise yield no
		// detection frames at all. Tried before the stream switch so we don't blame a
		// healthy fallback stream for a GPU-session problem.
		if quickFailStreak >= hwAccelFallbackFailures && r.usesHWAccel() {
			r.mu.Lock()
			alreadyOff := r.hwAccelOff
			r.hwAccelOff = true
			r.mu.Unlock()
			if !alreadyOff {
				log.Printf("recording rtsp cam%d: %d quick failures with hardware decode; falling back to software decode (GPU may be out of decode sessions)", r.cfg.CameraId, quickFailStreak)
				quickFailStreak = 0
				failCount = 0
			}
		}

		// After 2 quick failures switch streams (primary → fallback → primary …).
		if failCount >= 2 && r.cfg.FallbackRTSPURI != "" {
			if !usingFallback {
				log.Printf("recording rtsp cam%d: %d quick failures; switching to fallback stream", r.cfg.CameraId, failCount)
				usingFallback = true
				currentURI = r.cfg.FallbackRTSPURI
			} else {
				log.Printf("recording rtsp cam%d: fallback also unstable; reverting to primary stream", r.cfg.CameraId)
				usingFallback = false
				currentURI = r.cfg.RTSPURI
			}
			failCount = 0
		}

		// Adaptive restart backoff. A camera that streamed healthily and then
		// dropped is most likely a transient blip — reconnect fast (1 s) so the
		// gap of permanently-lost footage (which shows up as skipped time on the
		// playback timeline) stays small. A camera that keeps failing quickly
		// (bad URI, auth failure, hard-down) backs off so we don't hammer it or
		// flood the logs.
		restartDelay := time.Second
		switch {
		case quickFailStreak >= 5:
			restartDelay = 15 * time.Second
		case quickFailStreak >= 2:
			restartDelay = 5 * time.Second
		}
		log.Printf("recording rtsp cam%d: ffmpeg exited; restarting in %s", r.cfg.CameraId, restartDelay)
		r.cfg.countMetric(MetricFFmpegRestartsTotal, nil)
		select {
		case <-ctx.Done():
			return
		case <-time.After(restartDelay):
		}
	}
}

// watchSegments polls the live directory every 5 s, detects completed TS
// segments, remuxes them to MP4, and persists them to the segment sink.
func (r *rtspRecorder) watchSegments(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.saveCompletedSegments(ctx)
		}
	}
}

// claimStem reserves a stem for finalization. It returns false when the stem is
// already persisted, already discarded, or has finalization in flight — so a slow
// remux is never started twice by successive watch ticks.
func (r *rtspRecorder) claimStem(stem string) bool {
	r.segMu.Lock()
	defer r.segMu.Unlock()
	if r.segDone[stem] || r.segBusy[stem] {
		return false
	}
	r.segBusy[stem] = true
	return true
}

// finishStem records the outcome of finalizing a stem and releases the in-flight
// mark. A failed stem is deliberately left NOT done so the next watch tick retries
// it — the previous code marked a stem saved BEFORE attempting the remux, so any
// failure stranded the segment permanently and retention later shredded it.
func (r *rtspRecorder) finishStem(ctx context.Context, f liveSegInfo, outcome remuxOutcome) {
	// Every finalize attempt funnels through here, so this is the one place that sees
	// the whole picture. Anything other than `saved` accumulating means footage is not
	// reaching the recordings list — which is otherwise invisible until someone goes
	// looking for a clip that isn't there.
	r.cfg.countMetric(MetricSegmentFinalizeTotal, telemetry.Labels{"outcome": outcome.String()})

	r.segMu.Lock()
	delete(r.segBusy, f.stem)
	switch {
	case outcome == remuxSaved || outcome == remuxDiscarded:
		r.segDone[f.stem] = true
		delete(r.segFails, f.stem)
		r.segMu.Unlock()
		return
	case outcome == remuxUnsaved || ctx.Err() != nil:
		// Either the MP4 is safely on disk and only the DB write failed, or the recorder
		// is shutting down. Neither is the segment's fault, so don't burn a retry.
		r.segMu.Unlock()
		return
	}
	r.segFails[f.stem]++
	attempts := r.segFails[f.stem]
	r.segMu.Unlock()

	if attempts < maxRemuxAttempts {
		return
	}
	// Repeatedly unfinalizable. Park the source TS in quarantine/ rather than leaving
	// it in live/ where the retention purge would silently shred footage we never
	// managed to save, and stop retrying so it cannot spin ffmpeg forever.
	//
	// Counted separately: a non-zero quarantined count is footage that exists on disk but
	// will never appear in the recordings list. It is the one recorder metric worth
	// paging someone about.
	r.cfg.countMetric(MetricSegmentFinalizeTotal, telemetry.Labels{"outcome": "quarantined"})
	r.quarantineSegment(f)
	r.segMu.Lock()
	r.segDone[f.stem] = true
	delete(r.segFails, f.stem)
	r.segMu.Unlock()
}

// quarantineSegment moves a TS that could not be finalized out of the live directory
// so it survives retention and an operator can see it.
func (r *rtspRecorder) quarantineSegment(f liveSegInfo) {
	if f.tsPath == "" {
		return
	}
	dir := filepath.Join(filepath.Dir(r.liveDir), "quarantine")
	if err := os.MkdirAll(dir, 0755); err != nil {
		log.Printf("recording rtsp cam%d: quarantine mkdir: %v", r.cfg.CameraId, err)
		return
	}
	dst := filepath.Join(dir, f.stem+".ts")
	if err := os.Rename(f.tsPath, dst); err != nil {
		log.Printf("recording rtsp cam%d: quarantine %s: %v", r.cfg.CameraId, f.stem, err)
		return
	}
	log.Printf("recording rtsp cam%d: segment %s could not be finalized after %d attempts — moved to %s; footage is preserved but will not appear in the recordings list",
		r.cfg.CameraId, f.stem, maxRemuxAttempts, dst)
}

func (r *rtspRecorder) saveCompletedSegments(ctx context.Context) {
	files := r.listLiveSegments()
	// Need ≥2 files to know which are complete (all but the newest TS).
	if len(files) < 2 {
		return
	}
	// The last entry is the one currently being written; all others are complete.
	complete := files[:len(files)-1]

	for i, f := range complete {
		if !r.claimStem(f.stem) {
			continue
		}
		endedAt := files[i+1].startedAt
		if endedAt == 0 {
			endedAt = f.startedAt + int64(segMinutes(r.cfg)*60)
		}

		if f.tsPath != "" {
			// A .ts sibling means this stem was NEVER finalized: either it has not been
			// remuxed yet, or a previous run was interrupted mid-remux. The TS is the
			// source of truth, so remux it and let the rename replace any partial MP4
			// left behind. (Previously an interrupted remux left a truncated .mp4 which
			// was then adopted as the canonical footage while the intact .ts was
			// abandoned to the retention purge — silent footage loss on every restart.)
			f := f
			r.remuxWG.Add(1)
			go func() {
				defer r.remuxWG.Done()
				r.finishStem(ctx, f, r.remuxSegment(ctx, f, endedAt))
			}()
			continue
		}
		// Bare MP4, no TS: by the encrypt-then-rename invariant this is a complete,
		// encrypted segment that a previous run finalized but did not manage to persist.
		// Adopt it. This is also the retry path for a failed DB save.
		r.finishStem(ctx, f, r.adoptSegment(ctx, f, endedAt))
	}
}

// adoptSegment persists an already-finalized MP4 that has no DB row. It only ever
// runs when no .ts sibling exists; a stem with a TS is an unfinished remux and goes
// back through remuxSegment instead.
func (r *rtspRecorder) adoptSegment(ctx context.Context, f liveSegInfo, endedAt int64) remuxOutcome {
	fi, err := os.Stat(f.path)
	if err != nil || fi.Size() == 0 {
		return remuxDiscarded
	}

	encrypted := r.cfg.Cipher != nil && fileIsEncrypted(f.path)
	codec := ""
	if !encrypted {
		// Probe only while the file is plaintext — ffprobe cannot read ciphertext, and
		// spawning it on every encrypted segment in the live dir on the first tick after
		// a restart is a pure process storm for a result we always discard.
		if ffmpegPath, ferr := resolveFFmpeg(r.cfg.FFmpegPath); ferr == nil {
			endedAt = segmentEndedAt(ffmpegPath, f.path, f.startedAt, endedAt)
			codec = probeVideoCodec(ffmpegPath, f.path)
		}
	}
	if r.cfg.Cipher != nil && !encrypted {
		// Legacy safety net: a segment finalized by an older build (which encrypted in
		// place AFTER publishing the .mp4) can still be plaintext on disk. Encrypt it
		// now — otherwise it would sit unencrypted and survive a crypto-erase wipe.
		if err := r.cfg.Cipher.EncryptFileInPlace(f.path); err != nil {
			log.Printf("recording rtsp cam%d: encrypt orphaned segment %s: %v", r.cfg.CameraId, f.stem, err)
			return remuxFailed
		}
		if efi, statErr := os.Stat(f.path); statErr == nil {
			fi = efi
		}
	}

	if r.sink == nil {
		return remuxSaved
	}
	// Detached from the recorder context: a segment that is already finalized on disk
	// must still be persisted while the recorder is shutting down, or it is orphaned.
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	if err := r.sink.SaveSegment(saveCtx, SegmentResult{
		CameraId:  r.cfg.CameraId,
		AlertId:   0,
		FilePath:  f.path,
		StartedAt: f.startedAt,
		EndedAt:   endedAt,
		FileSize:  fi.Size(),
		Codec:     codec,
	}); err != nil {
		log.Printf("recording rtsp cam%d: save segment %s: %v", r.cfg.CameraId, f.stem, err)
		// The file is intact; only the DB write failed. Retry on the next tick.
		return remuxUnsaved
	}
	return remuxSaved
}

// remuxSegment converts a completed TS file to MP4 and persists it to the sink.
//
// The MP4 is built at <stem>.mp4.part and only renamed into place once it is
// complete AND encrypted, so a bare <stem>.mp4 is always a finished segment and an
// interrupted remux leaves the source TS intact for the next run to retry. The
// original TS is deleted only once the finished MP4 has been published.
func (r *rtspRecorder) remuxSegment(ctx context.Context, f liveSegInfo, endedAt int64) remuxOutcome {
	mp4Path := filepath.Join(r.liveDir, f.stem+".mp4")
	partPath := mp4Path + partSuffix

	// Skip empty segments. When the RTSP stream stalls or drops at a segment
	// boundary, ffmpeg's segment muxer still creates the .ts file but writes no
	// data. Handing a 0-byte file to ffmpeg only produces an "Invalid data found"
	// error and leaves the file behind, so discard it quietly instead.
	if fi, statErr := os.Stat(f.tsPath); statErr == nil && fi.Size() == 0 {
		log.Printf("recording rtsp cam%d: skipping empty segment %s (no stream data)", r.cfg.CameraId, f.stem)
		_ = os.Remove(f.tsPath)
		_ = os.Remove(partPath)
		return remuxDiscarded
	}

	ffmpegPath, err := resolveFFmpeg(r.cfg.FFmpegPath)
	if err != nil {
		log.Printf("recording rtsp cam%d: remux ffmpeg not found: %v", r.cfg.CameraId, err)
		return remuxFailed
	}

	// Storage codec: "copy" (default) streams the camera's native video through
	// unchanged; "h264"/"hevc" re-encode the video once here, on the GPU, to shrink
	// the segment. Re-encoding decodes the input, so it needs the same hardware-decode
	// input options the siphon tee uses; copy never decodes and takes none.
	videoArgs, storedCodec := remuxVideoArgs(r.cfg.RecordCodec, r.cfg.RecordQuality)
	encoding := reencodes(r.cfg.RecordCodec)
	// Proactively fall back to stream-copy when the configured NVENC encoder can't run
	// on this host (no NVIDIA GPU/driver, or ffmpeg built without it). The probe is
	// cached, so we skip re-encoding entirely rather than failing every segment.
	if encoding && r.cfg.RecordFallbackCopy && !NVENCUsable(ffmpegPath, nvencEncoder(r.cfg.RecordCodec)) {
		encoding = false
		videoArgs, storedCodec = remuxVideoArgs("", 0)
	}

	// Bound the remux, but keep it tied to the RECORDER context so a shutdown or a
	// reconfigure tears the ffmpeg child down instead of orphaning it on
	// context.Background() for up to 10 minutes, writing into a live directory that a
	// freshly-started recorder now owns.
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	// Any early exit below leaves a partial file behind; drop it so it neither wastes
	// disk nor confuses the next attempt. Cleared once the part is published.
	removePart := true
	defer func() {
		if removePart {
			_ = os.Remove(partPath)
		}
	}()

	// buildArgs assembles the ffmpeg finalize command. Re-encoding decodes the input,
	// so it needs the siphon's hardware-decode input options; stream-copy never
	// decodes and takes none. runRemux executes it and returns ffmpeg's output.
	buildArgs := func(vArgs []string, decode bool) []string {
		a := []string{"-hide_banner", "-loglevel", "error", "-fflags", "+genpts"}
		if decode {
			a = append(a, r.hwAccelInputArgs()...)
		}
		a = append(a, "-i", filepath.ToSlash(f.tsPath))
		a = append(a, vArgs...)
		// -f mp4 is REQUIRED, not belt-and-braces: we write to <stem>.mp4.part, and ffmpeg
		// picks the output container from the file extension. ".part" is not a container it
		// knows, so without an explicit format it aborts with "Unable to choose an output
		// format" and every segment fails to finalize.
		return append(a, "-movflags", "+faststart", "-f", "mp4", "-y", filepath.ToSlash(partPath))
	}
	runRemux := func(vArgs []string, decode bool) ([]byte, error) {
		cmd := exec.CommandContext(ctx, ffmpegPath, buildArgs(vArgs, decode)...)
		procutil.HideWindow(cmd)
		return cmd.CombinedOutput()
	}

	if encoding {
		// GPU re-encode is gated by the shared NVENC semaphore so concurrent remuxes
		// (and playback transcodes) never exceed the card's session cap. The .ts is
		// already on disk, so blocking here only delays finalize — it can't drop footage.
		release, acqErr := AcquireNVENC(ctx)
		if acqErr != nil {
			log.Printf("recording rtsp cam%d: remux %s: nvenc acquire: %v", r.cfg.CameraId, f.stem, acqErr)
			return remuxFailed
		}
		out, err := runRemux(videoArgs, true)
		release()
		if err != nil {
			// A late NVENC failure (a transient driver/session issue that slipped past
			// the capability probe) still shouldn't drop footage — fall back to plain
			// stream-copy when enabled instead of losing the segment.
			if !r.cfg.RecordFallbackCopy {
				log.Printf("recording rtsp cam%d: remux %s: %v: %s", r.cfg.CameraId, f.stem, err, out)
				return remuxFailed
			}
			log.Printf("recording rtsp cam%d: remux %s re-encode failed (%v); storing as stream-copy", r.cfg.CameraId, f.stem, err)
			copyArgs, _ := remuxVideoArgs("", 0)
			if out2, err2 := runRemux(copyArgs, false); err2 != nil {
				log.Printf("recording rtsp cam%d: remux %s copy fallback: %v: %s", r.cfg.CameraId, f.stem, err2, out2)
				return remuxFailed
			}
			storedCodec = "" // probe the copied stream's native codec below
		}
	} else if out, err := runRemux(videoArgs, false); err != nil {
		log.Printf("recording rtsp cam%d: remux %s: %v: %s", r.cfg.CameraId, f.stem, err, out)
		return remuxFailed
	}

	fi, err := os.Stat(partPath)
	if err != nil || fi.Size() == 0 {
		return remuxFailed
	}
	// Use the actual media duration; the inferred endedAt overstates segments
	// that were cut short by an ffmpeg restart. (Probe BEFORE encryption — ffprobe
	// needs the plaintext mp4.)
	endedAt = segmentEndedAt(ffmpegPath, partPath, f.startedAt, endedAt)

	// Record the on-disk video codec so the playback path can decide whether to
	// transcode for the browser without re-probing the encrypted file. When we
	// re-encoded we already know it; in copy mode probe the (still plaintext) mp4.
	if storedCodec == "" {
		storedCodec = probeVideoCodec(ffmpegPath, partPath)
	}

	// Encrypt the finalized segment at rest so it can be crypto-erased. Done after the
	// duration probe; the on-disk size becomes the ciphertext size (correct for disk
	// accounting). Playback decrypts on the fly.
	//
	// A failure here is fatal to the attempt: publishing an unencrypted segment would
	// silently break the at-rest guarantee (crypto-erase would not shred it), so bail
	// and let the next tick retry from the still-intact TS.
	if r.cfg.Cipher != nil {
		if err := r.cfg.Cipher.EncryptFileInPlace(partPath); err != nil {
			log.Printf("recording rtsp cam%d: encrypt segment %s: %v", r.cfg.CameraId, f.stem, err)
			return remuxFailed
		}
		if efi, statErr := os.Stat(partPath); statErr == nil {
			fi = efi
		}
	}

	// Atomic publish. Past this line the segment exists on disk only in its finished,
	// encrypted form — which is what lets a bare .mp4 be trusted after a crash.
	if err := os.Rename(partPath, mp4Path); err != nil {
		log.Printf("recording rtsp cam%d: publish segment %s: %v", r.cfg.CameraId, f.stem, err)
		return remuxFailed
	}
	removePart = false

	// The MP4 is complete and encrypted, so the source TS is now redundant. Drop it
	// before the sink write: if that write fails, the next watch tick sees a bare .mp4
	// and retries persistence through adoptSegment instead of re-encoding the segment.
	_ = os.Remove(f.tsPath)

	if r.sink != nil {
		// Detached from the recorder context so a segment that finished remuxing during
		// shutdown is still persisted rather than orphaned.
		saveCtx, saveCancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		err := r.sink.SaveSegment(saveCtx, SegmentResult{
			CameraId:  r.cfg.CameraId,
			AlertId:   0,
			FilePath:  mp4Path,
			StartedAt: f.startedAt,
			EndedAt:   endedAt,
			FileSize:  fi.Size(),
			Codec:     storedCodec,
		})
		saveCancel()
		if err != nil {
			log.Printf("recording rtsp cam%d: save remuxed segment %s: %v", r.cfg.CameraId, f.stem, err)
			return remuxUnsaved
		}
	}
	log.Printf("recording rtsp cam%d: segment %s saved (%d bytes)", r.cfg.CameraId, f.stem, fi.Size())
	return remuxSaved
}

// listLiveSegments returns all segments in liveDir sorted by start time.
// For each stem it prefers the .mp4 file (remuxed/complete) over the .ts file.
func (r *rtspRecorder) listLiveSegments() []liveSegInfo {
	entries, err := os.ReadDir(r.liveDir)
	if err != nil {
		return nil
	}

	type presence struct{ hasMp4, hasTs bool }
	byName := map[string]*presence{}

	for _, e := range entries {
		name := e.Name()
		var stem, ext string
		if strings.HasSuffix(name, ".mp4") {
			stem, ext = strings.TrimSuffix(name, ".mp4"), ".mp4"
		} else if strings.HasSuffix(name, ".ts") {
			stem, ext = strings.TrimSuffix(name, ".ts"), ".ts"
		} else {
			continue
		}
		if _, err := time.ParseInLocation(segTimeFormat, stem, time.Local); err != nil {
			continue
		}
		if byName[stem] == nil {
			byName[stem] = &presence{}
		}
		if ext == ".mp4" {
			byName[stem].hasMp4 = true
		} else {
			byName[stem].hasTs = true
		}
	}

	var files []liveSegInfo
	for stem, p := range byName {
		t, _ := time.ParseInLocation(segTimeFormat, stem, time.Local)
		info := liveSegInfo{stem: stem, startedAt: t.Unix()}
		if p.hasMp4 {
			info.path = filepath.Join(r.liveDir, stem+".mp4")
		}
		if p.hasTs {
			info.tsPath = filepath.Join(r.liveDir, stem+".ts")
			if !p.hasMp4 {
				info.path = info.tsPath
			}
		}
		files = append(files, info)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].startedAt < files[j].startedAt })
	return files
}

// extractClip waits for the post-roll window, then concatenates the live
// segments covering [triggerAt-preRoll, triggerAt+postRoll] into a clip file.
// It uses .ts files for segments still being written (which are readable
// even when incomplete) and .mp4 for already-remuxed completed segments.
func (r *rtspRecorder) extractClip(alertId int64, triggerAt time.Time, postWait time.Duration) {
	time.Sleep(postWait)

	pre := preRoll(r.cfg)
	post := postRoll(r.cfg)
	clipStart := triggerAt.Add(-time.Duration(pre) * time.Second)
	clipEnd := triggerAt.Add(time.Duration(post) * time.Second)

	segs := r.listLiveSegments()
	log.Printf("recording rtsp cam%d alert%d: clip window [%v, %v]; %d live segments available",
		r.cfg.CameraId, alertId,
		clipStart.In(time.Local).Format("15:04:05"),
		clipEnd.In(time.Local).Format("15:04:05"),
		len(segs))
	for _, s := range segs {
		log.Printf("  seg %s startedAt=%v path=%s", s.stem, time.Unix(s.startedAt, 0).In(time.Local).Format("15:04:05"), s.path)
	}

	segEnd := func(i int) time.Time {
		if i+1 < len(segs) {
			return time.Unix(segs[i+1].startedAt, 0)
		}
		return time.Unix(segs[i].startedAt, 0).Add(time.Duration(segMinutes(r.cfg)) * time.Minute)
	}

	var selected []liveSegInfo
	for i, s := range segs {
		end := segEnd(i)
		if end.Before(clipStart) {
			continue
		}
		if time.Unix(s.startedAt, 0).After(clipEnd) {
			break
		}
		selected = append(selected, s)
	}
	if len(selected) == 0 {
		log.Printf("recording rtsp cam%d alert%d: no live segments cover [%v, %v]; live dir has %d files",
			r.cfg.CameraId, alertId, clipStart.Format(time.RFC3339), clipEnd.Format(time.RFC3339), len(segs))
		return
	}

	// Finalized .mp4 segments are encrypted at rest; ffmpeg can't read them, so decrypt
	// each to a temp file and concat those. Plaintext (.ts / legacy) are used directly.
	var clipTemps []func()
	defer func() {
		for _, c := range clipTemps {
			c()
		}
	}()

	listPath := filepath.Join(r.clipDir, fmt.Sprintf("clip_%d.txt", alertId))
	var sb strings.Builder
	for _, s := range selected {
		p := s.path
		if r.cfg.Cipher != nil && fileIsEncrypted(s.path) {
			if tmp, cleanup, derr := r.cfg.Cipher.DecryptToTempFile(s.path); derr == nil {
				p = tmp
				clipTemps = append(clipTemps, cleanup)
			} else {
				log.Printf("recording rtsp cam%d alert%d: decrypt segment %s: %v", r.cfg.CameraId, alertId, s.stem, derr)
			}
		}
		abs, _ := filepath.Abs(p)
		// Forward slashes required by ffmpeg concat demuxer on all platforms.
		fmt.Fprintf(&sb, "file '%s'\n", filepath.ToSlash(abs))
	}
	if err := os.WriteFile(listPath, []byte(sb.String()), 0644); err != nil {
		log.Printf("recording rtsp cam%d alert%d: write concat list: %v", r.cfg.CameraId, alertId, err)
		return
	}
	defer os.Remove(listPath)

	ssOffset := clipStart.Sub(time.Unix(selected[0].startedAt, 0).UTC())
	if ssOffset < 0 {
		ssOffset = 0
	}
	duration := time.Duration(pre+post) * time.Second
	outputPath := filepath.Join(r.clipDir, fmt.Sprintf("alert_%d_%d.mp4", alertId, clipStart.Unix()))

	ffmpegPath, err := resolveFFmpeg(r.cfg.FFmpegPath)
	if err != nil {
		log.Printf("recording rtsp cam%d alert%d: %v", r.cfg.CameraId, alertId, err)
		return
	}
	// -ss is placed BEFORE -i (input seeking) on purpose: with -c copy, ffmpeg
	// then snaps the cut to the nearest keyframe at or before the requested
	// offset, so the clip always begins on a keyframe. Output-side seeking
	// (-ss after -i) would start the clip on whatever packet follows the offset
	// — often a non-keyframe — which slides/smears until the next IDR.
	args := []string{
		"-hide_banner", "-loglevel", "warning",
		"-fflags", "+genpts",
		"-ss", fmt.Sprintf("%.3f", ssOffset.Seconds()),
		"-f", "concat", "-safe", "0",
		"-i", filepath.ToSlash(listPath),
		"-t", fmt.Sprintf("%.3f", duration.Seconds()),
		"-c", "copy",
		"-movflags", "+faststart",
		"-y", filepath.ToSlash(outputPath),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	clip := exec.CommandContext(ctx, ffmpegPath, args...)
	procutil.HideWindow(clip)
	if out, err := clip.CombinedOutput(); err != nil {
		log.Printf("recording rtsp cam%d alert%d: extract clip: %v: %s", r.cfg.CameraId, alertId, err, out)
		return
	}

	fi, err := os.Stat(outputPath)
	if err != nil || fi.Size() == 0 {
		log.Printf("recording rtsp cam%d alert%d: clip output missing or empty", r.cfg.CameraId, alertId)
		return
	}

	// Encrypt the event clip at rest too (same as recorded segments).
	if r.cfg.Cipher != nil {
		if err := r.cfg.Cipher.EncryptFileInPlace(outputPath); err != nil {
			log.Printf("recording rtsp cam%d alert%d: encrypt clip: %v", r.cfg.CameraId, alertId, err)
		} else if efi, statErr := os.Stat(outputPath); statErr == nil {
			fi = efi
		}
	}

	if r.sink != nil {
		_ = r.sink.SaveSegment(context.Background(), SegmentResult{
			CameraId:  r.cfg.CameraId,
			AlertId:   alertId,
			FilePath:  outputPath,
			StartedAt: clipStart.Unix(),
			EndedAt:   clipEnd.Unix(),
			FileSize:  fi.Size(),
		})
	}
	log.Printf("recording rtsp cam%d alert%d: clip saved %s (%d bytes)", r.cfg.CameraId, alertId, filepath.Base(outputPath), fi.Size())
}

// isNoisyFFmpegWarning returns true for well-known ffmpeg warnings that are
// emitted by cameras with imperfect RTSP streams but do not indicate a real
// recording failure (ffmpeg corrects them automatically).
func isNoisyFFmpegWarning(line string) bool {
	noisy := []string{
		"Non-monotonic DTS",
		"Timestamps are unset",
		"This is deprecated and will stop working",
		"This may result in incorrect timestamps",
		"Fix your code to set the timestamps",
		"starts with a non keyframe",
		"DTS discontinuity",
		"is muxed as a private data stream",
		"Queue input is backward in time",
		// AAC encoder clamps the per-frame bitrate for low-rate audio; benign
		// once the stream is resampled, but old/edge cases should not alarm.
		"Too many bits",
	}
	for _, s := range noisy {
		if strings.Contains(line, s) {
			return true
		}
	}
	// Segment muxer lines that contain only the component address and nothing else,
	// e.g. "[segment @ 0x7f...]" with no actual message after it.
	if strings.HasPrefix(line, "[segment @") && strings.HasSuffix(line, "]") {
		return true
	}
	return false
}

// purgeOldFiles removes segment files older than RetentionDays.
func (r *rtspRecorder) purgeOldFiles(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cutoff := time.Now().AddDate(0, 0, -r.cfg.RetentionDays)
			for _, f := range r.listLiveSegments() {
				if time.Unix(f.startedAt, 0).Before(cutoff) {
					// Retention purge is a real deletion of footage, so honour the
					// shred setting (overwrite before unlink) rather than a plain remove.
					_ = SecureRemove(f.path, r.cfg.ShredPasses)
					if f.tsPath != "" {
						_ = SecureRemove(f.tsPath, r.cfg.ShredPasses)
					}
				}
			}
			r.purgeQuarantine(cutoff)
		}
	}
}

// purgeQuarantine applies the same retention rule to quarantined segments, so a
// camera that persistently fails to finalize cannot fill the disk with files that
// will never be looked at.
func (r *rtspRecorder) purgeQuarantine(cutoff time.Time) {
	dir := filepath.Join(filepath.Dir(r.liveDir), "quarantine")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		stem := strings.TrimSuffix(e.Name(), ".ts")
		t, err := time.ParseInLocation(segTimeFormat, stem, time.Local)
		if err != nil || !t.Before(cutoff) {
			continue
		}
		_ = SecureRemove(filepath.Join(dir, e.Name()), r.cfg.ShredPasses)
	}
}
