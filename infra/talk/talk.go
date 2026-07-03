// Package talk implements one-way "talk-back" audio delivery from the browser
// microphone to a camera's built-in speaker (two-way audio). Two transports are
// supported:
//
//   - The standard ONVIF audio backchannel: an RTSP session opened with
//     "Require: www.onvif.org/ver20/backchannel" whose sendonly G.711 media the
//     server pushes RTP into (Hikvision, Dahua, Axis, most Profile T cameras).
//   - The TP-Link proprietary protocol used by Tapo and VIGI consumer cameras:
//     a multipart HTTP session on port 8800 carrying G.711 A-law audio wrapped
//     in MPEG-TS parts (these cameras do not expose an RTSP backchannel).
//     Protocol reference: github.com/AlexxIT/go2rtc (pkg/tapo, MIT).
//
// All sessions consume G.711 A-law (PCMA, 8 kHz) frames — the codec browsers
// can encode natively in WebRTC — and convert internally when the camera wants
// µ-law.
package talk

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
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

// Probe8800Result describes a TP-Link (Tapo/VIGI) talk service found on port 8800.
type Probe8800Result struct {
	// Supported is true when the port answered with the TP-Link digest challenge.
	Supported bool
	// EncryptType is the encrypt_type from the challenge: 3 means the cloud
	// password is hashed with SHA-256, otherwise MD5.
	EncryptType int
	// NoneAuth is true when the camera advertises the legacy username="none"
	// exchange (CVE-2022-37255) — no cloud password needed.
	NoneAuth bool
}

// Probe8800 checks whether host answers the TP-Link proprietary stream protocol
// on port 8800. host may include a port; :8800 is assumed otherwise. Only a
// genuine TP-Link "Streamd" service (its speaker/talk daemon) is reported — this
// is the fingerprint that gates the TP-Link talk config in the UI, so unrelated
// devices that merely have port 8800 open are never misdetected.
func Probe8800(ctx context.Context, host string) Probe8800Result {
	host = normalizeTapoHost(host)
	dialer := &net.Dialer{Timeout: probeTimeout}
	conn, err := dialer.DialContext(ctx, "tcp", host)
	if err != nil {
		return Probe8800Result{}
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(probeTimeout))

	req, err := http.NewRequest(http.MethodPost, "http://"+host+"/stream", nil)
	if err != nil {
		return Probe8800Result{}
	}
	req.Header.Set("Content-Type", "multipart/mixed; boundary="+clientBoundary)
	if err := req.Write(conn); err != nil {
		return Probe8800Result{}
	}
	res, err := http.ReadResponse(bufio.NewReader(conn), req)
	if err != nil {
		return Probe8800Result{}
	}
	_, _ = io.Copy(io.Discard, res.Body)
	_ = res.Body.Close()

	auth := res.Header.Get("WWW-Authenticate")
	if res.StatusCode != http.StatusUnauthorized || !strings.HasPrefix(auth, "Digest") {
		return Probe8800Result{}
	}
	// Require the TP-Link fingerprint so an unrelated device that merely has port
	// 8800 open (and answers with some other Digest service) is not misdetected as
	// a Tapo/VIGI speaker. TP-Link's streaming daemon reports Server "Streamd" and
	// a "TP-Link IP-Camera" realm.
	if !isTPLinkStreamd(res.Header.Get("Server"), auth) {
		return Probe8800Result{}
	}
	out := Probe8800Result{Supported: true}
	if strings.Contains(auth, `encrypt_type="3"`) {
		out.EncryptType = 3
	}
	if strings.Contains(auth, `username="none"`) {
		out.NoneAuth = true
	}
	return out
}

// isTPLinkStreamd reports whether an 8800 response looks like TP-Link's Streamd
// talk service, from the Server header and/or the auth challenge realm.
func isTPLinkStreamd(server, auth string) bool {
	if strings.EqualFold(strings.TrimSpace(server), "Streamd") {
		return true
	}
	lower := strings.ToLower(auth)
	return strings.Contains(lower, "tp-link") || strings.Contains(lower, "ip-camera")
}

var errSessionClosed = errors.New("talk session closed")
