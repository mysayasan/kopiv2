package talk

import (
	"crypto/rand"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/bluenviron/gortsplib/v5"
	"github.com/bluenviron/gortsplib/v5/pkg/base"
	"github.com/bluenviron/gortsplib/v5/pkg/description"
	"github.com/bluenviron/gortsplib/v5/pkg/format"
	"github.com/pion/rtp"
)

// onvifSession pushes G.711 RTP into a camera's ONVIF audio backchannel opened
// over RTSP. The browser always supplies A-law (PCMA); this converts to µ-law
// when the camera's backchannel advertises PCMU.
type onvifSession struct {
	mu     sync.Mutex
	client *gortsplib.Client
	media  *description.Media
	format *format.G711

	payloadType byte
	muLaw       bool
	ssrc        uint32
	seq         uint16
	closed      bool
}

// DialONVIF opens an ONVIF RTSP audio backchannel to the camera and returns a
// session ready to accept PCMA frames. It errors when the camera exposes no
// G.711 backchannel.
func DialONVIF(rtspURI string) (Session, error) {
	u, err := base.ParseURL(strings.TrimSpace(rtspURI))
	if err != nil {
		return nil, fmt.Errorf("parse rtsp url failed: %w", err)
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
		return nil, fmt.Errorf("start rtsp client failed: %w", err)
	}

	desc, _, err := client.Describe(u)
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("describe rtsp stream failed: %w", err)
	}
	media, forma := findG711Backchannel(desc)
	if media == nil || forma == nil {
		client.Close()
		return nil, errors.New("camera does not expose a G.711 ONVIF audio backchannel")
	}
	if _, err := client.Setup(desc.BaseURL, media, 0, 0); err != nil {
		client.Close()
		return nil, fmt.Errorf("setup backchannel failed: %w", err)
	}
	if _, err := client.Play(nil); err != nil {
		client.Close()
		return nil, fmt.Errorf("play backchannel failed: %w", err)
	}

	return &onvifSession{
		client:      client,
		media:       media,
		format:      forma,
		payloadType: forma.PayloadType(),
		muLaw:       forma.MULaw,
		ssrc:        randUint32(),
	}, nil
}

func (s *onvifSession) WritePCMA(payload []byte, timestamp uint32) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errSessionClosed
	}
	if s.muLaw {
		payload = alawToUlaw(payload)
	}
	pkt := &rtp.Packet{
		Header: rtp.Header{
			Version:        2,
			PayloadType:    s.payloadType,
			SequenceNumber: s.seq,
			Timestamp:      timestamp,
			SSRC:           s.ssrc,
		},
		Payload: payload,
	}
	s.seq++
	return s.client.WritePacketRTP(s.media, pkt)
}

func (s *onvifSession) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	s.client.Close()
	return nil
}

// findG711Backchannel returns the first backchannel media that carries a G.711
// format (the near-universal talk-back codec).
func findG711Backchannel(desc *description.Session) (*description.Media, *format.G711) {
	for _, media := range desc.Medias {
		if !media.IsBackChannel {
			continue
		}
		for _, forma := range media.Formats {
			if g711, ok := forma.(*format.G711); ok {
				return media, g711
			}
		}
	}
	return nil, nil
}

func randUint32() uint32 {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return uint32(time.Now().UnixNano())
	}
	return uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
}
