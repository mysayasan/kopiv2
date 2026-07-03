package talk

import (
	"bufio"
	"bytes"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// TP-Link Tapo/VIGI proprietary talk transport: a long-lived multipart HTTP
// POST on port 8800 authenticated with MD5 digest. The talk channel is opened
// with a JSON request part that returns a session id; audio is then sent as
// "audio/mp2t" parts carrying G.711 A-law in MPEG-TS. Protocol ported from
// go2rtc's pkg/tapo (MIT).

const (
	clientBoundary = "--client-stream-boundary--"
	deviceBoundary = "--device-stream-boundary--"

	tapoDialTimeout  = 8 * time.Second
	tapoWriteTimeout = 5 * time.Second

	// tapoNonePassword is the fixed password of the legacy username="none"
	// exchange (CVE-2022-37255) some old firmware still offers.
	tapoNonePassword = "TPL075526460603"
)

// TapoBrand selects how the port-8800 password is derived.
type TapoBrand string

const (
	// BrandTapo hashes the TP-Link cloud-account password (MD5 or SHA-256
	// uppercase hex depending on the camera's encrypt_type) for user "admin".
	BrandTapo TapoBrand = "tapo"
	// BrandVigi obfuscates the camera admin password with TP-Link's
	// securityEncode scheme.
	BrandVigi TapoBrand = "vigi"
)

// TapoConfig configures a Tapo/VIGI talk session.
type TapoConfig struct {
	// Host is the camera IP; :8800 is assumed when no port is given.
	Host  string
	Brand TapoBrand
	// CloudPassword is the TP-Link cloud account password (BrandTapo).
	CloudPassword string
	// Username/Password are the camera admin credentials (BrandVigi).
	Username string
	Password string
}

type tapoSession struct {
	mu      sync.Mutex
	conn    net.Conn
	muxer   tsMuxer
	session string
	closed  bool
}

// DialTapo opens a talk session to a Tapo/VIGI camera speaker.
func DialTapo(cfg TapoConfig) (Session, error) {
	host := normalizeTapoHost(cfg.Host)

	conn, err := tapoAuthenticatedConn(host, cfg)
	if err != nil {
		return nil, err
	}

	s := &tapoSession{conn: conn}
	sessionID, err := s.request(`{"params":{"talk":{"mode":"aec"},"method":"get"},"seq":3,"type":"request"}`)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("talk request failed: %w", err)
	}
	if strings.TrimSpace(sessionID) == "" {
		conn.Close()
		return nil, errors.New("camera did not return a talk session id (two-way audio may be unsupported or the speaker password is wrong)")
	}
	s.session = sessionID

	// The camera expects the MPEG-TS PAT/PMT once before audio payloads.
	if err := s.writePart(s.muxer.header()); err != nil {
		conn.Close()
		return nil, fmt.Errorf("write talk stream header failed: %w", err)
	}
	return s, nil
}

func (s *tapoSession) WritePCMA(payload []byte, timestamp uint32) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errSessionClosed
	}
	return s.writePart(s.muxer.payload(timestamp, payload))
}

// writePart sends one audio/mp2t multipart part. Callers hold the lock (or are
// still single-threaded during setup).
func (s *tapoSession) writePart(body []byte) error {
	buf := bytes.NewBuffer(nil)
	buf.WriteString("--" + clientBoundary + "\r\n")
	buf.WriteString("Content-Type: audio/mp2t\r\n")
	buf.WriteString("X-If-Encrypt: 0\r\n")
	buf.WriteString("X-Session-Id: " + s.session + "\r\n")
	buf.WriteString("Content-Length: " + strconv.Itoa(len(body)) + "\r\n\r\n")
	buf.Write(body)

	_ = s.conn.SetWriteDeadline(time.Now().Add(tapoWriteTimeout))
	_, err := buf.WriteTo(s.conn)
	return err
}

// request sends one JSON request part and reads the device's JSON response part,
// returning its params.session_id.
func (s *tapoSession) request(body string) (string, error) {
	buf := bytes.NewBuffer(nil)
	buf.WriteString("--" + clientBoundary + "\r\n")
	buf.WriteString("Content-Type: application/json\r\n")
	buf.WriteString("Content-Length: " + strconv.Itoa(len(body)) + "\r\n\r\n")
	buf.WriteString(body)
	buf.WriteString("\r\n")

	_ = s.conn.SetDeadline(time.Now().Add(tapoDialTimeout))
	defer s.conn.SetDeadline(time.Time{})
	if _, err := buf.WriteTo(s.conn); err != nil {
		return "", err
	}

	reader := multipart.NewReader(s.conn, deviceBoundary)
	part, err := reader.NextRawPart()
	if err != nil {
		return "", err
	}
	var v struct {
		Params struct {
			SessionID string `json:"session_id"`
		} `json:"params"`
	}
	if err := json.NewDecoder(part).Decode(&v); err != nil {
		return "", err
	}
	return v.Params.SessionID, nil
}

func (s *tapoSession) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	return s.conn.Close()
}

