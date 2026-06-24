package recording

import (
	"context"
	"strings"
	"testing"
)

// remuxVideoArgs must leave the historical stream-copy behaviour untouched for the
// default ("copy"/empty) codec so existing installs re-mux exactly as before.
func TestRemuxVideoArgsCopy(t *testing.T) {
	for _, codec := range []string{"", "copy", "unknown"} {
		args, stored := remuxVideoArgs(codec, 0)
		if strings.Join(args, " ") != "-c copy" {
			t.Fatalf("codec %q: expected stream copy, got %v", codec, args)
		}
		if stored != "" {
			t.Fatalf("codec %q: copy mode must not assert a stored codec, got %q", codec, stored)
		}
		if reencodes(codec) {
			t.Fatalf("codec %q must not re-encode", codec)
		}
	}
}

func TestRemuxVideoArgsHEVC(t *testing.T) {
	args, stored := remuxVideoArgs("hevc", 0)
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "hevc_nvenc") {
		t.Fatalf("expected hevc_nvenc encoder, got %v", args)
	}
	if !strings.Contains(joined, "-c:a copy") {
		t.Fatalf("audio must be copied (already AAC from capture), got %v", args)
	}
	if !strings.Contains(joined, "-tag:v hvc1") {
		t.Fatalf("HEVC-in-mp4 needs the hvc1 tag for Safari/MSE playback, got %v", args)
	}
	// Default CQ applied when none configured.
	if !strings.Contains(joined, "-cq 26") {
		t.Fatalf("expected default CQ 26, got %v", args)
	}
	if stored != "hevc" {
		t.Fatalf("expected stored codec hevc, got %q", stored)
	}
	if !reencodes("hevc") {
		t.Fatal("hevc must re-encode")
	}
}

func TestRemuxVideoArgsH264Quality(t *testing.T) {
	args, stored := remuxVideoArgs("h264", 30)
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "h264_nvenc") || !strings.Contains(joined, "-cq 30") {
		t.Fatalf("expected h264_nvenc at cq 30, got %v", args)
	}
	if strings.Contains(joined, "hvc1") {
		t.Fatalf("h264 must not carry the hvc1 tag, got %v", args)
	}
	if stored != "h264" {
		t.Fatalf("expected stored codec h264, got %q", stored)
	}
}

// The NVENC semaphore must cap concurrency and release slots so it never blocks
// forever once an encode finishes.
func TestNVENCSemaphore(t *testing.T) {
	SetNVENCConcurrency(1)
	rel1, err := AcquireNVENC(context.Background())
	if err != nil {
		t.Fatalf("first acquire failed: %v", err)
	}

	// Second acquire must block while the only slot is held; a cancelled context
	// returns promptly instead of deadlocking.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := AcquireNVENC(ctx); err == nil {
		t.Fatal("expected cancelled acquire to fail while slot held")
	}

	// Releasing frees the slot for the next acquire.
	rel1()
	rel2, err := AcquireNVENC(context.Background())
	if err != nil {
		t.Fatalf("acquire after release failed: %v", err)
	}
	rel2()
	rel2() // release must be idempotent
}
