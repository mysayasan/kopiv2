package discovery

import (
	"bufio"
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const ssdpMSearch = "M-SEARCH * HTTP/1.1\r\nHOST: 239.255.255.250:1900\r\nMAN: \"ssdp:discover\"\r\nMX: 3\r\nST: ssdp:all\r\n\r\n"

// DiscoverSSDP sends an SSDP M-SEARCH multicast and fetches UPnP device descriptors.
func DiscoverSSDP(ctx context.Context, timeout time.Duration) ([]Device, error) {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}

	conn, err := net.ListenUDP("udp4", &net.UDPAddr{})
	if err != nil {
		return nil, fmt.Errorf("ssdp: open socket: %w", err)
	}
	defer conn.Close()

	deadline := time.Now().Add(timeout)
	if dl, ok := ctx.Deadline(); ok && dl.Before(deadline) {
		deadline = dl
	}
	conn.SetDeadline(deadline)

	dst, err := net.ResolveUDPAddr("udp4", "239.255.255.250:1900")
	if err != nil {
		return nil, fmt.Errorf("ssdp: resolve multicast addr: %w", err)
	}
	if _, err := conn.WriteToUDP([]byte(ssdpMSearch), dst); err != nil {
		return nil, fmt.Errorf("ssdp: send M-SEARCH: %w", err)
	}

	// Collect unique LOCATION URLs.
	locations := make(map[string]string) // location → source IP
	buf := make([]byte, 16*1024)
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
		if loc := parseSSDPLocation(buf[:n]); loc != "" {
			if _, seen := locations[loc]; !seen {
				locations[loc] = src.IP.String()
			}
		}
	}

done:
	if len(locations) == 0 {
		return nil, nil
	}

	client := &http.Client{Timeout: 3 * time.Second}
	var (
		results []Device
		mu      sync.Mutex
		wg      sync.WaitGroup
	)
	for loc, srcIP := range locations {
		loc, srcIP := loc, srcIP
		wg.Add(1)
		go func() {
			defer wg.Done()
			dev, err := fetchUPnPDescriptor(ctx, client, loc, srcIP)
			if err != nil {
				return
			}
			mu.Lock()
			results = append(results, dev)
			mu.Unlock()
		}()
	}
	wg.Wait()
	return results, nil
}

// parseSSDPLocation extracts the LOCATION header from a raw SSDP response.
func parseSSDPLocation(data []byte) string {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(strings.ToUpper(line), "LOCATION:") {
			return strings.TrimSpace(line[9:])
		}
	}
	return ""
}

type upnpRoot struct {
	XMLName xml.Name   `xml:"root"`
	Device  upnpDevice `xml:"device"`
}

type upnpDevice struct {
	DeviceType       string `xml:"deviceType"`
	FriendlyName     string `xml:"friendlyName"`
	Manufacturer     string `xml:"manufacturer"`
	ModelName        string `xml:"modelName"`
	ModelDescription string `xml:"modelDescription"`
	SerialNumber     string `xml:"serialNumber"`
	PresentationURL  string `xml:"presentationURL"`
}

func fetchUPnPDescriptor(ctx context.Context, client *http.Client, location, srcIP string) (Device, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, location, nil)
	if err != nil {
		return Device{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return Device{}, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if err != nil {
		return Device{}, err
	}

	var root upnpRoot
	if err := xml.Unmarshal(body, &root); err != nil {
		return Device{}, err
	}

	dev := root.Device
	return Device{
		IP:           srcIP,
		Hostname:     firstNonEmpty(dev.FriendlyName),
		Manufacturer: firstNonEmpty(dev.Manufacturer),
		Model:        firstNonEmpty(dev.ModelName, dev.ModelDescription),
		Serial:       firstNonEmpty(dev.SerialNumber),
		Methods:      []string{MethodSSDPUPnP},
		Metadata: map[string]string{
			"deviceType":   dev.DeviceType,
			"friendlyName": dev.FriendlyName,
			"location":     location,
		},
	}, nil
}
