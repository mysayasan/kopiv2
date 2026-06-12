package discovery

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"strings"
	"time"
)

const mdnsPort = 5353

// Camera-related mDNS service types to query.
var mdnsServiceTypes = []string{
	"_axis_video._tcp.local.",
	"_rtsp._tcp.local.",
	"_http._tcp.local.",
	"_camera._tcp.local.",
	"_onvif._tcp.local.",
}

const (
	dnsTypeA   uint16 = 1
	dnsTypePTR uint16 = 12
	dnsTypeTXT uint16 = 16
	dnsTypeSRV uint16 = 33
	dnsClassIN uint16 = 1
	dnsQUBit   uint16 = 0x8000 // unicast-response request bit
)

type mdnsEntry struct {
	ip       string
	hostname string
	port     int
	name     string
	txt      map[string]string
}

// DiscoverMDNS sends PTR queries to the mDNS multicast group and collects
// device announcements. Devices that respect the QU bit respond via unicast.
func DiscoverMDNS(ctx context.Context, timeout time.Duration) ([]Device, error) {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}

	conn, joined := openMDNSConn()
	if conn == nil {
		return nil, fmt.Errorf("mdns: could not open any UDP listener")
	}
	defer conn.Close()

	deadline := time.Now().Add(timeout)
	if dl, ok := ctx.Deadline(); ok && dl.Before(deadline) {
		deadline = dl
	}
	conn.SetDeadline(deadline)

	// Send PTR queries for each service type.
	mcastAddr := &net.UDPAddr{IP: net.ParseIP("224.0.0.251"), Port: mdnsPort}
	for _, svcType := range mdnsServiceTypes {
		pkt := buildMDNSQuery(svcType, !joined)
		conn.WriteToUDP(pkt, mcastAddr) //nolint:errcheck
	}

	// Collect responses.
	entries := make(map[string]*mdnsEntry) // instance name → entry
	addrByName := make(map[string]string)  // hostname → IP

	buf := make([]byte, 9000)
	for {
		select {
		case <-ctx.Done():
			goto done
		default:
		}
		n, src, err := conn.ReadFromUDP(buf)
		if err != nil {
			break
		}
		parseMDNSPacket(buf[:n], src.IP.String(), entries, addrByName)
	}

done:
	var devices []Device
	for _, entry := range entries {
		ip := firstNonEmpty(addrByName[entry.hostname], addrByName[entry.hostname+"."],
			addrByName[entry.name], addrByName[entry.name+"."], entry.ip)
		if ip == "" {
			continue
		}
		dev := Device{
			IP:       ip,
			Hostname: firstNonEmpty(entry.hostname, entry.name),
			Methods:  []string{MethodMDNS},
			Metadata: make(map[string]string),
		}
		if entry.port > 0 {
			dev.HTTPPort = entry.port
		}
		// Map well-known Axis TXT record keys.
		if model, ok := entry.txt["model"]; ok {
			dev.Model = model
			dev.Manufacturer = "Axis"
		}
		if fw, ok := entry.txt["fwversion"]; ok {
			dev.FirmwareVersion = fw
		}
		if serial, ok := entry.txt["serial"]; ok {
			dev.Serial = serial
		}
		if mac, ok := entry.txt["macaddress"]; ok {
			dev.Metadata["mac"] = mac
		}
		if entry.name != "" {
			dev.Metadata["instanceName"] = entry.name
		}
		devices = append(devices, dev)
	}
	return devices, nil
}

// openMDNSConn tries to bind to port 5353 via multicast first.
// Falls back to a random port if 5353 is in use (e.g. system mDNS daemon running).
func openMDNSConn() (*net.UDPConn, bool) {
	mcastIP := net.ParseIP("224.0.0.251")
	ifaces, _ := net.Interfaces()
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagMulticast == 0 {
			continue
		}
		conn, err := net.ListenMulticastUDP("udp4", &iface, &net.UDPAddr{
			IP:   mcastIP,
			Port: mdnsPort,
		})
		if err == nil {
			return conn, true
		}
	}
	// Fallback: random port, QU-bit queries only.
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{})
	if err != nil {
		return nil, false
	}
	return conn, false
}

// buildMDNSQuery returns a wire-format DNS PTR query for the given service name.
// If qu is true, the QU bit is set in QCLASS to request a unicast response.
func buildMDNSQuery(service string, qu bool) []byte {
	qname := encodeDNSName(service)
	class := dnsClassIN
	if qu {
		class |= dnsQUBit
	}
	msg := make([]byte, 12+len(qname)+4)
	// Header: ID=0, flags=0, QDCOUNT=1.
	binary.BigEndian.PutUint16(msg[4:], 1)
	copy(msg[12:], qname)
	off := 12 + len(qname)
	binary.BigEndian.PutUint16(msg[off:], uint16(dnsTypePTR))
	binary.BigEndian.PutUint16(msg[off+2:], class)
	return msg
}

