package discover

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"strings"
	"time"
)

// ScanEtherNetIP finds EtherNet/IP devices (Allen-Bradley/Rockwell and other CIP PLCs, drives, I/O)
// by broadcasting a ListIdentity request on UDP 44818 and parsing the Identity replies. Read-only:
// ListIdentity is the CIP-standard "who are you" probe, it changes nothing on the device.
func ScanEtherNetIP(ctx context.Context, timeout time.Duration) []Found {
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		return nil
	}
	defer conn.Close()

	dst := &net.UDPAddr{IP: net.IPv4bcast, Port: 44818}
	if _, err := conn.WriteToUDP(enipListIdentityRequest(), dst); err != nil {
		return nil
	}

	_ = conn.SetReadDeadline(time.Now().Add(timeout))
	seen := map[string]Found{}
	buf := make([]byte, 1024)
	for {
		if ctx.Err() != nil {
			break
		}
		n, src, err := conn.ReadFromUDP(buf)
		if err != nil {
			break
		}
		vendorID, product, ok := parseENIPIdentity(buf[:n])
		if !ok {
			continue
		}
		ip := src.IP.String()
		if _, dup := seen[ip]; dup {
			continue
		}
		vendor := enipVendor(vendorID)
		name := strings.TrimSpace(product)
		if name == "" {
			name = "EtherNet/IP device"
		}
		seen[ip] = Found{
			Source: "ethernetip", Address: ip, Name: name, Vendor: vendor, Model: strings.TrimSpace(product),
			Detail: strings.TrimSpace(fmt.Sprintf("EtherNet/IP identity — vendor %s", vendor)),
		}
	}

	out := make([]Found, 0, len(seen))
	for _, f := range seen {
		out = append(out, f)
	}
	return out
}

// enipListIdentityRequest builds the 24-byte encapsulation header for a ListIdentity (command
// 0x0063) with an empty body — everything else zero.
func enipListIdentityRequest() []byte {
	b := make([]byte, 24)
	binary.LittleEndian.PutUint16(b[0:], 0x0063)
	return b
}

// parseENIPIdentity extracts the vendor id and product name from a ListIdentity reply. The reply is
// the 24-byte encapsulation header, then a CPF list; the Identity item (type 0x000C) holds the
// fields at fixed offsets (protocol version, socket address, vendor id, …, product name).
func parseENIPIdentity(resp []byte) (vendorID uint16, product string, ok bool) {
	if len(resp) < 26 || binary.LittleEndian.Uint16(resp[0:]) != 0x0063 {
		return 0, "", false
	}
	p := 24
	itemCount := binary.LittleEndian.Uint16(resp[p:])
	p += 2
	for i := 0; i < int(itemCount) && p+4 <= len(resp); i++ {
		itype := binary.LittleEndian.Uint16(resp[p:])
		ilen := int(binary.LittleEndian.Uint16(resp[p+2:]))
		p += 4
		if p+ilen > len(resp) {
			break
		}
		if itype == 0x000C {
			d := resp[p : p+ilen]
			// proto(2) socket(16) vendorID(2) devType(2) prodCode(2) rev(2) status(2) serial(4) nameLen(1) name(n) state(1)
			if len(d) >= 20 {
				vendorID = binary.LittleEndian.Uint16(d[18:])
			}
			const nameLenOff = 32
			if len(d) > nameLenOff {
				nlen := int(d[nameLenOff])
				if nameLenOff+1+nlen <= len(d) {
					product = string(d[nameLenOff+1 : nameLenOff+1+nlen])
				}
			}
			return vendorID, product, true
		}
		p += ilen
	}
	return 0, "", false
}

// enipVendor names a few of the most common CIP vendor ids; the rest are shown numerically.
func enipVendor(id uint16) string {
	switch id {
	case 1:
		return "Rockwell Automation / Allen-Bradley"
	case 5:
		return "Schneider Electric"
	case 26:
		return "Festo"
	case 108:
		return "Omron"
	case 283:
		return "HMS / Anybus"
	case 0:
		return ""
	default:
		return fmt.Sprintf("vendor %d", id)
	}
}
