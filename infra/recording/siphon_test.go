package recording

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

// TestReadSiphonFramesKeepsLatest feeds two concatenated JPEG frames (SOI..EOI)
// and checks the splitter keeps the most recent complete frame.
func TestReadSiphonFramesKeepsLatest(t *testing.T) {
	r := &rtspRecorder{}
	f1 := []byte{0xFF, 0xD8, 0x11, 0x22, 0xFF, 0xD9}
	f2 := []byte{0xFF, 0xD8, 0x33, 0x44, 0x55, 0xFF, 0xD9}
	stream := append(append([]byte{}, f1...), f2...)

	r.readSiphonFrames(io.NopCloser(bytes.NewReader(stream)))

	got, at, ok := r.latestFrame()
	if !ok {
		t.Fatal("expected a captured frame")
	}
	if !bytes.Equal(got, f2) {
		t.Fatalf("latest frame = % x, want % x", got, f2)
	}
	if at <= 0 {
		t.Fatalf("expected a capture timestamp, got %d", at)
	}
}

// TestReadSiphonFramesIgnoresLeadingGarbage ensures bytes before the first SOI
// (e.g. ffmpeg banner noise) don't corrupt the first frame.
func TestReadSiphonFramesIgnoresLeadingGarbage(t *testing.T) {
	r := &rtspRecorder{}
	frame := []byte{0xFF, 0xD8, 0xAA, 0xBB, 0xFF, 0xD9}
	stream := append([]byte{0x00, 0x99, 0xFF, 0x00}, frame...)

	r.readSiphonFrames(io.NopCloser(bytes.NewReader(stream)))

	got, _, ok := r.latestFrame()
	if !ok || !bytes.Equal(got, frame) {
		t.Fatalf("latest frame = % x (ok=%v), want % x", got, ok, frame)
	}
}

// TestBuildFFmpegArgsDetectOnly verifies a detect-only recorder emits only the
// MJPEG siphon tee — the tee is present (even with no configured fps) and the NVR
// segment muxer/audio transcode are absent.
func TestBuildFFmpegArgsDetectOnly(t *testing.T) {
	r := &rtspRecorder{cfg: RecorderConfig{DetectOnly: true}}
	args := r.buildFFmpegArgs("rtsp://cam/stream", "tcp", 900, "/live/%Y.ts", 0, 640)
	joined := strings.Join(args, " ")

	if !strings.Contains(joined, "image2pipe") || !strings.Contains(joined, "mjpeg") {
		t.Fatalf("detect-only args missing siphon tee: %s", joined)
	}
	for _, banned := range []string{"-f segment", "segment", "-c:a", "aac", "/live/%Y.ts"} {
		if contains(args, banned) {
			t.Fatalf("detect-only args must not contain %q: %s", banned, joined)
		}
	}
}

// TestBuildFFmpegArgsRecording verifies a normal recorder emits both the siphon tee
// and the segment output.
func TestBuildFFmpegArgsRecording(t *testing.T) {
	r := &rtspRecorder{cfg: RecorderConfig{}}
	args := r.buildFFmpegArgs("rtsp://cam/stream", "tcp", 900, "/live/%Y.ts", 2, 640)
	if !contains(args, "image2pipe") {
		t.Fatalf("recording args missing siphon tee: %v", args)
	}
	if !contains(args, "segment") || !contains(args, "/live/%Y.ts") {
		t.Fatalf("recording args missing segment output: %v", args)
	}
}

// TestBuildFFmpegArgsHWAccel verifies the siphon tee picks up the configured
// hardware decoder as input options before -i, and that they are omitted when the
// tee is inactive (copy-only run never decodes).
func TestBuildFFmpegArgsHWAccel(t *testing.T) {
	r := &rtspRecorder{cfg: RecorderConfig{DetectOnly: true, HWAccel: "cuda", HWAccelDevice: "0"}}
	args := r.buildFFmpegArgs("rtsp://cam/stream", "tcp", 0, "", 2, 640)
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-hwaccel cuda") || !strings.Contains(joined, "-hwaccel_device 0") {
		t.Fatalf("expected hwaccel flags in detect-only args: %s", joined)
	}
	// hwaccel must precede the input.
	if idxHW, idxIn := strings.Index(joined, "-hwaccel"), strings.Index(joined, "-i "); idxHW < 0 || idxHW > idxIn {
		t.Fatalf("hwaccel must come before -i: %s", joined)
	}

	// "none" hwaccel and no tee → no hwaccel flags.
	r2 := &rtspRecorder{cfg: RecorderConfig{HWAccel: "none"}}
	if got := strings.Join(r2.buildFFmpegArgs("rtsp://cam/stream", "tcp", 900, "/live/%Y.ts", 0, 640), " "); strings.Contains(got, "-hwaccel") {
		t.Fatalf("copy-only run must not request hwaccel: %s", got)
	}
}

// TestHWAccelSoftwareFallback verifies that once the runtime software-decode fallback
// is engaged, the tee emits no hardware-decode flags even though the config requests
// them — so a GPU out of decode sessions degrades to software instead of looping.
func TestHWAccelSoftwareFallback(t *testing.T) {
	r := &rtspRecorder{cfg: RecorderConfig{DetectOnly: true, HWAccel: "cuda"}}
	if !r.usesHWAccel() {
		t.Fatal("expected usesHWAccel() true for cuda config")
	}
	if got := strings.Join(r.buildFFmpegArgs("rtsp://cam/s", "tcp", 0, "", 2, 640), " "); !strings.Contains(got, "-hwaccel cuda") {
		t.Fatalf("expected hwaccel before fallback: %s", got)
	}
	r.hwAccelOff = true
	if got := strings.Join(r.buildFFmpegArgs("rtsp://cam/s", "tcp", 0, "", 2, 640), " "); strings.Contains(got, "-hwaccel") {
		t.Fatalf("software fallback must drop hwaccel flags: %s", got)
	}
}

func contains(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}

// TestLatestFrameEmpty returns ok=false before any frame is captured.
func TestLatestFrameEmpty(t *testing.T) {
	r := &rtspRecorder{}
	if _, _, ok := r.latestFrame(); ok {
		t.Fatal("expected ok=false with no captured frame")
	}
}
