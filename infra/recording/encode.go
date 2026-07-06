package recording

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/mysayasan/kopiv2/infra/procutil"
)

// defaultNVENCConcurrency caps simultaneous GPU encode sessions when no limit is
// configured. Consumer NVIDIA cards historically cap NVENC sessions (older drivers
// at 2-3); 2 is a safe default that leaves headroom for live decode.
const defaultNVENCConcurrency = 2

// defaultRecordCQ is the NVENC constant-quality target used when re-encoding
// segments and none is configured. ~26 trades a small, usually imperceptible
// quality drop for a large size reduction versus the camera's native bitrate.
const defaultRecordCQ = 26

var (
	nvencMu  sync.Mutex
	nvencSem chan struct{}
)

// SetNVENCConcurrency sizes the global NVENC semaphore shared by remux-time
// re-encoding and (later) playback transcode, so the GPU is never oversubscribed
// past its session limit. Call once at startup; n <= 0 uses the default. Safe to
// call before any encode starts; resizing after work is in flight is best-effort.
func SetNVENCConcurrency(n int) {
	if n <= 0 {
		n = defaultNVENCConcurrency
	}
	nvencMu.Lock()
	nvencSem = make(chan struct{}, n)
	nvencMu.Unlock()
}

func nvencSemaphore() chan struct{} {
	nvencMu.Lock()
	defer nvencMu.Unlock()
	if nvencSem == nil {
		nvencSem = make(chan struct{}, defaultNVENCConcurrency)
	}
	return nvencSem
}

