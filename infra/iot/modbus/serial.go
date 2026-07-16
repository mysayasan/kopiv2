package modbus

import (
	"os"
	"sync"
	"time"

	"go.bug.st/serial"
)

// SerialParams are the line settings for a Modbus RTU serial connection. The near-universal default
// for solar/meter RS485 is 9600 8N1; zero values fill in to exactly that.
type SerialParams struct {
	Baud     int    // e.g. 9600, 19200
	DataBits int    // usually 8
	Parity   string // "N" (none, default), "E" (even), "O" (odd)
	StopBits int    // 1 (default) or 2
}

func (s SerialParams) mode() *serial.Mode {
	baud := s.Baud
	if baud <= 0 {
		baud = 9600
	}
	data := s.DataBits
	if data <= 0 {
		data = 8
	}
	parity := serial.NoParity
	switch s.Parity {
	case "E", "e":
		parity = serial.EvenParity
	case "O", "o":
		parity = serial.OddParity
	}
	stop := serial.OneStopBit
	if s.StopBits == 2 {
		stop = serial.TwoStopBits
	}
	return &serial.Mode{BaudRate: baud, DataBits: data, Parity: parity, StopBits: stop}
}

// A serial port is an EXCLUSIVE resource, but an RS485 bus is multi-drop: several devices (unit ids)
// share one physical port and must be polled one at a time. Each device gets its own poller
// goroutine, so a per-port lock serialises access — a device holds the port for the duration of one
// poll (open → reads → close) and the others wait their turn.
var (
	portLocksMu sync.Mutex
	portLocks   = map[string]*sync.Mutex{}
)

func portLock(name string) *sync.Mutex {
	portLocksMu.Lock()
	defer portLocksMu.Unlock()
	m := portLocks[name]
	if m == nil {
		m = &sync.Mutex{}
		portLocks[name] = m
	}
	return m
}

// serialConn adapts a serial.Port to io.ReadWriteCloser for the RTU transport, and carries the
// per-port lock so closing the connection releases the bus for the next device.
type serialConn struct {
	p      serial.Port
	unlock func()
	once   sync.Once
}

func (s *serialConn) Read(b []byte) (int, error) {
	n, err := s.p.Read(b)
	// go.bug.st/serial signals a read timeout as (0, nil); turn it into an error so io.ReadFull
	// stops rather than spinning, and the poll fails cleanly (retried next tick).
	if n == 0 && err == nil {
		return 0, os.ErrDeadlineExceeded
	}
	return n, err
}

func (s *serialConn) Write(b []byte) (int, error) { return s.p.Write(b) }

func (s *serialConn) Close() error {
	err := s.p.Close()
	s.once.Do(s.unlock)
	return err
}

// DialSerial opens a Modbus RTU connection over a serial port (e.g. "COM3", "/dev/ttyUSB0") for one
// unit id. It acquires the per-port lock for the lifetime of the returned Client, so the caller must
// Close it (the poller does, every tick) to release the bus.
func DialSerial(portName string, unit byte, timeout time.Duration, sp SerialParams) (*Client, error) {
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	lock := portLock(portName)
	lock.Lock()
	p, err := serial.Open(portName, sp.mode())
	if err != nil {
		lock.Unlock()
		return nil, err
	}
	if err := p.SetReadTimeout(timeout); err != nil {
		_ = p.Close()
		lock.Unlock()
		return nil, err
	}
	tr := &rtuTransport{rw: &serialConn{p: p, unlock: lock.Unlock}}
	return &Client{tr: tr, unit: unit, timeout: timeout}, nil
}
