package discovery

import (
	"context"
	"encoding/xml"
	"fmt"
	"net"
	"time"

	"github.com/google/uuid"
)

// DiscoverSADP sends a Hikvision SADP probe to the multicast group and parses responses.
// SADP is Hikvision's proprietary device discovery protocol, used by iVMS and SADP tool.
func DiscoverSADP(ctx context.Context, timeout time.Duration) ([]Device, error) {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}

	conn, err := net.ListenUDP("udp4", &net.UDPAddr{})
	if err != nil {
		return nil, fmt.Errorf("sadp: open socket: %w", err)
	}
	defer conn.Close()

	deadline := time.Now().Add(timeout)
	if dl, ok := ctx.Deadline(); ok && dl.Before(deadline) {
		deadline = dl
	}
	conn.SetDeadline(deadline)

	dst, err := net.ResolveUDPAddr("udp4", "239.255.255.250:37020")
	if err != nil {
		return nil, fmt.Errorf("sadp: resolve multicast addr: %w", err)
	}

	probe := buildSADPProbe()
	if _, err := conn.WriteToUDP(probe, dst); err != nil {
		return nil, fmt.Errorf("sadp: send probe: %w", err)
	}

	var devices []Device
	seen := make(map[string]bool)
	buf := make([]byte, 16*1024)
	for {
		select {
		case <-ctx.Done():
			goto done
		default:
		}
		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			break
		}
		dev, err := parseSADPResponse(buf[:n])
		if err != nil || dev.IP == "" {
			continue
		}
		if !seen[dev.IP] {
			seen[dev.IP] = true
			devices = append(devices, dev)
		}
	}

done:
	return devices, nil
}

type sadpProbe struct {
	XMLName xml.Name `xml:"Probe"`
	Uuid    string   `xml:"Uuid"`
	Types   string   `xml:"Types"`
}

// sadpProbeMatch handles both the flat format and the nested DeviceDescription format
// used by different Hikvision firmware versions.
type sadpProbeMatch struct {
	XMLName xml.Name    `xml:"ProbeMatch"`
	Uuid    string      `xml:"Uuid"`
	Types   string      `xml:"Types"`
	// Flat fields (newer firmware).
	DeviceType      string `xml:"DeviceType"`
	Manufacturer    string `xml:"Manufacturer"`
	Model           string `xml:"Model"`
	SeriNo          string `xml:"SeriNo"`
	SerialNumber    string `xml:"SerialNumber"`
	FirmwareVersion string `xml:"FirmwareVersion"`
	Firmware        string `xml:"Firmware"`
	MAC             string `xml:"MAC"`
	IPv4Address     string `xml:"IPv4Address"`
	HttpPort        int    `xml:"HttpPort"`
	// Nested format (older firmware).
	DeviceDescription sadpDeviceDesc `xml:"DeviceDescription"`
}

type sadpDeviceDesc struct {
	FriendlyName    string `xml:"FriendlyName"`
	Manufacturer    string `xml:"Manufacturer"`
	Model           string `xml:"Model"`
	SerialNumber    string `xml:"SerialNumber"`
	FirmwareVersion string `xml:"FirmwareVersion"`
	MACAddress      string `xml:"MACAddress"`
	IPv4Address     string `xml:"IPv4Address"`
	HttpPort        int    `xml:"HttpPort"`
}

func buildSADPProbe() []byte {
	probe := sadpProbe{
		Uuid:  uuid.NewString(),
		Types: "inquiry",
	}
	data, _ := xml.Marshal(probe)
	return append([]byte(xml.Header), data...)
}

func parseSADPResponse(data []byte) (Device, error) {
	var match sadpProbeMatch
	if err := xml.Unmarshal(data, &match); err != nil {
		return Device{}, err
	}

	ip := firstNonEmpty(match.IPv4Address, match.DeviceDescription.IPv4Address)
	if ip == "" {
		return Device{}, fmt.Errorf("sadp: no IP in response")
	}

	manufacturer := firstNonEmpty(match.Manufacturer, match.DeviceDescription.Manufacturer, "Hikvision")
	model := firstNonEmpty(match.Model, match.DeviceDescription.Model, match.DeviceType)
	serial := firstNonEmpty(match.SeriNo, match.SerialNumber, match.DeviceDescription.SerialNumber)
	fw := firstNonEmpty(match.FirmwareVersion, match.Firmware, match.DeviceDescription.FirmwareVersion)
	mac := firstNonEmpty(match.MAC, match.DeviceDescription.MACAddress)
	httpPort := match.HttpPort
	if httpPort == 0 {
		httpPort = match.DeviceDescription.HttpPort
	}

	dev := Device{
		IP:              ip,
		Manufacturer:    manufacturer,
		Model:           model,
		Serial:          serial,
		FirmwareVersion: fw,
		Methods:         []string{MethodSADP},
		HTTPPort:        httpPort,
		Metadata:        make(map[string]string),
	}
	if mac != "" {
		dev.Metadata["mac"] = mac
	}
	if match.DeviceType != "" {
		dev.Metadata["deviceType"] = match.DeviceType
	}
	if firstNonEmpty(match.DeviceDescription.FriendlyName) != "" {
		dev.Hostname = match.DeviceDescription.FriendlyName
	}
	return dev, nil
}
