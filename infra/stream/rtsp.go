package stream

import (
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

const (
	rtspReadyTimeout       = 12 * time.Second
	rtspSubscriberBufSize  = 256
	rtspReadTimeout        = 15 * time.Second
	rtspWriteTimeout       = 10 * time.Second
	rtspUDPInitialTimeout  = 2 * time.Second
	rtspUDPReadBufferBytes = 2 * 1024 * 1024
)

// RTSPConnector shares one RTSP reader per camera URI and fans RTP packets out
// to browser WebRTC peer subscriptions.
type RTSPConnector struct {
	mu       sync.Mutex
	sessions map[string]*rtspSession
}

// NewRTSPConnector creates the default camera stream connector.
func NewRTSPConnector() *RTSPConnector {
	return &RTSPConnector{sessions: map[string]*rtspSession{}}
}

func (c *RTSPConnector) Subscribe(source Source) (*Subscription, error) {
	source.URI = strings.TrimSpace(source.URI)
	if source.URI == "" {
		return nil, errors.New("rtsp uri is required")
	}
	key := source.ID
	if strings.TrimSpace(key) == "" {
		key = source.URI
	}

	c.mu.Lock()
	session := c.sessions[key]
	var replaced *rtspSession
	if session == nil || session.isStopped() || session.uri != source.URI {
		if session != nil && !session.isStopped() && session.uri != source.URI {
			replaced = session
		}
		session = newRTSPSession(key, source.URI, func() {
			c.remove(key, session)
		})
		c.sessions[key] = session
		session.start()
	}
	c.mu.Unlock()
	if replaced != nil {
		replaced.stop()
	}

	return session.subscribe()
}

func (c *RTSPConnector) Close() error {
	c.mu.Lock()
	sessions := make([]*rtspSession, 0, len(c.sessions))
	for _, session := range c.sessions {
		sessions = append(sessions, session)
	}
	c.sessions = map[string]*rtspSession{}
	c.mu.Unlock()

	for _, session := range sessions {
		session.stop()
	}
	return nil
}

func (c *RTSPConnector) remove(key string, target *rtspSession) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.sessions[key] == target {
		delete(c.sessions, key)
	}
}

type rtspSession struct {
	key      string
	uri      string
	onStop   func()
	stopCh   chan struct{}
	stopOnce sync.Once

	mu                 sync.Mutex
	codec              Codec
	audioCodec         Codec
	h264ProfileLevelID string
	readyErr           error
	readyCh               chan struct{}
	stopped               bool
	subscribers           map[uint64]chan *rtp.Packet
	audioSubscribers      map[uint64]chan *rtp.Packet
	nextSubID             uint64
}

func newRTSPSession(key string, uri string, onStop func()) *rtspSession {
	return &rtspSession{
		key:              key,
		uri:              uri,
		onStop:           onStop,
		stopCh:           make(chan struct{}),
		readyCh:          make(chan struct{}),
		subscribers:      map[uint64]chan *rtp.Packet{},
		audioSubscribers: map[uint64]chan *rtp.Packet{},
	}
}

func (s *rtspSession) start() {
	go s.run()
}

func (s *rtspSession) subscribe() (*Subscription, error) {
	select {
	case <-s.readyCh:
	case <-time.After(rtspReadyTimeout):
		return nil, errors.New("camera stream did not become ready in time")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.readyErr != nil {
		return nil, s.readyErr
	}
	if s.stopped {
		return nil, errors.New("camera stream stopped")
	}

	id := s.nextSubID
	s.nextSubID++
	packets := make(chan *rtp.Packet, rtspSubscriberBufSize)
	s.subscribers[id] = packets

	var audioPackets chan *rtp.Packet
	if s.audioCodec != "" {
		audioPackets = make(chan *rtp.Packet, rtspSubscriberBufSize)
		s.audioSubscribers[id] = audioPackets
	}

	var closeOnce sync.Once
	return &Subscription{
		Codec:              s.codec,
		Packets:            packets,
		AudioCodec:         s.audioCodec,
		AudioPackets:       audioPackets,
		H264ProfileLevelID: s.h264ProfileLevelID,
		Close: func() {
			closeOnce.Do(func() {
				s.removeSubscriber(id)
			})
		},
	}, nil
}

func (s *rtspSession) removeSubscriber(id uint64) {
	var shouldStop bool
	s.mu.Lock()
	if packets, ok := s.subscribers[id]; ok {
		delete(s.subscribers, id)
		close(packets)
	}
	if packets, ok := s.audioSubscribers[id]; ok {
		delete(s.audioSubscribers, id)
		close(packets)
	}
	shouldStop = len(s.subscribers) == 0
	s.mu.Unlock()

	if shouldStop {
		s.stop()
	}
}

func (s *rtspSession) stop() {
	s.stopOnce.Do(func() {
		close(s.stopCh)
	})
}

func (s *rtspSession) isStopped() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopped
}

