package discover

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/mysayasan/kopiv2/infra/iot/modbus"
	"github.com/mysayasan/kopiv2/infra/iot/sunspec"
)

// ScanModbus sweeps hosts for a Modbus device. It is deliberately gentle: it first TCP-probes the
// port (a cheap connect) and only sweeps unit ids on hosts that actually answer, so a subnet of
// silence costs one failed connect per host, not a full unit walk. For each responding host it tries
// SunSpec auto-discovery on each unit (the safest possible identification — a couple of register
// reads, no writes); a SunSpec hit yields a fully-identified candidate (vendor/model/serial), and a
// port that answers but is not SunSpec yields an "unidentified Modbus" candidate for the admin to
// assign a vendor profile to. transport is "tcp" (Modbus TCP) or "rtutcp" (RTU over a gateway).
func ScanModbus(ctx context.Context, hosts []string, port int, transport string, units []int, perOp time.Duration, concurrency int) []Found {
	if port <= 0 {
		port = 502
	}
	if perOp <= 0 {
		perOp = 800 * time.Millisecond
	}
	if concurrency <= 0 {
		concurrency = 32
	}
	if len(units) == 0 {
		units = []int{1}
	}
	sem := make(chan struct{}, concurrency)
	var mu sync.Mutex
	var out []Found
	var wg sync.WaitGroup

	for _, h := range hosts {
		if ctx.Err() != nil {
			break
		}
		h := h
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			addr := fmt.Sprintf("%s:%d", h, port)
			// Gentle: skip a full unit walk unless the port is even open.
			conn, err := net.DialTimeout("tcp", addr, perOp)
			if err != nil {
				return
			}
			_ = conn.Close()

			var hits []Found
			for _, u := range units {
				if ctx.Err() != nil {
					return
				}
				if f, ok := probeModbusUnit(addr, transport, byte(u), perOp); ok {
					hits = append(hits, f)
				}
			}
			if len(hits) == 0 {
				// Something answers on :502 but is not SunSpec — surface it as unidentified so the
				// admin can attach a vendor register-map profile (Huawei/Sungrow/Deye/…).
				hits = append(hits, Found{
					Source: "modbus", Address: addr, Endpoint: addr, Unit: units[0],
					Transport: normTransport(transport),
					Name:      "Modbus device (unidentified)",
					Detail:    "Answers on Modbus but is not SunSpec — assign a vendor register-map profile.",
				})
			}
			mu.Lock()
			out = append(out, hits...)
			mu.Unlock()
		}()
	}
	wg.Wait()
	return out
}

func probeModbusUnit(addr, transport string, unit byte, timeout time.Duration) (Found, bool) {
	var c *modbus.Client
	var err error
	switch normTransport(transport) {
	case "rtutcp":
		c, err = modbus.DialRTUTCP(addr, unit, timeout)
	default:
		c, err = modbus.Dial(addr, unit, timeout)
	}
	if err != nil {
		return Found{}, false
	}
	defer c.Close()

	base, blocks, err := sunspec.Discover(c)
	if err != nil {
		return Found{}, false
	}
	f := Found{
		Source: "modbus", Address: addr, Endpoint: addr, Unit: int(unit),
		Transport: normTransport(transport),
		Detail:    fmt.Sprintf("SunSpec device (base %d, unit %d)", base, unit),
	}
	for _, b := range blocks {
		if b.ID == 1 {
			cm := sunspec.DecodeCommon(b)
			f.Vendor = strings.TrimSpace(cm.Mfg)
			f.Model = strings.TrimSpace(cm.Model)
			f.Name = strings.TrimSpace(strings.TrimSpace(cm.Mfg) + " " + strings.TrimSpace(cm.Model))
			if s := strings.TrimSpace(cm.Serial); s != "" {
				f.Detail += ", serial " + s
			}
		}
	}
	if f.Name == "" {
		f.Name = fmt.Sprintf("SunSpec unit %d", unit)
	}
	return f, true
}

func normTransport(t string) string {
	if strings.EqualFold(strings.TrimSpace(t), "rtutcp") {
		return "rtutcp"
	}
	return "tcp"
}
