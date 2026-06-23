package stream

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"os/exec"
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
	// gopCacheMaxPackets bounds the per-session group-of-pictures cache used to
	// prime late-joining subscribers with a keyframe. When a GOP exceeds this
	// (a camera with an unusually long keyframe interval), priming is skipped
	// for that GOP and the subscriber waits for the next keyframe, rather than
	// being primed with an incomplete GOP that would still slide.
	gopCacheMaxPackets = 1024
)

// RTSPConnector shares one RTSP reader per camera URI and fans RTP packets out
// to browser WebRTC peer subscriptions.
type RTSPConnector struct {
	mu         sync.Mutex
	sessions   map[string]*rtspSession
	ffmpegPath string
}

// NewRTSPConnector creates the default camera stream connector. ffmpegPath enables
// transcoding non-G.711 camera audio to Opus for live view (empty = disabled).
func NewRTSPConnector(ffmpegPath string) *RTSPConnector {
	return &RTSPConnector{sessions: map[string]*rtspSession{}, ffmpegPath: strings.TrimSpace(ffmpegPath)}
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
		session = newRTSPSession(key, source.URI, c.ffmpegPath, func() {
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
	key        string
	uri        string
	ffmpegPath string
	onStop     func()
	stopCh     chan struct{}
	stopOnce   sync.Once

	mu                 sync.Mutex
	codec              Codec
	audioCodec         Codec
	h264ProfileLevelID string
	readyErr           error
	readyCh            chan struct{}
	stopped            bool
	subscribers        map[uint64]chan *rtp.Packet
	audioSubscribers   map[uint64]chan *rtp.Packet
	nextSubID          uint64

	// gop holds the current group-of-pictures (most recent keyframe onward) so a
	// late subscriber can be primed and start decoding on a keyframe. haveKeyframe
	// is false until the first keyframe is seen; gopOverflow disables caching for
	// an over-long GOP until the next keyframe resets it.
	gop          []*rtp.Packet
	haveKeyframe bool
	gopOverflow  bool
}

func newRTSPSession(key string, uri string, ffmpegPath string, onStop func()) *rtspSession {
	return &rtspSession{
		key:              key,
		uri:              uri,
		ffmpegPath:       ffmpegPath,
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

	// Snapshot the current GOP atomically with registration: the next broadcast
	// (holding the same lock) appends to the channel, so the live stream resumes
	// exactly where this backlog ends — no gap, no duplicated packet.
	var backlog []*rtp.Packet
	if len(s.gop) > 0 {
		backlog = make([]*rtp.Packet, len(s.gop))
		copy(backlog, s.gop)
	}

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
		Backlog:            backlog,
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
	transcodeAudio := false
	if g711Format != nil {
		// G.711 is browser-native — forward its RTP unchanged (no transcode).
		if g711Format.MULaw {
			audioCodec = CodecPCMU
		} else {
			audioCodec = CodecPCMA
		}
	} else if s.ffmpegPath != "" && firstAudioMedia(desc) != nil {
		// Non-G.711 audio (AAC, etc.): transcode to Opus via ffmpeg so live view
		// has sound regardless of the camera's codec. gortsplib handles video only;
		// a separate ffmpeg leg pulls + transcodes the audio.
		audioCodec = CodecOpus
		transcodeAudio = true
	}

	// When transcoding, set up only the video media here; the audio is handled by
	// the ffmpeg transcoder, so gortsplib should not also pull the audio track.
	mediasToSetup := desc.Medias
	if transcodeAudio {
		mediasToSetup = []*description.Media{media}
	}
	if err := client.SetupAll(desc.BaseURL, mediasToSetup); err != nil {
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

	if transcodeAudio {
		go s.runAudioTranscoder()
	}

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

// firstAudioMedia returns the first audio media of any codec (AAC, G.726, etc.).
func firstAudioMedia(desc *description.Session) *description.Media {
	for _, media := range desc.Medias {
		if media.Type == description.MediaTypeAudio {
			return media
		}
	}
	return nil
}

// runAudioTranscoder pulls the camera's (non-G.711) audio with a dedicated ffmpeg
// process, transcodes it to Opus, and emits RTP to a loopback UDP port whose
// packets are broadcast to audio subscribers as Opus. It restarts ffmpeg on
// failure and exits when the session stops. A transcoder failure only costs live
// audio for this camera (never the video path), so it is best-effort.
func (s *rtspSession) runAudioTranscoder() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		select {
		case <-s.stopCh:
			cancel()
		case <-ctx.Done():
		}
	}()

	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		log.Printf("stream %s: audio transcode listen failed: %v", s.key, err)
		return
	}
	defer conn.Close()
	go func() {
		<-ctx.Done()
		conn.Close()
	}()
	port := conn.LocalAddr().(*net.UDPAddr).Port

	// Reader: drain ffmpeg's Opus RTP into the audio broadcast until the socket
	// closes (on session stop).
	go func() {
		buf := make([]byte, 1600)
		for {
			n, err := conn.Read(buf)
			if err != nil {
				return
			}
			pkt := &rtp.Packet{}
			if pkt.Unmarshal(buf[:n]) != nil {
				continue
			}
			s.broadcastAudio(pkt)
		}
	}()

	for ctx.Err() == nil {
		args := []string{
			"-hide_banner", "-loglevel", "error",
			"-fflags", "+nobuffer", "-flags", "low_delay",
			"-rtsp_transport", "tcp",
			"-allowed_media_types", "audio",
			"-i", s.uri,
			"-vn",
			"-c:a", "libopus",
			"-ar", "48000", "-ac", "2", "-b:a", "64k",
			"-application", "lowdelay",
			"-payload_type", "111",
			"-f", "rtp",
			fmt.Sprintf("rtp://127.0.0.1:%d", port),
		}
		cmd := exec.CommandContext(ctx, s.ffmpegPath, args...)
		if stderr, perr := cmd.StderrPipe(); perr == nil {
			go func() {
				sc := bufio.NewScanner(stderr)
				for sc.Scan() {
					if line := strings.TrimSpace(sc.Text()); line != "" {
						log.Printf("stream %s audio ffmpeg: %s", s.key, line)
					}
				}
			}()
		}
		_ = cmd.Run()
		if ctx.Err() != nil {
			return
		}
		// Brief backoff before reconnecting (camera hiccup / transient failure).
		select {
		case <-ctx.Done():
			return
		case <-time.After(3 * time.Second):
		}
	}
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
	s.cacheForGOP(pkt)
	for _, packets := range s.subscribers {
		select {
		case packets <- pkt.Clone():
		default:
			// Drop stale packets for slow browsers; live view should stay current.
		}
	}
}

// cacheForGOP maintains the current group-of-pictures for late-subscriber
// priming. pkt is already a session-owned clone. A fresh slice is allocated on
// each keyframe so snapshots taken by subscribers are never mutated underneath
// them.
func (s *rtspSession) cacheForGOP(pkt *rtp.Packet) {
	if pkt == nil {
		return
	}
	if h264IsKeyframeStart(pkt.Payload) {
		s.gop = []*rtp.Packet{pkt}
		s.haveKeyframe = true
		s.gopOverflow = false
		return
	}
	if !s.haveKeyframe || s.gopOverflow {
		return
	}
	if len(s.gop) >= gopCacheMaxPackets {
		// GOP too large to replay cleanly; drop it and wait for the next keyframe.
		s.gop = nil
		s.haveKeyframe = false
		s.gopOverflow = true
		return
	}
	s.gop = append(s.gop, pkt)
}

// h264IsKeyframeStart reports whether an H264 RTP payload begins a keyframe
// access unit — i.e. it carries (or starts) an SPS (NAL type 7) or IDR slice
// (type 5), across single-NAL, STAP-A aggregated, and FU-A fragmented packets.
func h264IsKeyframeStart(payload []byte) bool {
	if len(payload) < 1 {
		return false
	}
	switch nalType := payload[0] & 0x1f; nalType {
	case 5, 7: // single NAL unit: IDR slice or SPS
		return true
	case 24: // STAP-A: one or more aggregated NAL units
		off := 1
		for off+2 <= len(payload) {
			size := int(payload[off])<<8 | int(payload[off+1])
			off += 2
			if size == 0 || off+size > len(payload) {
				break
			}
			if t := payload[off] & 0x1f; t == 5 || t == 7 {
				return true
			}
			off += size
		}
	case 28: // FU-A: only the start fragment marks the access unit boundary
		if len(payload) >= 2 && payload[1]&0x80 != 0 {
			if t := payload[1] & 0x1f; t == 5 || t == 7 {
				return true
			}
		}
	}
	return false
}
