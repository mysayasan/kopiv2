package mailer

import (
	"bufio"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
)

// fakeRelay is a minimal in-process SMTP server. The mailer is tested against a
// real SMTP conversation rather than a mocked transport because every defect
// this package can have — a header that never reaches the wire, a RCPT loop that
// aborts on the first rejection, a body that is not dot-stuffed — lives in the
// protocol exchange, which a mock by definition does not have.
type fakeRelay struct {
	addr string
	ln   net.Listener

	// rejectRcpt makes the relay refuse these addresses at RCPT time
	// (case-insensitive), the way a real relay refuses an unknown mailbox.
	rejectRcpt map[string]bool

	// advertiseAuth makes the relay offer AUTH PLAIN and accept it. It exists so a
	// test can prove the mailer REFUSES to authenticate over cleartext even when
	// the relay would happily take the password.
	advertiseAuth bool

	mu       sync.Mutex
	messages []receivedMessage
	// authLines records every AUTH command the relay saw, so a test can assert a
	// credential never reached the wire rather than merely that a send errored.
	authLines []string
}

type receivedMessage struct {
	From string
	To   []string
	Data string
}

func newFakeRelay(t *testing.T, reject ...string) *fakeRelay {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	r := &fakeRelay{addr: ln.Addr().String(), ln: ln, rejectRcpt: map[string]bool{}}
	for _, a := range reject {
		r.rejectRcpt[strings.ToLower(a)] = true
	}
	go r.serve()
	t.Cleanup(func() { _ = ln.Close() })
	return r
}

func (r *fakeRelay) host() string {
	h, _, _ := net.SplitHostPort(r.addr)
	return h
}

func (r *fakeRelay) port() int {
	var p int
	_, portStr, _ := net.SplitHostPort(r.addr)
	fmt.Sscanf(portStr, "%d", &p)
	return p
}

// sawAuth reports every AUTH command line the relay received.
func (r *fakeRelay) sawAuth() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.authLines))
	copy(out, r.authLines)
	return out
}

func (r *fakeRelay) received() []receivedMessage {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]receivedMessage, len(r.messages))
	copy(out, r.messages)
	return out
}

func (r *fakeRelay) serve() {
	for {
		conn, err := r.ln.Accept()
		if err != nil {
			return
		}
		go r.handle(conn)
	}
}

func (r *fakeRelay) handle(conn net.Conn) {
	defer conn.Close()
	br := bufio.NewReader(conn)
	w := func(s string) { fmt.Fprintf(conn, "%s\r\n", s) }

	w("220 fake.relay ESMTP")
	var msg receivedMessage
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		verb := strings.ToUpper(strings.Fields(line + " ")[0])
		switch verb {
		case "EHLO":
			// No STARTTLS advertised: a TLS handshake here would exercise crypto/tls,
			// not this package. AUTH is offered only when a test asks for it.
			w("250-fake.relay")
			if r.advertiseAuth {
				w("250-AUTH PLAIN LOGIN")
			}
			w("250 SIZE 35882577")
		case "AUTH":
			r.mu.Lock()
			r.authLines = append(r.authLines, line)
			r.mu.Unlock()
			w("235 2.7.0 Authentication successful")
		case "HELO":
			w("250 fake.relay")
		case "MAIL":
			msg = receivedMessage{From: extractAddr(line)}
			w("250 2.1.0 Ok")
		case "RCPT":
			addr := extractAddr(line)
			if r.rejectRcpt[strings.ToLower(addr)] {
				w("550 5.1.1 <" + addr + ">: Recipient address rejected: User unknown")
				continue
			}
			msg.To = append(msg.To, addr)
			w("250 2.1.5 Ok")
		case "DATA":
			w("354 End data with <CR><LF>.<CR><LF>")
			var body strings.Builder
			for {
				dl, err := br.ReadString('\n')
				if err != nil {
					return
				}
				if dl == ".\r\n" || dl == ".\n" {
					break
				}
				// Undo dot-stuffing so the test sees the message as sent.
				if strings.HasPrefix(dl, "..") {
					dl = dl[1:]
				}
				body.WriteString(dl)
			}
			msg.Data = body.String()
			r.mu.Lock()
			r.messages = append(r.messages, msg)
			r.mu.Unlock()
			w("250 2.0.0 Ok: queued")
		case "QUIT":
			w("221 2.0.0 Bye")
			return
		case "RSET":
			w("250 2.0.0 Ok")
		default:
			w("250 2.0.0 Ok")
		}
	}
}

// extractAddr pulls the address out of "MAIL FROM:<a@b>" / "RCPT TO:<a@b>".
func extractAddr(line string) string {
	if i := strings.Index(line, "<"); i >= 0 {
		if j := strings.Index(line[i:], ">"); j > 0 {
			return line[i+1 : i+j]
		}
	}
	if i := strings.Index(line, ":"); i >= 0 {
		return strings.TrimSpace(line[i+1:])
	}
	return ""
}

// relayConfig returns a Config pointed at the fake relay.
func (r *fakeRelay) relayConfig(from string) Config {
	return Config{Enabled: true, Host: r.host(), Port: r.port(), From: from}
}
