package modbus

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"time"
)

// transport carries one Modbus request/response transaction for a unit. It hides the framing
// difference: MBAP over TCP (a 7-byte header, no CRC) versus RTU (a bare unit+PDU+CRC-16, used both
// over a serial line and over a "transparent" TCP gateway). Every read/write method on Client goes
// through txn, so the decoders (SunSpec walk, register map) work identically on any transport.
//
// txn takes and returns a bare PDU: the request PDU is [function, ...args]; the returned response
// PDU is [function, ...data] with the unit id and any framing/CRC stripped, or a non-nil error for a
// transport failure or a Modbus exception. This is the exact shape the old TCP-only client returned,
// so nothing downstream changed when transports were introduced.
type transport interface {
	txn(unit byte, pdu []byte, timeout time.Duration) ([]byte, error)
	Close() error
}

// --- MBAP over TCP (the original transport) --------------------------------------------------

type mbapTransport struct {
	conn  net.Conn
	txnID uint16
}

func (t *mbapTransport) txn(unit byte, pdu []byte, timeout time.Duration) ([]byte, error) {
	t.txnID++
	frame := make([]byte, 7+len(pdu))
	binary.BigEndian.PutUint16(frame[0:], t.txnID)
	binary.BigEndian.PutUint16(frame[2:], 0) // protocol id
	binary.BigEndian.PutUint16(frame[4:], uint16(1+len(pdu)))
	frame[6] = unit
	copy(frame[7:], pdu)

	if err := t.conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		return nil, err
	}
	if _, err := t.conn.Write(frame); err != nil {
		return nil, err
	}
	head := make([]byte, 6)
	if _, err := io.ReadFull(t.conn, head); err != nil {
		return nil, err
	}
	length := int(binary.BigEndian.Uint16(head[4:6]))
	if length < 2 || length > 260 {
		return nil, fmt.Errorf("modbus: bad frame length %d", length)
	}
	rest := make([]byte, length) // unit id + PDU
	if _, err := io.ReadFull(t.conn, rest); err != nil {
		return nil, err
	}
	resp := rest[1:] // drop the echoed unit id
	if len(resp) >= 2 && resp[0]&0x80 != 0 {
		return nil, fmt.Errorf("modbus: exception fn=%#x code=%d", resp[0]&0x7f, resp[1])
	}
	return resp, nil
}

func (t *mbapTransport) Close() error { return t.conn.Close() }

// --- RTU (serial line, or a transparent TCP gateway) ------------------------------------------

// rtuTransport speaks Modbus RTU over any byte stream: a serial port or a raw TCP socket. RTU has no
// length header, so the response size is inferred from the request's function code — read replies
// carry a byte-count byte, write echoes are fixed-width, exceptions are three bytes. setDeadline is
// the stream's whole-transaction deadline where it has one (net.Conn); a serial stream instead has a
// per-read timeout applied at open, so setDeadline is nil there.
type rtuTransport struct {
	rw          io.ReadWriteCloser
	setDeadline func(time.Time) error
}

func (t *rtuTransport) txn(unit byte, pdu []byte, timeout time.Duration) ([]byte, error) {
	frame := appendCRC(append([]byte{unit}, pdu...))
	if t.setDeadline != nil {
		if err := t.setDeadline(time.Now().Add(timeout)); err != nil {
			return nil, err
		}
	}
	if _, err := t.rw.Write(frame); err != nil {
		return nil, err
	}

	// Every response begins [unit, function].
	hdr := make([]byte, 2)
	if _, err := io.ReadFull(t.rw, hdr); err != nil {
		return nil, err
	}
	fn := hdr[1]

	if fn&0x80 != 0 {
		// Exception: [excCode][crc-lo][crc-hi].
		tail := make([]byte, 3)
		if _, err := io.ReadFull(t.rw, tail); err != nil {
			return nil, err
		}
		if !crcOK(append(hdr, tail...)) {
			return nil, fmt.Errorf("modbus rtu: bad CRC on exception frame")
		}
		return nil, fmt.Errorf("modbus: exception fn=%#x code=%d", fn&0x7f, tail[0])
	}

	switch pdu[0] {
	case 0x03, 0x04: // read holding/input: [byteCount][data...][crc]
		bc := make([]byte, 1)
		if _, err := io.ReadFull(t.rw, bc); err != nil {
			return nil, err
		}
		tail := make([]byte, int(bc[0])+2) // data + crc
		if _, err := io.ReadFull(t.rw, tail); err != nil {
			return nil, err
		}
		full := append(append(hdr, bc...), tail...)
		if !crcOK(full) {
			return nil, fmt.Errorf("modbus rtu: bad CRC on read response")
		}
		return full[1 : len(full)-2], nil // strip unit + crc -> [fn, byteCount, data...]
	case 0x06, 0x10: // write single/multiple: echo [addr-hi,addr-lo,val/qty-hi,val/qty-lo][crc]
		tail := make([]byte, 4+2)
		if _, err := io.ReadFull(t.rw, tail); err != nil {
			return nil, err
		}
		full := append(hdr, tail...)
		if !crcOK(full) {
			return nil, fmt.Errorf("modbus rtu: bad CRC on write response")
		}
		return full[1 : len(full)-2], nil
	default:
		return nil, fmt.Errorf("modbus rtu: unsupported function %#x", pdu[0])
	}
}

func (t *rtuTransport) Close() error { return t.rw.Close() }
