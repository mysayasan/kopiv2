package recording

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

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
}

func newRTSPRecorder(cfg RecorderConfig, sink SegmentSink) *rtspRecorder {
	return &rtspRecorder{cfg: cfg, sink: sink}
}

func (r *rtspRecorder) WriteFrame(_ []byte, _ int64) {}

// TriggerEvent waits for the post-roll window then slices an event clip from
// the already-recorded live segments. frameCapturedAt is the Unix second
// timestamp of the frame that triggered the alert; the clip window is anchored
// to that moment so YOLO inference latency does not shift the recording.
func (r *rtspRecorder) TriggerEvent(alertId int64, frameCapturedAt int64) {
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
	segs := r.listLiveSegments()
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

	return CameraStatus{
		CameraId:           r.cfg.CameraId,
		Mode:               "rtsp",
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

	for _, d := range []string{r.liveDir, r.clipDir} {
		if err := os.MkdirAll(d, 0755); err != nil {
			return fmt.Errorf("recording: mkdir %s: %w", d, err)
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
	go r.watchSegments(recCtx)
	if r.cfg.RetentionDays > 0 {
		go r.purgeOldFiles(recCtx)
	}
	return nil
}

func (r *rtspRecorder) runFFmpeg(ctx context.Context, ffmpegPath, transport string, segSec int) {
	pattern := filepath.ToSlash(filepath.Join(r.liveDir, "%Y%m%d_%H%M%S.ts"))

	buildArgs := func(uri string) []string {
		return []string{
			"-hide_banner", "-loglevel", "warning",
			"-rtsp_transport", transport,
			"-fflags", "+genpts",
			"-i", uri,
			"-c", "copy",
			"-f", "segment",
			"-segment_time", fmt.Sprintf("%d", segSec),
			"-strftime", "1",
			"-segment_format", "mpegts",
			"-reset_timestamps", "1",
			pattern,
		}
	}

	// Track which URI is active and switch to the fallback after repeated quick failures.
	currentURI := r.cfg.RTSPURI
	usingFallback := false
	failCount := 0

	for {
		if ctx.Err() != nil {
			return
		}
		cmd := exec.CommandContext(ctx, ffmpegPath, buildArgs(currentURI)...)
		stderr, _ := cmd.StderrPipe()
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
					if line == "" {
						continue
					}
					log.Printf("recording rtsp cam%d ffmpeg: %s", r.cfg.CameraId, line)
					if !isNoisyFFmpegWarning(line) {
						r.mu.Lock()
						r.lastErrMsg = line
						r.mu.Unlock()
					}
				}
			}()
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
		} else {
			failCount = 0
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

		log.Printf("recording rtsp cam%d: ffmpeg exited; restarting in 3 s", r.cfg.CameraId)
		time.Sleep(3 * time.Second)
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

	listPath := filepath.Join(r.clipDir, fmt.Sprintf("clip_%d.txt", alertId))
	var sb strings.Builder
	for _, s := range selected {
		abs, _ := filepath.Abs(s.path)
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
	args := []string{
		"-hide_banner", "-loglevel", "warning",
		"-fflags", "+genpts",
		"-f", "concat", "-safe", "0",
		"-i", filepath.ToSlash(listPath),
		"-ss", fmt.Sprintf("%.3f", ssOffset.Seconds()),
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
		"changing to 1. This may result",
		"Fix your code to set the timestamps",
		"starts with a non keyframe",
		"DTS discontinuity",
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
					_ = os.Remove(f.path)
					if f.tsPath != "" {
						_ = os.Remove(f.tsPath)
					}
				}
			}
		}
	}
}