// encodeDNSName converts a dotted domain name to DNS label-length wire format.
func encodeDNSName(name string) []byte {
	name = strings.TrimSuffix(name, ".")
	var buf []byte
	for _, label := range strings.Split(name, ".") {
		buf = append(buf, byte(len(label)))
		buf = append(buf, []byte(label)...)
	}
	return append(buf, 0)
}

// parseDNSName decodes a DNS name starting at offset, following compression pointers.
// Returns the name string, the offset after the name, and whether parsing succeeded.
func parseDNSName(data []byte, offset int) (string, int, bool) {
	var parts []string
	visited := make(map[int]bool)
	nextOffset := -1
	for offset < len(data) {
		if visited[offset] {
			return "", 0, false
		}
		visited[offset] = true
		b := data[offset]
		if b == 0 {
			offset++
			break
		}
		if b&0xC0 == 0xC0 {
			if offset+1 >= len(data) {
				return "", 0, false
			}
			ptr := int(binary.BigEndian.Uint16(data[offset:offset+2]) & 0x3FFF)
			if nextOffset < 0 {
				nextOffset = offset + 2
			}
			offset = ptr
			continue
		}
		if b&0xC0 != 0 {
			return "", 0, false
		}
		offset++
		if offset+int(b) > len(data) {
			return "", 0, false
		}
		parts = append(parts, string(data[offset:offset+int(b)]))
		offset += int(b)
	}
	if nextOffset < 0 {
		nextOffset = offset
	}
	return strings.Join(parts, "."), nextOffset, true
}

// parseMDNSPacket parses a raw mDNS UDP payload and populates the shared maps.
func parseMDNSPacket(data []byte, srcIP string, entries map[string]*mdnsEntry, addrByName map[string]string) {
	if len(data) < 12 {
		return
	}
	qdCount := int(binary.BigEndian.Uint16(data[4:6]))
	anCount := int(binary.BigEndian.Uint16(data[6:8]))
	nsCount := int(binary.BigEndian.Uint16(data[8:10]))
	arCount := int(binary.BigEndian.Uint16(data[10:12]))

	offset := 12

	// Skip question section.
	for i := 0; i < qdCount; i++ {
		_, next, ok := parseDNSName(data, offset)
		if !ok {
			return
		}
		next += 4 // QTYPE(2) + QCLASS(2)
		if next > len(data) {
			return
		}
		offset = next
	}

	// Parse all resource records (answers + authority + additional).
	total := anCount + nsCount + arCount
	for i := 0; i < total; i++ {
		if offset+1 >= len(data) {
			return
		}
		name, next, ok := parseDNSName(data, offset)
		if !ok {
			return
		}
		offset = next
		if offset+10 > len(data) {
			return
		}
		rtype := binary.BigEndian.Uint16(data[offset:])
		rdlen := int(binary.BigEndian.Uint16(data[offset+8:]))
		offset += 10
		rdStart := offset
		if offset+rdlen > len(data) {
			return
		}
		rdata := data[offset : offset+rdlen]
		offset += rdlen

		switch rtype {
		case dnsTypeA:
			if rdlen == 4 {
				ip := net.IP(rdata).String()
				addrByName[name] = ip
				addrByName[name+"."] = ip
			}

		case dnsTypePTR:
			ptr, _, ok := parseDNSName(data, rdStart)
			if !ok {
				continue
			}
			if _, exists := entries[ptr]; !exists {
				entries[ptr] = &mdnsEntry{name: ptr, txt: make(map[string]string)}
			}
			if entries[ptr].ip == "" {
				entries[ptr].ip = srcIP
			}

		case dnsTypeSRV:
			if rdlen < 7 {
				continue
			}
			port := int(binary.BigEndian.Uint16(rdata[4:6]))
			host, _, ok := parseDNSName(data, rdStart+6)
			if !ok {
				continue
			}
			if _, exists := entries[name]; !exists {
				entries[name] = &mdnsEntry{name: name, txt: make(map[string]string)}
			}
			entries[name].port = port
			entries[name].hostname = host
			if entries[name].ip == "" {
				entries[name].ip = srcIP
			}

		case dnsTypeTXT:
			if _, exists := entries[name]; !exists {
				entries[name] = &mdnsEntry{name: name, txt: make(map[string]string)}
			}
			pos := 0
			for pos < len(rdata) {
				l := int(rdata[pos])
				pos++
				if pos+l > len(rdata) {
					break
				}
				kv := string(rdata[pos : pos+l])
				pos += l
				if eq := strings.IndexByte(kv, '='); eq >= 0 {
					k := strings.ToLower(kv[:eq])
					entries[name].txt[k] = kv[eq+1:]
				}
			}
			if entries[name].ip == "" {
				entries[name].ip = srcIP
			}
		}
	}
}
