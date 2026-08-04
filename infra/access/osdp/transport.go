package osdp

import (
	"context"
	"io"
	"net"
	"sync/atomic"
	"time"
)

// Transport is the byte pipe a Bus owns for the life of the process.
//
// It is deliberately just io.ReadWriteCloser. Read deadlines are NOT part of the interface: the Bus
// runs a dedicated reader goroutine and applies timeouts with a select, which keeps every transport
// — TCP, serial, or an in-memory pipe in a test — equally simple. Serial ports and net.Conn express
// deadlines completely differently, and pushing that difference into an interface buys nothing.
type Transport = io.ReadWriteCloser

// DialTCP connects to an OSDP endpoint over TCP: the simulator during development, or a
// serial-to-Ethernet gateway in the field.
//
// Serial (go.bug.st/serial, already vendored for Modbus) is build-order step 5 and lands with the
// first real reader on the bench, because the line settings and the CRC byte order are things to
// confirm against hardware rather than to guess at now. When it arrives it will note two carry-overs
// from infra/iot/modbus: the (0, nil) read-timeout quirk at serial.go:71-79, and that a multi-drop
// bus is one talker at a time. What it must NOT copy is DialSerial's open→poll→close lifetime — a
// CP holds the port open permanently.
func DialTCP(ctx context.Context, addr string) (Transport, error) {
	var d net.Dialer
	return d.DialContext(ctx, "tcp", addr)
}

// countingReader tallies bytes actually read off the wire.
//
// This exists for one diagnosis that is otherwise impossible. When two readers sit at the same
// address — the out-of-box case, since PDs ship at address 0 — their replies collide and the result
// almost never survives framing. At the frame layer that is INDISTINGUISHABLE from an empty address:
// both produce nothing. Comparing bytes-read against frames-decoded is the only signal that
// separates "two readers fighting" from "no reader here", and onboarding has to tell them apart.
type countingReader struct {
	r     io.Reader
	bytes atomic.Uint64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	if n > 0 {
		c.bytes.Add(uint64(n))
	}
	return n, err
}

func (c *countingReader) count() uint64 { return c.bytes.Load() }

// sleepCtx waits for d, returning false if the context is cancelled first.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
