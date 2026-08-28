package discover

import (
	"encoding/binary"
	"testing"
)

func TestHostsExpandsAndCaps(t *testing.T) {
	hosts, err := Hosts([]string{"192.168.5.0/30", "10.0.0.9"}, 100)
	if err != nil {
		t.Fatalf("Hosts: %v", err)
	}
	// /30 = 4 addresses + the bare host.
	if len(hosts) != 5 {
		t.Fatalf("expected 5 hosts, got %d: %v", len(hosts), hosts)
	}
	if hosts[0] != "192.168.5.0" || hosts[3] != "192.168.5.3" || hosts[4] != "10.0.0.9" {
		t.Errorf("unexpected expansion: %v", hosts)
	}
	if _, err := Hosts([]string{"10.0.0.0/8"}, 1000); err == nil {
		t.Error("expected a range-too-large error for a /8 under a 1000 cap")
	}
}

// The LAN-local guard. A scan target is a string an admin types, and until this existed nothing
// checked it: a running appliance accepted a sweep of 192.0.2.0/24 (public space) and of
// 127.0.0.1/32, and really ran both. An unconstrained sweep makes the appliance a port scanner
// pointed wherever it has a route.

func TestHostsRefusesPublicAddressSpace(t *testing.T) {
	for _, target := range []string{
		"192.0.2.0/24",  // TEST-NET-1
		"198.51.100.7",  // TEST-NET-2, a bare host
		"8.8.8.8/32",    // a real public address
		"172.32.0.0/16", // just OUTSIDE 172.16/12 — the boundary people get wrong
	} {
		if _, err := Hosts([]string{target}, 4096); err == nil {
			t.Errorf("Hosts(%q) must be refused: a scan is LAN-local", target)
		}
	}
}

func TestHostsRefusesTheApplianceOwnInterior(t *testing.T) {
	for _, target := range []string{"127.0.0.1/32", "127.0.0.1", "0.0.0.0", "::1"} {
		if _, err := Hosts([]string{target}, 4096); err == nil {
			t.Errorf("Hosts(%q) must be refused: loopback is not a LAN", target)
		}
	}
}

func TestHostsAllowsEveryRangeASiteActuallyNumbersEquipmentOut_Of(t *testing.T) {
	// The guard must not be a blanket no — refusing legitimate ranges would break discovery
	// rather than constrain it.
	for _, target := range []string{
		"10.4.4.0/30", "172.16.9.0/30", "172.31.9.0/30", "192.168.1.0/30",
		"100.64.3.0/30",  // CGNAT: some site networks really are numbered here
		"169.254.7.0/30", // link-local: gear that came up with no DHCP server
	} {
		hosts, err := Hosts([]string{target}, 4096)
		if err != nil {
			t.Errorf("Hosts(%q) must be allowed: %v", target, err)
			continue
		}
		if len(hosts) == 0 {
			t.Errorf("Hosts(%q) allowed but expanded to nothing", target)
		}
	}
}

func TestHostsRefusesAMixedListRatherThanScanningTheGoodHalf(t *testing.T) {
	// One bad target poisons the request. Silently scanning the LAN half and dropping the rest
	// would hide the mistake from the admin who made it.
	if _, err := Hosts([]string{"192.168.1.0/30", "8.8.8.8"}, 4096); err == nil {
		t.Error("a list containing a public target must be refused whole")
	}
}

func TestHostsRefusesATargetItCannotCheck(t *testing.T) {
	// A name that does not resolve cannot be shown to be LAN-local, so it is not scanned. Fail
	// closed: the alternative is probing something nobody could vouch for.
	if _, err := Hosts([]string{"no-such-host.invalid"}, 4096); err == nil {
		t.Error("an unresolvable target must be refused, not scanned")
	}
}

func TestParseENIPIdentity(t *testing.T) {
	name := "1756-EN2T"
	item := make([]byte, 0, 64)
	item = append(item, 0x01, 0x00)                     // protocol version
	item = append(item, make([]byte, 16)...)            // socket address
	item = binary.LittleEndian.AppendUint16(item, 1)    // vendor id = 1 (Rockwell)
	item = binary.LittleEndian.AppendUint16(item, 0x0C) // device type
	item = binary.LittleEndian.AppendUint16(item, 0x00) // product code
	item = append(item, 0x01, 0x02)                     // revision
	item = binary.LittleEndian.AppendUint16(item, 0)    // status
	item = append(item, 0xDE, 0xAD, 0xBE, 0xEF)         // serial
	item = append(item, byte(len(name)))                // product name length
	item = append(item, []byte(name)...)                // product name
	item = append(item, 0x03)                           // state

	resp := make([]byte, 24)
	binary.LittleEndian.PutUint16(resp[0:], 0x0063)  // ListIdentity reply
	resp = binary.LittleEndian.AppendUint16(resp, 1) // CPF item count
	resp = binary.LittleEndian.AppendUint16(resp, 0x000C)
	resp = binary.LittleEndian.AppendUint16(resp, uint16(len(item)))
	resp = append(resp, item...)

	vid, product, ok := parseENIPIdentity(resp)
	if !ok || vid != 1 || product != name {
		t.Fatalf("parseENIPIdentity = (%d, %q, %v), want (1, %q, true)", vid, product, ok, name)
	}
	if enipVendor(1) == "" {
		t.Error("vendor 1 should have a name")
	}
}

func TestParseBACnetIAm(t *testing.T) {
	// object identifier: device (type 8) instance 1234.
	objid := (uint32(8) << 22) | 1234
	pkt := []byte{0x81, 0x0a, 0x00, 0x00, 0x01, 0x00, 0x10, 0x00} // BVLC + NPDU + APDU(I-Am)
	pkt = append(pkt, 0xC4)                                       // app tag 12 (object id), len 4
	pkt = binary.BigEndian.AppendUint32(pkt, objid)
	pkt = append(pkt, 0x22, 0x01, 0xE0) // max APDU (unsigned, len 2)
	pkt = append(pkt, 0x91, 0x00)       // segmentation (enum, len 1)
	pkt = append(pkt, 0x21, 0x0F)       // vendor id (unsigned, len 1) = 15

	inst, vid, ok := parseBACnetIAm(pkt)
	if !ok || inst != 1234 || vid != 15 {
		t.Fatalf("parseBACnetIAm = (%d, %d, %v), want (1234, 15, true)", inst, vid, ok)
	}
}

func TestParseSSDPHeaders(t *testing.T) {
	resp := "HTTP/1.1 200 OK\r\nSERVER: Linux/3.14 UPnP/1.0 Sonos/1.0\r\nST: urn:schemas-upnp-org:device:ZonePlayer:1\r\nUSN: uuid:abc::x\r\nLOCATION: http://192.168.1.9:1400/xml/device_description.xml\r\n\r\n"
	h := parseSSDPHeaders(resp)
	if h["server"] != "Linux/3.14 UPnP/1.0 Sonos/1.0" {
		t.Errorf("server = %q", h["server"])
	}
	if h["location"] == "" || h["st"] == "" || h["usn"] == "" {
		t.Errorf("missing headers: %#v", h)
	}
}
