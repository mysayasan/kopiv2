package services

import "testing"

func TestFFmpegDownloadURL(t *testing.T) {
	supported := []struct {
		goos, goarch, kind string
	}{
		{"windows", "amd64", "zip"},
		{"windows", "arm64", "zip"},
		{"linux", "amd64", "tarxz"},
		{"linux", "arm64", "tarxz"},
		{"darwin", "amd64", "zip"},
		{"darwin", "arm64", "zip"},
	}
	for _, tc := range supported {
		url, kind, err := ffmpegDownloadURL(tc.goos, tc.goarch)
		if err != nil {
			t.Fatalf("%s/%s: unexpected error %v", tc.goos, tc.goarch, err)
		}
		if url == "" {
			t.Fatalf("%s/%s: empty url", tc.goos, tc.goarch)
		}
		if kind != tc.kind {
			t.Fatalf("%s/%s: kind = %q, want %q", tc.goos, tc.goarch, kind, tc.kind)
		}
	}

	unsupported := [][2]string{
		{"linux", "386"},
		{"windows", "386"},
		{"plan9", "amd64"},
	}
	for _, tc := range unsupported {
		if _, _, err := ffmpegDownloadURL(tc[0], tc[1]); err == nil {
			t.Fatalf("%s/%s: expected unsupported error, got nil", tc[0], tc[1])
		}
	}
}

func TestIsFFmpegBinaryName(t *testing.T) {
	for _, ok := range []string{"ffmpeg", "ffmpeg.exe", "ffprobe", "ffprobe.exe"} {
		if !isFFmpegBinaryName(ok) {
			t.Fatalf("%q should be recognized", ok)
		}
	}
	for _, no := range []string{"ffmpeg.txt", "libavcodec.dll", "readme", "ffplay"} {
		if isFFmpegBinaryName(no) {
			t.Fatalf("%q should not be recognized", no)
		}
	}
}
