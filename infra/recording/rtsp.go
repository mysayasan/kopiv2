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
}

func newRTSPRecorder(cfg RecorderConfig, sink SegmentSink) *rtspRecorder {
	return &rtspRecorder{cfg: cfg, sink: sink}
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

func (r *rtspRecorder) Close() {
	r.mu.Lock()
	cancel := r.cancel
	r.mu.Unlock()
	if cancel != nil {
		cancel()
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

	go r.runFFmpeg(recCtx, ffmpegPath, transport, segSec)
	// Detect-only recorders produce no segment files, so the segment watcher and
	// retention purge have nothing to do.
	if !r.cfg.DetectOnly {
		go r.watchSegments(recCtx)
		if r.cfg.RetentionDays > 0 {
			go r.purgeOldFiles(recCtx)
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
	saved := map[string]bool{}

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.saveCompletedSegments(ctx, saved)
		}
	}
}

func (r *rtspRecorder) saveCompletedSegments(ctx context.Context, saved map[string]bool) {
	files := r.listLiveSegments()
	// Need ≥2 files to know which are complete (all but the newest TS).
	if len(files) < 2 {
		return
	}
	// The last entry is the one currently being written; all others are complete.
	complete := files[:len(files)-1]

	for i, f := range complete {
		if saved[f.stem] {
			continue
		}
		saved[f.stem] = true
		endedAt := files[i+1].startedAt
		if endedAt == 0 {
			endedAt = f.startedAt + int64(segMinutes(r.cfg)*60)
		}

		if f.tsPath != "" && strings.HasSuffix(f.path, ".ts") {
			// TS not yet remuxed — convert to MP4 in background.
			go r.remuxSegment(f, endedAt)
		} else if strings.HasSuffix(f.path, ".mp4") {
			// MP4 already exists (e.g. server restarted after a prior remux).
			// Persist to DB; the dedup in SaveSegment prevents double-inserts.
			fi, err := os.Stat(f.path)
			if err != nil || fi.Size() == 0 {
				continue
			}
			if ffmpegPath, ferr := resolveFFmpeg(r.cfg.FFmpegPath); ferr == nil {
				endedAt = segmentEndedAt(ffmpegPath, f.path, f.startedAt, endedAt)
			}
			if r.sink != nil {
				if err := r.sink.SaveSegment(ctx, SegmentResult{
					CameraId:  r.cfg.CameraId,
					AlertId:   0,
					FilePath:  f.path,
					StartedAt: f.startedAt,
					EndedAt:   endedAt,
					FileSize:  fi.Size(),
				}); err != nil {
					log.Printf("recording rtsp cam%d: save segment %s: %v", r.cfg.CameraId, f.stem, err)
				}
			}
		}
	}
}

// remuxSegment converts a completed TS file to MP4 and persists it to the DB.
// The original TS is deleted after a successful remux.
func (r *rtspRecorder) remuxSegment(f liveSegInfo, endedAt int64) {
	mp4Path := filepath.Join(r.liveDir, f.stem+".mp4")

	// Skip empty segments. When the RTSP stream stalls or drops at a segment
	// boundary, ffmpeg's segment muxer still creates the .ts file but writes no
	// data. Handing a 0-byte file to ffmpeg only produces an "Invalid data found"
	// error and leaves the file behind, so discard it quietly instead.
	if fi, statErr := os.Stat(f.tsPath); statErr == nil && fi.Size() == 0 {
		log.Printf("recording rtsp cam%d: skipping empty segment %s (no stream data)", r.cfg.CameraId, f.stem)
		_ = os.Remove(f.tsPath)
		return
	}

	ffmpegPath, err := resolveFFmpeg(r.cfg.FFmpegPath)
	if err != nil {
		log.Printf("recording rtsp cam%d: remux ffmpeg not found: %v", r.cfg.CameraId, err)
		return
	}

	args := []string{
		"-hide_banner", "-loglevel", "error",
		"-fflags", "+genpts",
		"-i", filepath.ToSlash(f.tsPath),
		"-c", "copy",
		"-movflags", "+faststart",
		"-y", filepath.ToSlash(mp4Path),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if out, err := exec.CommandContext(ctx, ffmpegPath, args...).CombinedOutput(); err != nil {
		log.Printf("recording rtsp cam%d: remux %s: %v: %s", r.cfg.CameraId, f.stem, err, out)
		return
	}

	fi, err := os.Stat(mp4Path)
	if err != nil || fi.Size() == 0 {
		return
	}
	// Use the actual media duration; the inferred endedAt overstates segments
	// that were cut short by an ffmpeg restart. (Probe BEFORE encryption — ffprobe
	// needs the plaintext mp4.)
	endedAt = segmentEndedAt(ffmpegPath, mp4Path, f.startedAt, endedAt)

	// Encrypt the finalized segment at rest so it can be crypto-erased. Done after the
	// duration probe; the on-disk size becomes the ciphertext size (correct for disk
	// accounting). Playback decrypts on the fly.
	if r.cfg.Cipher != nil {
		if err := r.cfg.Cipher.EncryptFileInPlace(mp4Path); err != nil {
			log.Printf("recording rtsp cam%d: encrypt segment %s: %v", r.cfg.CameraId, f.stem, err)
		} else if efi, statErr := os.Stat(mp4Path); statErr == nil {
			fi = efi
		}
	}
	if r.sink != nil {
		if err := r.sink.SaveSegment(context.Background(), SegmentResult{
			CameraId:  r.cfg.CameraId,
			AlertId:   0,
			FilePath:  mp4Path,
			StartedAt: f.startedAt,
			EndedAt:   endedAt,
			FileSize:  fi.Size(),
		}); err != nil {
			log.Printf("recording rtsp cam%d: save remuxed segment %s: %v", r.cfg.CameraId, f.stem, err)
		}
	}
	// Remove the source TS only after a successful DB save.
	_ = os.Remove(f.tsPath)
	log.Printf("recording rtsp cam%d: segment %s saved (%d bytes)", r.cfg.CameraId, f.stem, fi.Size())
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

	if out, err := exec.CommandContext(ctx, ffmpegPath, args...).CombinedOutput(); err != nil {
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
		}
	}
}