func (s *rtspSession) run() {
	defer func() {
		s.finish(nil)
		if s.onStop != nil {
			s.onStop()
		}
	}()

	u, err := base.ParseURL(s.uri)
	if err != nil {
		s.finish(fmt.Errorf("parse RTSP URL failed: %w", err))
		return
	}
	if u.Scheme != "rtsp" && u.Scheme != "rtsps" {
		s.finish(fmt.Errorf("unsupported RTSP scheme %q", u.Scheme))
		return
	}

	proto := gortsplib.ProtocolTCP
	client := &gortsplib.Client{
		Scheme:                u.Scheme,
		Host:                  u.Host,
		Protocol:              &proto,
		ReadTimeout:           rtspReadTimeout,
		WriteTimeout:          rtspWriteTimeout,
		InitialUDPReadTimeout: rtspUDPInitialTimeout,
		UDPReadBufferSize:     rtspUDPReadBufferBytes,
	}

	if err := client.Start(); err != nil {
		s.finish(fmt.Errorf("start RTSP client failed: %w", err))
		return
	}
	defer client.Close()

	desc, _, err := client.Describe(u)
	if err != nil {
		s.finish(fmt.Errorf("describe RTSP stream failed: %w", err))
		return
	}

	media, h264Format := firstH264(desc)
	if media == nil || h264Format == nil {
		s.finish(errors.New("camera RTSP stream does not expose an H264 video track for WebRTC"))
		return
	}

	audioMedia, g711Format := firstG711(desc)
	var audioCodec Codec
	if g711Format != nil {
		if g711Format.MULaw {
			audioCodec = CodecPCMU
		} else {
			audioCodec = CodecPCMA
		}
	}

	if err := client.SetupAll(desc.BaseURL, desc.Medias); err != nil {
		s.finish(fmt.Errorf("setup RTSP medias failed: %w", err))
		return
	}

	client.OnPacketRTP(media, h264Format, func(pkt *rtp.Packet) {
		s.broadcast(pkt.Clone())
	})
	if audioMedia != nil && g711Format != nil {
		client.OnPacketRTP(audioMedia, g711Format, func(pkt *rtp.Packet) {
			s.broadcastAudio(pkt.Clone())
		})
	}

	s.markReady(CodecH264, audioCodec, h264ProfileLevelID(h264Format))

	if _, err := client.Play(nil); err != nil {
		s.finish(fmt.Errorf("play RTSP stream failed: %w", err))
		return
	}

	waitCh := make(chan error, 1)
	go func() {
		waitCh <- client.Wait()
	}()

	select {
	case <-s.stopCh:
	case err := <-waitCh:
		if err != nil {
			s.finish(fmt.Errorf("RTSP stream ended: %w", err))
		}
	}
}

func firstH264(desc *description.Session) (*description.Media, *format.H264) {
	for _, media := range desc.Medias {
		for _, forma := range media.Formats {
			if h264, ok := forma.(*format.H264); ok {
				return media, h264
			}
		}
	}
	return nil, nil
}

// h264ProfileLevelID extracts the 6-hex-digit SDP profile-level-id from the SPS
// embedded in the RTSP DESCRIBE response. Falls back to "42e01f" (Baseline 3.1)
// when no SPS is present so the SDP remains valid.
func h264ProfileLevelID(f *format.H264) string {
	if f != nil && len(f.SPS) >= 4 {
		return fmt.Sprintf("%02x%02x%02x", f.SPS[1], f.SPS[2], f.SPS[3])
	}
	return "42e01f"
}

func firstG711(desc *description.Session) (*description.Media, *format.G711) {
	for _, media := range desc.Medias {
		for _, forma := range media.Formats {
			if g711, ok := forma.(*format.G711); ok {
				return media, g711
			}
		}
	}
	return nil, nil
}

func (s *rtspSession) markReady(codec Codec, audioCodec Codec, h264ProfileLevelID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.readyErr == nil && s.codec == "" {
		s.codec = codec
		s.audioCodec = audioCodec
		s.h264ProfileLevelID = h264ProfileLevelID
		close(s.readyCh)
	}
}

func (s *rtspSession) broadcastAudio(pkt *rtp.Packet) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, packets := range s.audioSubscribers {
		select {
		case packets <- pkt.Clone():
		default:
		}
	}
}

func (s *rtspSession) finish(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil && s.readyErr == nil && s.codec == "" {
		s.readyErr = err
		close(s.readyCh)
	}
	if s.stopped {
		return
	}
	s.stopped = true
	for id, packets := range s.subscribers {
		delete(s.subscribers, id)
		close(packets)
	}
	for id, packets := range s.audioSubscribers {
		delete(s.audioSubscribers, id)
		close(packets)
	}
}

func (s *rtspSession) broadcast(pkt *rtp.Packet) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, packets := range s.subscribers {
		select {
		case packets <- pkt.Clone():
		default:
			// Drop stale packets for slow browsers; live view should stay current.
		}
	}
}
