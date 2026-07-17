// Package modbus is a minimal Modbus client plus the two ways myiotsan reads a device: SunSpec
// auto-discovery (infra/iot/sunspec) for compliant hardware, and a manual register map for the
// vendors that are not. Both normalise to codec.Sample, so everything downstream — deadband,
// storage, rules, dashboards — is identical regardless of how the bytes were fetched.
//
// Three transports share the same function-code layer (read holding/input, write single/multiple):
// MBAP over TCP (the default), RTU over a "transparent" TCP gateway, and RTU over a serial line. The
// framing lives in transport.go; the serial line is the one external dependency (go.bug.st/serial,
// pure Go). Everything else is stdlib.
package modbus

import (
	"encoding/binary"
	"fmt"
	"net"
	"time"
)

// Reader is the read surface the SunSpec decoder and the register map depend on.
type Reader interface {
	ReadHolding(addr, qty int) ([]uint16, error)
}

// Client is a Modbus connection to one unit id over a chosen transport.
type Client struct {
	tr      transport
	unit    byte
	timeout time.Duration
}

// Dial opens a Modbus TCP (MBAP) connection to addr for the given unit id. Kept as the historical
// name/behaviour so existing callers are unchanged; DialRTUTCP and DialSerial are the alternatives.
func Dial(addr string, unit byte, timeout time.Duration) (*Client, error) {
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return nil, err
	}
	return &Client{tr: &mbapTransport{conn: conn}, unit: unit, timeout: timeout}, nil
}

// DialRTUTCP opens a Modbus RTU-over-TCP connection (raw RTU frames + CRC over a socket, no MBAP) to
// a "transparent" serial gateway. Many cheap RS485→TCP gateways speak only this, not real Modbus TCP.
func DialRTUTCP(addr string, unit byte, timeout time.Duration) (*Client, error) {
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return nil, err
	}
	return &Client{tr: &rtuTransport{rw: conn, setDeadline: conn.SetDeadline}, unit: unit, timeout: timeout}, nil
}

func (c *Client) Close() error { return c.tr.Close() }

// request sends one PDU and returns the response PDU, surfacing a Modbus exception as an error.
func (c *Client) request(pdu []byte) ([]byte, error) {
	return c.tr.txn(c.unit, pdu, c.timeout)
}

func (c *Client) readRegs(fn byte, addr, qty int) ([]uint16, error) {
	if qty < 1 || qty > 125 {
		return nil, fmt.Errorf("modbus: bad quantity %d", qty)
	}
	pdu := []byte{fn, byte(addr >> 8), byte(addr), byte(qty >> 8), byte(qty)}
	resp, err := c.request(pdu)
	if err != nil {
		return nil, err
	}
	if len(resp) < 2 {
		return nil, fmt.Errorf("modbus: short read response")
	}
	bc := int(resp[1])
	if bc != qty*2 || len(resp) < 2+bc {
		return nil, fmt.Errorf("modbus: read byte count mismatch (got %d want %d)", bc, qty*2)
	}
	out := make([]uint16, qty)
	for i := 0; i < qty; i++ {
		out[i] = binary.BigEndian.Uint16(resp[2+i*2:])
	}
	return out, nil
}

// ReadHolding reads qty holding registers (fn 3). Satisfies Reader and sunspec.Reader.
func (c *Client) ReadHolding(addr, qty int) ([]uint16, error) { return c.readRegs(0x03, addr, qty) }

// ReadInput reads qty input registers (fn 4).
func (c *Client) ReadInput(addr, qty int) ([]uint16, error) { return c.readRegs(0x04, addr, qty) }

// WriteSingle writes one holding register (fn 6).
func (c *Client) WriteSingle(addr int, v uint16) error {
	pdu := []byte{0x06, byte(addr >> 8), byte(addr), byte(v >> 8), byte(v)}
	_, err := c.request(pdu)
	return err
}

// WriteMultiple writes a run of holding registers (fn 16).
func (c *Client) WriteMultiple(addr int, vals []uint16) error {
	if len(vals) < 1 || len(vals) > 123 {
		return fmt.Errorf("modbus: bad write quantity %d", len(vals))
	}
	pdu := make([]byte, 6+len(vals)*2)
	pdu[0] = 0x10
	binary.BigEndian.PutUint16(pdu[1:], uint16(addr))
	binary.BigEndian.PutUint16(pdu[3:], uint16(len(vals)))
	pdu[5] = byte(len(vals) * 2)
	for i, v := range vals {
		binary.BigEndian.PutUint16(pdu[6+i*2:], v)
	}
	_, err := c.request(pdu)
	return err
}
