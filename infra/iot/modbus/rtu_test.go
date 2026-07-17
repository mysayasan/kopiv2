package modbus

import (
	"bytes"
	"testing"
	"time"
)

// TestCRC16CheckValue pins crc16 against the canonical CRC-16/MODBUS check value: the CRC of the
// ASCII string "123456789" is 0x4B37. If this drifts, every RTU frame we send is malformed.
func TestCRC16CheckValue(t *testing.T) {
	if got := crc16([]byte("123456789")); got != 0x4B37 {
		t.Fatalf("crc16(\"123456789\") = %#04x, want 0x4b37", got)
	}
	if !crcOK(appendCRC([]byte{0x01, 0x03, 0x02, 0x00, 0x2A})) {
		t.Error("crcOK rejected a frame we just CRC'd")
	}
	bad := appendCRC([]byte{0x01, 0x03, 0x02, 0x00, 0x2A})
	bad[2] ^= 0xFF // corrupt a payload byte
	if crcOK(bad) {
		t.Error("crcOK accepted a corrupted frame")
	}
}

// fakeRTU is an in-memory RTU peer: each Write (a request frame) enqueues the correct RTU response,
// which the subsequent Reads drain. It proves the rtuTransport frames requests and parses responses
// the same way a real serial device / gateway does.
type fakeRTU struct {
	bank    map[int]uint16
	buf     bytes.Buffer
	lastReq []byte
	exc     bool // if set, answer every request with a Modbus exception
}

func (f *fakeRTU) Write(p []byte) (int, error) {
	f.lastReq = append([]byte(nil), p...)
	unit, fn := p[0], p[1]
	if f.exc {
		f.buf.Write(appendCRC([]byte{unit, fn | 0x80, 0x02}))
		return len(p), nil
	}
	switch fn {
	case 0x03, 0x04: // read: echo a byte-count + data block
		addr := int(p[2])<<8 | int(p[3])
		qty := int(p[4])<<8 | int(p[5])
		body := []byte{unit, fn, byte(qty * 2)}
		for i := 0; i < qty; i++ {
			v := f.bank[addr+i]
			body = append(body, byte(v>>8), byte(v))
		}
		f.buf.Write(appendCRC(body))
	case 0x06: // write single: echo the request back (addr+value)
		f.buf.Write(append([]byte(nil), p...))
		addr := int(p[2])<<8 | int(p[3])
		f.bank[addr] = uint16(p[4])<<8 | uint16(p[5])
	}
	return len(p), nil
}

func (f *fakeRTU) Read(p []byte) (int, error) { return f.buf.Read(p) }
func (f *fakeRTU) Close() error               { return nil }

func rtuClient(f *fakeRTU) *Client {
	return &Client{tr: &rtuTransport{rw: f}, unit: 1, timeout: time.Second}
}

// TestRTUReadWriteRoundTrip drives a full read and a full write through the RTU framing.
func TestRTUReadWriteRoundTrip(t *testing.T) {
	f := &fakeRTU{bank: map[int]uint16{10: 0x1234, 11: 0x00FF}}
	c := rtuClient(f)

	regs, err := c.ReadHolding(10, 2)
	if err != nil {
		t.Fatalf("ReadHolding: %v", err)
	}
	if regs[0] != 0x1234 || regs[1] != 0x00FF {
		t.Errorf("ReadHolding = %#v, want [0x1234 0x00ff]", regs)
	}
	// The request we sent must be a well-formed RTU frame (unit, fn3, addr, qty, valid CRC).
	if !crcOK(f.lastReq) || f.lastReq[0] != 1 || f.lastReq[1] != 0x03 {
		t.Errorf("read request frame malformed: %#v", f.lastReq)
	}

	// Input registers use the same framing on a different function code.
	if _, err := c.ReadInput(10, 1); err != nil || f.lastReq[1] != 0x04 {
		t.Errorf("ReadInput fn/err = %#v / %v", f.lastReq, err)
	}

	if err := c.WriteSingle(20, 0xBEEF); err != nil {
		t.Fatalf("WriteSingle: %v", err)
	}
	if f.bank[20] != 0xBEEF {
		t.Errorf("write did not land: bank[20] = %#04x", f.bank[20])
	}
}

// TestRTUExceptionSurfaces proves a Modbus exception reply becomes a Go error, not a bad decode.
func TestRTUExceptionSurfaces(t *testing.T) {
	f := &fakeRTU{bank: map[int]uint16{}, exc: true}
	c := rtuClient(f)
	if _, err := c.ReadHolding(0, 1); err == nil {
		t.Fatal("expected an error from an exception response")
	}
}
