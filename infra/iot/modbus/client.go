// Package modbus is a minimal Modbus TCP client plus the two ways myiotsan reads a device: SunSpec
// auto-discovery (infra/iot/sunspec) for compliant hardware, and a manual register map for the
// vendors that are not. Both normalise to codec.Sample, so everything downstream — deadband,
// storage, rules, dashboards — is identical regardless of how the bytes were fetched.
//
// The client is stdlib-only: Modbus TCP framing is a 7-byte MBAP header plus a short PDU, and we
// use exactly the function codes a SunSpec/solar device needs (read holding/input, write single/
// multiple registers). No external Modbus dependency is pulled into the appliance.
package modbus

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"time"
)

// Reader is the read surface the SunSpec decoder and the register map depend on.
type Reader interface {
	ReadHolding(addr, qty int) ([]uint16, error)
}

// Client is a Modbus TCP connection to one unit id.
type Client struct {
	conn    net.Conn
	unit    byte
	txn     uint16
	timeout time.Duration
}

// Dial opens a Modbus TCP connection to addr for the given unit id.
func Dial(addr string, unit byte, timeout time.Duration) (*Client, error) {
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return nil, err
	}
	return &Client{conn: conn, unit: unit, timeout: timeout}, nil
}

func (c *Client) Close() error { return c.conn.Close() }

// request sends one PDU and returns the response PDU, surfacing a Modbus exception as an error.
func (c *Client) request(pdu []byte) ([]byte, error) {
	c.txn++
	frame := make([]byte, 7+len(pdu))
	binary.BigEndian.PutUint16(frame[0:], c.txn)
	binary.BigEndian.PutUint16(frame[2:], 0) // protocol id
	binary.BigEndian.PutUint16(frame[4:], uint16(1+len(pdu)))
	frame[6] = c.unit
	copy(frame[7:], pdu)

	if err := c.conn.SetDeadline(time.Now().Add(c.timeout)); err != nil {
		return nil, err
	}
	if _, err := c.conn.Write(frame); err != nil {
		return nil, err
	}
	head := make([]byte, 6)
	if _, err := io.ReadFull(c.conn, head); err != nil {
		return nil, err
	}
	length := int(binary.BigEndian.Uint16(head[4:6]))
	if length < 2 || length > 260 {
		return nil, fmt.Errorf("modbus: bad frame length %d", length)
	}
	rest := make([]byte, length) // unit id + PDU
	if _, err := io.ReadFull(c.conn, rest); err != nil {
		return nil, err
	}
	resp := rest[1:] // drop the echoed unit id
	if len(resp) >= 2 && resp[0]&0x80 != 0 {
		return nil, fmt.Errorf("modbus: exception fn=%#x code=%d", resp[0]&0x7f, resp[1])
	}
	return resp, nil
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
