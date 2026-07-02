// Package talk implements one-way "talk-back" audio delivery from the browser
// microphone to a camera's built-in speaker (two-way audio) over the standard
// ONVIF audio backchannel: an RTSP session opened with
// "Require: www.onvif.org/ver20/backchannel" whose sendonly G.711 media the
// server pushes RTP into (Hikvision, Dahua, Axis, most Profile T cameras).
//
// Sessions consume G.711 A-law (PCMA, 8 kHz) frames — the codec browsers can
// encode natively in WebRTC — and convert internally when the camera wants
// µ-law.
package talk

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/bluenviron/gortsplib/v5"
	"github.com/bluenviron/gortsplib/v5/pkg/base"
)

// Session is one open audio path to a camera's speaker.
type Session interface {
	// WritePCMA sends one frame of G.711 A-law samples. timestamp is the RTP
	// timestamp in 8 kHz units (only deltas matter).
	WritePCMA(payload []byte, timestamp uint32) error
	Close() error
}

const probeTimeout = 6 * time.Second

// HasBackchannel reports whether the camera's RTSP endpoint advertises an ONVIF
// audio backchannel with a G.711 format (a DESCRIBE sent with the backchannel
// Require header).
func HasBackchannel(ctx context.Context, rtspURI string) bool {
	u, err := base.ParseURL(strings.TrimSpace(rtspURI))
	if err != nil {
		return false
	}
	proto := gortsplib.ProtocolTCP
	client := &gortsplib.Client{
		Scheme:              u.Scheme,
		Host:                u.Host,
		Protocol:            &proto,
		ReadTimeout:         probeTimeout,
		WriteTimeout:        probeTimeout,
		RequestBackChannels: true,
	}
	if err := client.Start(); err != nil {
		return false
	}
	defer client.Close()

	done := make(chan bool, 1)
	go func() {
		desc, _, err := client.Describe(u)
		if err != nil {
			done <- false
			return
		}
		media, _ := findG711Backchannel(desc)
		done <- media != nil
	}()
	select {
	case ok := <-done:
		return ok
	case <-ctx.Done():
		return false
	}
}

var errSessionClosed = errors.New("talk session closed")
