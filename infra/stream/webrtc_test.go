package stream

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
)

func TestCreateWebRTCAnswerNegotiatesH264(t *testing.T) {
	manager := NewManagerWithConnector(&fakeConnector{})
	defer manager.Close()

	offerPC, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatalf("create offer peer connection: %v", err)
	}
	defer offerPC.Close()

	if _, err := offerPC.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo, webrtc.RTPTransceiverInit{
		Direction: webrtc.RTPTransceiverDirectionRecvonly,
	}); err != nil {
		t.Fatalf("add receive transceiver: %v", err)
	}

	offer, err := offerPC.CreateOffer(nil)
	if err != nil {
		t.Fatalf("create offer: %v", err)
	}
	gatherComplete := webrtc.GatheringCompletePromise(offerPC)
	if err := offerPC.SetLocalDescription(offer); err != nil {
		t.Fatalf("set local description: %v", err)
	}
	<-gatherComplete

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	answer, err := manager.CreateWebRTCAnswer(ctx, Source{ID: "camera-1", URI: "rtsp://camera/live"}, SessionDescription{
		Type: "offer",
		SDP:  offerPC.LocalDescription().SDP,
	})
	if err != nil {
		t.Fatalf("create answer: %v", err)
	}
	if answer.Type != "answer" {
		t.Fatalf("unexpected answer type %q", answer.Type)
	}
	if !strings.Contains(answer.SDP, "H264") {
		t.Fatalf("answer SDP does not include H264:\n%s", answer.SDP)
	}
}

func TestCreateWebRTCAnswerStreamsH264RTP(t *testing.T) {
	manager := NewManagerWithConnector(&streamingFakeConnector{})
	defer manager.Close()

	offerPC, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatalf("create offer peer connection: %v", err)
	}
	defer offerPC.Close()

	received := make(chan *rtp.Packet, 1)
	offerPC.OnTrack(func(track *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		packet, _, err := track.ReadRTP()
		if err == nil {
			received <- packet
		}
	})

	if _, err := offerPC.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo, webrtc.RTPTransceiverInit{
		Direction: webrtc.RTPTransceiverDirectionRecvonly,
	}); err != nil {
		t.Fatalf("add receive transceiver: %v", err)
	}

	offer, err := offerPC.CreateOffer(nil)
	if err != nil {
		t.Fatalf("create offer: %v", err)
	}
	gatherComplete := webrtc.GatheringCompletePromise(offerPC)
	if err := offerPC.SetLocalDescription(offer); err != nil {
		t.Fatalf("set local description: %v", err)
	}
	<-gatherComplete

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	answer, err := manager.CreateWebRTCAnswer(ctx, Source{ID: "camera-1", URI: "rtsp://camera/live"}, SessionDescription{
		Type: "offer",
		SDP:  offerPC.LocalDescription().SDP,
	})
	if err != nil {
		t.Fatalf("create answer: %v", err)
	}
	if err := offerPC.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeAnswer,
		SDP:  answer.SDP,
	}); err != nil {
		t.Fatalf("set remote description: %v", err)
	}

	select {
	case packet := <-received:
		if len(packet.Payload) == 0 {
			t.Fatalf("received empty RTP payload")
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for live RTP packet")
	}
}

type fakeConnector struct{}

func (f *fakeConnector) Subscribe(source Source) (*Subscription, error) {
	packets := make(chan *rtp.Packet)
	return &Subscription{
		Codec:   CodecH264,
		Packets: packets,
		Close: func() {
			close(packets)
		},
	}, nil
}

func (f *fakeConnector) Close() error {
	return nil
}

type streamingFakeConnector struct{}

func (f *streamingFakeConnector) Subscribe(source Source) (*Subscription, error) {
	packets := make(chan *rtp.Packet, 16)
	done := make(chan struct{})
	go func() {
		defer close(packets)
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
		var sequence uint16
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				sequence++
				packet := &rtp.Packet{
					Header: rtp.Header{
						Version:        2,
						PayloadType:    96,
						SequenceNumber: sequence,
						Timestamp:      uint32(sequence) * 3000,
						SSRC:           1234,
						Marker:         true,
					},
					Payload: []byte{0x65, 0x88, 0x84},
				}
				select {
				case packets <- packet:
				default:
				}
			}
		}
	}()
	return &Subscription{
		Codec:   CodecH264,
		Packets: packets,
		Close: func() {
			close(done)
		},
	}, nil
}

func (f *streamingFakeConnector) Close() error {
	return nil
}
