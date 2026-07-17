package discover

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"time"
)

// ScanBACnet finds BACnet/IP devices (building-automation controllers, VAV boxes, meters) by
// broadcasting a Who-Is on UDP 47808 and parsing the I-Am replies. Read-only: Who-Is is the
// BACnet-standard discovery broadcast.
func ScanBACnet(ctx context.Context, timeout time.Duration) []Found {
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		return nil
	}
	defer conn.Close()

	dst := &net.UDPAddr{IP: net.IPv4bcast, Port: 47808}
	if _, err := conn.WriteToUDP(bacnetWhoIs(), dst); err != nil {
		return nil
	}

	_ = conn.SetReadDeadline(time.Now().Add(timeout))
	seen := map[string]Found{}
	buf := make([]byte, 1500)
	for {
		if ctx.Err() != nil {
			break
		}
		n, src, err := conn.ReadFromUDP(buf)
		if err != nil {
			break
		}
		instance, vendorID, ok := parseBACnetIAm(buf[:n])
		if !ok {
			continue
		}
		ip := src.IP.String()
		if _, dup := seen[ip]; dup {
			continue
		}
		seen[ip] = Found{
			Source: "bacnet", Address: ip,
			Name:   fmt.Sprintf("BACnet device %d", instance),
			Detail: fmt.Sprintf("BACnet I-Am — device instance %d, vendor id %d", instance, vendorID),
		}
	}

	out := make([]Found, 0, len(seen))
	for _, f := range seen {
		out = append(out, f)
	}
	return out
}

// bacnetWhoIs builds an unconstrained Who-Is: BVLC (Original-Broadcast) + NPDU + APDU (unconfirmed
// request, service Who-Is).
func bacnetWhoIs() []byte {
	return []byte{
		0x81, 0x0b, 0x00, 0x08, // BVLC: type 0x81, func 0x0b (orig-broadcast), length 8
		0x01, 0x00, // NPDU: version 1, control 0
		0x10, 0x08, // APDU: unconfirmed-request, service Who-Is
	}
}

// parseBACnetIAm pulls the device instance and vendor id out of an I-Am. It locates the APDU
// (unconfirmed-request 0x10 + service I-Am 0x00) then walks the application-tagged primitives: the
// object identifier (tag 12) carries the instance in its low 22 bits, and the vendor id is the last
// unsigned (tag 2) in the fixed I-Am sequence.
func parseBACnetIAm(pkt []byte) (instance uint32, vendorID uint32, ok bool) {
	p := apduStart(pkt)
	if p < 0 || p+2 > len(pkt) || pkt[p] != 0x10 || pkt[p+1] != 0x00 {
		// Fall back to a scan for the I-Am marker if the NPDU had optional fields.
		p = indexOf(pkt, []byte{0x10, 0x00})
		if p < 0 {
			return 0, 0, false
		}
	}
	p += 2

	type prim struct {
		tag byte
		val []byte
	}
	var prims []prim
	for p < len(pkt) {
		t := pkt[p]
		p++
		if t&0x08 != 0 { // a context tag — I-Am uses application tags only, so stop
			break
		}
		tagNum := t >> 4
		l := int(t & 0x07)
		if l == 5 { // extended length
			if p >= len(pkt) {
				break
			}
			l = int(pkt[p])
			p++
		}
		if p+l > len(pkt) {
			break
		}
		prims = append(prims, prim{tagNum, pkt[p : p+l]})
		p += l
	}

	for _, pr := range prims {
		if pr.tag == 12 && len(pr.val) == 4 {
			instance = binary.BigEndian.Uint32(pr.val) & 0x3FFFFF
			ok = true
			break
		}
	}
	for _, pr := range prims { // vendor id = the last unsigned (tag 2)
		if pr.tag == 2 {
			vendorID = beUint(pr.val)
		}
	}
	return instance, vendorID, ok
}

// apduStart returns the offset of the APDU: after the 4-byte BVLC and a plain 2-byte NPDU.
func apduStart(pkt []byte) int {
	if len(pkt) < 6 || pkt[0] != 0x81 {
		return -1
	}
	return 6
}

func indexOf(hay, needle []byte) int {
	for i := 0; i+len(needle) <= len(hay); i++ {
		match := true
		for j := range needle {
			if hay[i+j] != needle[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

func beUint(b []byte) uint32 {
	var v uint32
	for _, x := range b {
		v = v<<8 | uint32(x)
	}
	return v
}