// AcquireNVENC blocks until a GPU encode slot is free or ctx is cancelled. The
// returned release func MUST be called once the encode finishes. Because the
// segment .ts is already on disk before remux runs, waiting here never risks
// dropping footage — at worst the mp4 finalize is delayed.
func AcquireNVENC(ctx context.Context) (release func(), err error) {
	sem := nvencSemaphore()
	select {
	case sem <- struct{}{}:
		var once sync.Once
		return func() { once.Do(func() { <-sem }) }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// TranscodeH264 streams video read from src to dst as a fragmented MP4 with H.264
// video (NVENC) and copied audio, for browsers that cannot decode the stored HEVC
// (Firefox, older browsers). It runs under the shared NVENC semaphore so it never
// oversubscribes the GPU alongside remux-time encoding.
//
// Input is decoded in software (no input hwaccel) to avoid consuming a scarce NVDEC
// decode session per playback; only the encode uses the GPU. Fragmented movflags
// (frag_keyframe+empty_moov) make the output playable while streaming over a
// response that has no Content-Length or range support — matching the encrypted
// serve path. ffmpegPath may be empty (resolves to "ffmpeg" on PATH).
func TranscodeH264(ctx context.Context, ffmpegPath string, src io.Reader, dst io.Writer) error {
	release, err := AcquireNVENC(ctx)
	if err != nil {
		return err
	}
	defer release()

	resolved, err := resolveFFmpeg(ffmpegPath)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, resolved,
		"-hide_banner", "-loglevel", "error",
		"-i", "pipe:0",
		"-c:v", "h264_nvenc", "-preset", "p5",
		"-c:a", "copy",
		"-movflags", "frag_keyframe+empty_moov",
		"-f", "mp4",
		"pipe:1",
	)
	procutil.HideWindow(cmd)
	cmd.Stdin = src
	cmd.Stdout = dst
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("transcode hevc->h264: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

var (
	nvencProbeMu sync.Mutex
	nvencProbe   = map[string]bool{} // encoder name -> usable on this host
)

// NVENCUsable reports whether the given NVENC encoder actually works on this host —
// i.e. ffmpeg was built with it AND a compatible NVIDIA GPU/driver is present. It
// runs one tiny throwaway encode of a generated frame the first time and caches the
// result per encoder, so the check costs at most a single short ffmpeg run. Empty /
// copy encoders are always "usable" (they never touch the GPU).
//
// This lets the recorder decide up front to store segments as plain stream-copy when
// the configured re-encode codec can't run, instead of failing every segment remux.
func NVENCUsable(ffmpegPath, encoder string) bool {
	if strings.TrimSpace(encoder) == "" {
		return true
	}
	nvencProbeMu.Lock()
	defer nvencProbeMu.Unlock()
	if v, ok := nvencProbe[encoder]; ok {
		return v
	}
	resolved, err := resolveFFmpeg(ffmpegPath)
	if err != nil {
		nvencProbe[encoder] = false
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	// Encode two frames of a generated source to the null muxer. If NVENC can't
	// initialise (no GPU, driver mismatch, encoder not compiled in) ffmpeg exits
	// non-zero and we treat the encoder as unavailable.
	cmd := exec.CommandContext(ctx, resolved,
		"-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "color=c=black:s=256x256:r=5:d=1",
		"-frames:v", "2", "-c:v", encoder, "-f", "null", "-",
	)
	procutil.HideWindow(cmd)
	runErr := cmd.Run()
	usable := runErr == nil
	if !usable {
		log.Printf("recording: NVENC encoder %q is not usable on this host (%v); segments configured for it will be stored as stream-copy instead", encoder, runErr)
	}
	nvencProbe[encoder] = usable
	return usable
}

// nvencEncoder maps a storage codec name to its NVENC encoder. Returns "" for
// copy / empty / unrecognized values (meaning: do not re-encode).
func nvencEncoder(codec string) string {
	switch strings.ToLower(strings.TrimSpace(codec)) {
	case "h264", "avc", "h.264":
		return "h264_nvenc"
	case "hevc", "h265", "h.265":
		return "hevc_nvenc"
	}
	return ""
}

// canonicalCodec normalizes a record-codec or ffprobe codec_name to the label
// stored in the segments table ("h264" / "hevc"), or "" when not a known/handled
// codec.
func canonicalCodec(codec string) string {
	switch strings.ToLower(strings.TrimSpace(codec)) {
	case "h264", "avc", "h.264":
		return "h264"
	case "hevc", "h265", "h.265":
		return "hevc"
	}
	return ""
}

// reencodes reports whether the given record codec triggers a GPU re-encode at
// remux (vs. plain stream copy).
func reencodes(codec string) bool { return nvencEncoder(codec) != "" }

// ReEncodes reports whether the given storage codec re-encodes at remux (h264/hevc)
// rather than plain stream-copy. Exported for the API layer so it can report whether
// a codec depends on the GPU.
func ReEncodes(codec string) bool { return reencodes(codec) }

// StorageCodecUsable reports whether the given storage codec can actually be produced
// on this host: copy/empty codecs are always usable; a re-encode codec (h264/hevc) is
// usable only when its NVENC encoder works here (probed and cached by NVENCUsable).
func StorageCodecUsable(ffmpegPath, codec string) bool {
	return NVENCUsable(ffmpegPath, nvencEncoder(codec))
}

// remuxVideoArgs returns the ffmpeg output args for finalizing a segment. For
// copy / empty codec it returns the historical stream-copy args. For h264/hevc it
// returns NVENC encode args (video re-encoded, audio copied — the .ts audio is
// already AAC from capture). It also returns the canonical on-disk codec label for
// the segments row; empty means "unchanged / probe to determine".
func remuxVideoArgs(codec string, quality int) (args []string, storedCodec string) {
	enc := nvencEncoder(codec)
	if enc == "" {
		return []string{"-c", "copy"}, ""
	}
	cq := quality
	if cq <= 0 {
		cq = defaultRecordCQ
	}
	args = []string{
		"-c:v", enc,
		"-preset", "p5",
		"-rc", "vbr",
		"-cq", fmt.Sprintf("%d", cq),
		"-c:a", "copy",
	}
	if enc == "hevc_nvenc" {
		// hvc1 tag (not the default hev1) is required for HEVC-in-mp4 to play in
		// Safari and the Media Source Extensions used by the playback probe.
		args = append(args, "-tag:v", "hvc1")
	}
	return args, canonicalCodec(codec)
}