// tapoAuthenticatedConn dials port 8800 and completes the digest handshake,
// returning the raw connection ready for multipart parts.
func tapoAuthenticatedConn(host string, cfg TapoConfig) (net.Conn, error) {
	req, err := http.NewRequest(http.MethodPost, "http://"+host+"/stream", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "multipart/mixed; boundary="+clientBoundary)

	conn, err := net.DialTimeout("tcp", host, tapoDialTimeout)
	if err != nil {
		return nil, err
	}
	_ = conn.SetDeadline(time.Now().Add(tapoDialTimeout))

	if err := req.Write(conn); err != nil {
		conn.Close()
		return nil, err
	}
	reader := bufio.NewReader(conn)
	res, err := http.ReadResponse(reader, req)
	if err != nil {
		conn.Close()
		return nil, err
	}
	_, _ = io.Copy(io.Discard, res.Body)
	_ = res.Body.Close()

	auth := res.Header.Get("WWW-Authenticate")
	if res.StatusCode != http.StatusUnauthorized || !strings.HasPrefix(auth, "Digest") {
		conn.Close()
		return nil, errors.New("camera talk port returned unexpected status " + res.Status)
	}

	username, password := tapoDigestCredentials(cfg, auth)
	req.Header.Set("Authorization", digestHeader(username, password, http.MethodPost, req.URL.RequestURI(), auth))

	if err := req.Write(conn); err != nil {
		conn.Close()
		return nil, err
	}
	if res, err = http.ReadResponse(reader, req); err != nil {
		conn.Close()
		return nil, err
	}
	// Do NOT drain the body here: it is the endless multipart device stream.
	if res.StatusCode != http.StatusOK {
		conn.Close()
		if res.StatusCode == http.StatusUnauthorized {
			// The password itself is wrong from the camera's point of view. For Tapo
			// this is almost always an account issue, not a typo — surface a code the
			// UI maps to the detailed account/SSO checklist.
			return nil, errors.New("talk-unauthorized: camera rejected the speaker password")
		}
		return nil, errors.New("camera talk auth failed with status " + res.Status)
	}
	_ = conn.SetDeadline(time.Time{})
	return conn, nil
}

// tapoDigestCredentials derives the digest username/password for the camera's
// challenge: the legacy none-exchange, the hashed cloud password (Tapo), or the
// securityEncode-obfuscated admin password (VIGI).
func tapoDigestCredentials(cfg TapoConfig, auth string) (string, string) {
	if strings.Contains(auth, `username="none"`) {
		return "none", tapoNonePassword
	}
	if cfg.Brand == BrandVigi {
		return cfg.Username, securityEncode(cfg.Password)
	}
	if strings.Contains(auth, `encrypt_type="3"`) {
		sum := sha256.Sum256([]byte(cfg.CloudPassword))
		return "admin", strings.ToUpper(hex.EncodeToString(sum[:]))
	}
	sum := md5.Sum([]byte(cfg.CloudPassword))
	return "admin", strings.ToUpper(hex.EncodeToString(sum[:]))
}

// digestHeader builds an RFC 7616 MD5 digest Authorization header for the
// TP-Link challenge (qop=auth).
func digestHeader(username, password, method, uri, challenge string) string {
	realm := headerValue(challenge, "realm")
	nonce := headerValue(challenge, "nonce")
	qop := headerValue(challenge, "qop")
	nc := "00000001"
	cnonce := randHex(16)
	ha1 := md5Join(username, realm, password)
	ha2 := md5Join(method, uri)
	response := md5Join(ha1, nonce, nc, cnonce, qop, ha2)

	header := fmt.Sprintf(
		`Digest username="%s", realm="%s", nonce="%s", uri="%s", qop=%s, nc=%s, cnonce="%s", response="%s"`,
		username, realm, nonce, uri, qop, nc, cnonce, response,
	)
	if opaque := headerValue(challenge, "opaque"); opaque != "" {
		header += fmt.Sprintf(`, opaque="%s", algorithm=MD5`, opaque)
	}
	return header
}

func md5Join(parts ...string) string {
	sum := md5.Sum([]byte(strings.Join(parts, ":")))
	return hex.EncodeToString(sum[:])
}

// headerValue extracts a quoted key="value" parameter from an auth challenge.
func headerValue(header, key string) string {
	_, after, ok := strings.Cut(header, key+`="`)
	if !ok {
		return ""
	}
	value, _, ok := strings.Cut(after, `"`)
	if !ok {
		return ""
	}
	return value
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func normalizeTapoHost(host string) string {
	host = strings.TrimSpace(host)
	if _, _, err := net.SplitHostPort(host); err != nil {
		host += ":8800"
	}
	return host
}

// securityEncode is TP-Link's VIGI password obfuscation (ported from go2rtc).
const (
	vigiKeyShort = "RDpbLfCPsJZ7fiv"
	vigiKeyLong  = "yLwVl0zKqws7LgKPRQ84Mdt708T1qQ3Ha7xv3H7NyU84p21BriUWBU43odz3iP4rBL3cD02KZciXTysVXiV8ngg6vL48rPJyAUw0HurW20xqxv9aYb4M9wK1Ae0wlro510qXeU07kV57fQMc8L6aLgMLwygtc0F10a0Dg70TOoouyFhdysuRMO51yY5ZlOZZLEal1h0t9YQW0Ko7oBwmCAHoic4HYbUyVeU3sfQ1xtXcPcf1aT303wAQhv66qzW"
)

func securityEncode(s string) string {
	size := len(s)
	n := size
	if len(vigiKeyShort) > n {
		n = len(vigiKeyShort)
	}
	b := make([]byte, n)
	for i := 0; i < n; i++ {
		c1 := 187
		c2 := 187
		if i >= size {
			c1 = int(vigiKeyShort[i])
		} else if i >= len(vigiKeyShort) {
			c2 = int(s[i])
		} else {
			c1 = int(vigiKeyShort[i])
			c2 = int(s[i])
		}
		b[i] = vigiKeyLong[(c1^c2)%len(vigiKeyLong)]
	}
	return string(b)
}
