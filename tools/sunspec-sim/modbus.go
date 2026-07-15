package main

// A minimal Modbus TCP server — just enough to serve a SunSpec register bank to a client and
// accept control writes. Implemented on the standard library alone (no Modbus dependency): the
// TCP framing is a 7-byte MBAP header plus a short PDU, and we support exactly the four function
// codes a SunSpec client uses.
//
//	MBAP:  [txn:2][proto:2=0][len:2][unit:1]      len counts unit + PDU
//	PDU :  [func:1][...]
//
// Supported: fn 3 (read holding), fn 4 (read input, served from the same bank), fn 6 (write single
// register), fn 16 (write multiple registers). Anything else returns an illegal-function exception.

import (
	"encoding/binary"
	"errors"
	"io"
	"log"
	"net"
)

const (
	fnReadHolding  = 0x03
	fnReadInput    = 0x04
	fnWriteSingle  = 0x06
	fnWriteMulti   = 0x10
	excIllegalFunc = 0x01
	excIllegalAddr = 0x02
	excIllegalVal  = 0x03
	excGatewayFail = 0x0B // no device at the requested unit id
)

// Server serves one or more devices over Modbus TCP, routed by unit id.
type Server struct {
	devices map[byte]Device
	verbose bool
	// onWrite is called after a successful register write, so the CLI can report control writes
	// (curtailment, battery mode) as they land — the whole reason a simulator supports writes.
	onWrite func(dev Device, addr int, value uint16)
}

func (s *Server) listenAndServe(addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	log.Printf("modbus tcp listening on %s", addr)
	for {
		conn, err := ln.Accept()
		if err != nil {
			return err
		}
		go s.serveConn(conn)
	}
}

func (s *Server) serveConn(conn net.Conn) {
	defer conn.Close()
	if s.verbose {
		log.Printf("client connected: %s", conn.RemoteAddr())
	}
	header := make([]byte, 7)
	for {
		// MBAP header. len (bytes 4-5) counts the unit id + the PDU.
		if _, err := io.ReadFull(conn, header); err != nil {
			if s.verbose && !errors.Is(err, io.EOF) {
				log.Printf("client %s read error: %v", conn.RemoteAddr(), err)
			}
			return
		}
		txn := binary.BigEndian.Uint16(header[0:2])
		length := int(binary.BigEndian.Uint16(header[4:6]))
		unit := header[6]
		if length < 2 || length > 260 {
			return // malformed frame
		}
		pdu := make([]byte, length-1) // -1: the unit id was the first byte counted by len
		if _, err := io.ReadFull(conn, pdu); err != nil {
			return
		}
		resp := s.handlePDU(unit, pdu)
		out := make([]byte, 7+len(resp))
		binary.BigEndian.PutUint16(out[0:2], txn)
		binary.BigEndian.PutUint16(out[2:4], 0) // protocol id
		binary.BigEndian.PutUint16(out[4:6], uint16(1+len(resp)))
		out[6] = unit
		copy(out[7:], resp)
		if _, err := conn.Write(out); err != nil {
			return
		}
	}
}

// handlePDU routes one request to the device at the given unit id and returns the response PDU
// (which may be an exception). A request to an absent unit gets a gateway-target-failed exception,
// exactly as a real Modbus gateway answers for a missing slave.
func (s *Server) handlePDU(unit byte, pdu []byte) []byte {
	if len(pdu) < 1 {
		return []byte{0x80, excIllegalFunc}
	}
	fn := pdu[0]
	dev, ok := s.devices[unit]
	if !ok {
		return exception(fn, excGatewayFail)
	}
	switch fn {
	case fnReadHolding, fnReadInput:
		return s.handleRead(dev, fn, pdu)
	case fnWriteSingle:
		return s.handleWriteSingle(dev, pdu)
	case fnWriteMulti:
		return s.handleWriteMulti(dev, pdu)
	default:
		return exception(fn, excIllegalFunc)
	}
}

func (s *Server) handleRead(dev Device, fn byte, pdu []byte) []byte {
	if len(pdu) < 5 {
		return exception(fn, excIllegalVal)
	}
	start := int(binary.BigEndian.Uint16(pdu[1:3]))
	qty := int(binary.BigEndian.Uint16(pdu[3:5]))
	if qty < 1 || qty > 125 {
		return exception(fn, excIllegalVal)
	}
	regs, ok := dev.bank().readRange(start, qty)
	if !ok {
		return exception(fn, excIllegalAddr)
	}
	if s.verbose {
		log.Printf("unit %d read fn=%d addr=%d qty=%d", dev.unit(), fn, start, qty)
	}
	resp := make([]byte, 2+qty*2)
	resp[0] = fn
	resp[1] = byte(qty * 2)
	for i, r := range regs {
		binary.BigEndian.PutUint16(resp[2+i*2:], r)
	}
	return resp
}

func (s *Server) handleWriteSingle(dev Device, pdu []byte) []byte {
	if len(pdu) < 5 {
		return exception(fnWriteSingle, excIllegalVal)
	}
	addr := int(binary.BigEndian.Uint16(pdu[1:3]))
	val := binary.BigEndian.Uint16(pdu[3:5])
	if !dev.bank().writeReg(addr, val) {
		return exception(fnWriteSingle, excIllegalAddr)
	}
	if s.onWrite != nil {
		s.onWrite(dev, addr, val)
	}
	// Response echoes the request.
	resp := make([]byte, 5)
	copy(resp, pdu[:5])
	return resp
}

func (s *Server) handleWriteMulti(dev Device, pdu []byte) []byte {
	if len(pdu) < 6 {
		return exception(fnWriteMulti, excIllegalVal)
	}
	addr := int(binary.BigEndian.Uint16(pdu[1:3]))
	qty := int(binary.BigEndian.Uint16(pdu[3:5]))
	byteCount := int(pdu[5])
	if qty < 1 || qty > 123 || byteCount != qty*2 || len(pdu) < 6+byteCount {
		return exception(fnWriteMulti, excIllegalVal)
	}
	for i := 0; i < qty; i++ {
		v := binary.BigEndian.Uint16(pdu[6+i*2:])
		if !dev.bank().writeReg(addr+i, v) {
			return exception(fnWriteMulti, excIllegalAddr)
		}
		if s.onWrite != nil {
			s.onWrite(dev, addr+i, v)
		}
	}
	resp := make([]byte, 5)
	copy(resp, pdu[:5]) // echo address + quantity
	return resp
}

func exception(fn, code byte) []byte {
	return []byte{fn | 0x80, code}
}
