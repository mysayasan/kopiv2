package rtsp

import (
	"reflect"
	"testing"
)

func TestBaseFFmpegArgsIncludesDecoderInputOptionsBeforeInput(t *testing.T) {
	args := baseFFmpegArgs(MJPEGOptions{
		RTSPTransport:   "udp",
		HWAccel:         "d3d11va",
		HWAccelDevice:   "1",
		InitHWDevice:    "d3d11va=cam:1",
		VideoDecoder:    "h264_cuvid",
		ProbeSize:       2048000,
		AnalyzeDuration: 3000000,
		LowDelay:        true,
		NoBuffer:        true,
	}, "rtsp://camera/live")

	want := []string{
		"-hide_banner",
		"-loglevel", "error",
		"-fflags", "nobuffer",
		"-flags", "low_delay",
		"-probesize", "2048000",
		"-analyzeduration", "3000000",
		"-init_hw_device", "d3d11va=cam:1",
		"-hwaccel", "d3d11va",
		"-hwaccel_device", "1",
		"-c:v", "h264_cuvid",
		"-rtsp_transport", "udp",
		"-i", "rtsp://camera/live",
	}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("args = %#v, want %#v", args, want)
	}
}

func TestBaseFFmpegArgsOmitsHardwareOptionsWhenDisabled(t *testing.T) {
	args := baseFFmpegArgs(MJPEGOptions{HWAccel: "none", HWAccelDevice: "1"}, "rtsp://camera/live")

	want := []string{
		"-hide_banner",
		"-loglevel", "error",
		"-rtsp_transport", "tcp",
		"-i", "rtsp://camera/live",
	}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("args = %#v, want %#v", args, want)
	}
}
