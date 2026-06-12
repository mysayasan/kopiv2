package discovery

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// Common camera TCP ports to probe.
// 8800 = TP-Link Tapo RTSP, 37777/34567 = Dahua proprietary.
var cameraPorts = []int{80, 443, 554, 8080, 8000, 8443, 8800, 37777, 34567}

// DiscoverPortScan probes each host in the given CIDR for camera-related open ports,
// then fingerprints the device type via HTTP and RTSP.
func DiscoverPortScan(ctx context.Context, cidr string) ([]Device, error) {
	hosts, err := subnetHosts(cidr)
	if err != nil {
		return nil, err
	}

	var (
		mu      sync.Mutex
		results []Device
		wg      sync.WaitGroup
		sem     = make(chan struct{}, 128) // max 128 concurrent host goroutines
	)

	for _, host := range hosts {
		if ctx.Err() != nil {
			break
		}
		host := host
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			dev, found := scanHost(ctx, host)
			if !found {
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

// subnetHosts expands a CIDR into usable host addresses (skips .0 and .255).
func subnetHosts(cidr string) ([]string, error) {
	_, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil, fmt.Errorf("portscan: parse cidr %q: %w", cidr, err)
	}
	var hosts []string
	for ip := cloneIP(ipNet.IP); ipNet.Contains(ip); incrementIP(ip) {
		last := ip[len(ip)-1]
		if last == 0 || last == 255 {
			continue
		}
		hosts = append(hosts, ip.String())
		if len(hosts) >= 254 { // hard cap regardless of subnet size
			break
		}
	}
	return hosts, nil
}

func cloneIP(ip net.IP) net.IP {
	cp := make(net.IP, len(ip))
	copy(cp, ip)
	return cp
}

func incrementIP(ip net.IP) {
	for i := len(ip) - 1; i >= 0; i-- {
		ip[i]++
		if ip[i] != 0 {
			break
		}
	}
}

// scanHost probes all camera ports on a single host concurrently.
// Returns (Device, true) only if at least one camera-relevant port is open.
func scanHost(ctx context.Context, ip string) (Device, bool) {
	type portResult struct {
		port int
		open bool
	}

	resultCh := make(chan portResult, len(cameraPorts))
	var wg sync.WaitGroup
	for _, port := range cameraPorts {
		port := port
		wg.Add(1)
		go func() {
			defer wg.Done()
			addr := fmt.Sprintf("%s:%d", ip, port)
			conn, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
			open := err == nil
			if conn != nil {
				conn.Close()
			}
			resultCh <- portResult{port, open}
		}()
	}
	wg.Wait()
	close(resultCh)

	var openPorts []int
	for r := range resultCh {
		if r.open {
			openPorts = append(openPorts, r.port)
		}
	}
	if len(openPorts) == 0 {
		return Device{}, false
	}
	sort.Ints(openPorts)

	dev := Device{
		IP:        ip,
		Methods:   []string{MethodPortScan},
		OpenPorts: openPorts,
	}

	// Pick HTTP / HTTPS ports.
	for _, p := range openPorts {
		if (p == 80 || p == 8080 || p == 8000) && dev.HTTPPort == 0 {
			dev.HTTPPort = p
		}
		if (p == 443 || p == 8443) && dev.HTTPSPort == 0 {
			dev.HTTPSPort = p
		}
	}

	// Fingerprint via HTTP first, then HTTPS if no HTTP port or no result.
	if dev.HTTPPort > 0 {
		httpFingerprint(ctx, ip, dev.HTTPPort, false, &dev)
	}
	if dev.HTTPSPort > 0 && dev.Manufacturer == "" {
		httpFingerprint(ctx, ip, dev.HTTPSPort, true, &dev)
	}

	// RTSP probe: standard port 554 and Tapo's port 8800.
	rtspPorts := []int{}
	for _, p := range openPorts {
		if p == 554 || p == 8800 {
			rtspPorts = append(rtspPorts, p)
		}
	}
	for _, p := range rtspPorts {
		rtspProbe(ctx, ip, p, &dev)
	}

	// Vendor hints from proprietary ports.
	for _, p := range openPorts {
		if (p == 37777 || p == 34567) && dev.Manufacturer == "" {
			dev.Manufacturer = "Dahua"
			break
		}
	}

	return dev, true
}

// cameraKeywords maps lower-case body/header substrings to manufacturer names.
// Empty string means "generic camera" — device is camera-like but brand unknown.
var cameraKeywords = []struct {
	keyword      string
	manufacturer string
}{
	{"hikvision", "Hikvision"},
	{"dahua", "Dahua"},
	{"tp-link", "TP-Link"},
	{"tapo", "TP-Link"},
	{"axis", "Axis"},
	{"reolink", "Reolink"},
	{"amcrest", "Amcrest"},
	{"foscam", "Foscam"},
	{"hanwha", "Hanwha"},
	{"uniview", "Uniview"},
	{"vivotek", "Vivotek"},
	{"mobotix", "Mobotix"},
	{"grandstream", "Grandstream"},
	{"network camera", ""},
	{"ip camera", ""},
	{"surveillance", ""},
	{"nvr", ""},
	{"dvr", ""},
}

// httpFingerprint probes an HTTP or HTTPS port and populates manufacturer/model.
func httpFingerprint(ctx context.Context, ip string, port int, https bool, dev *Device) {
	scheme := "http"
	if https {
		scheme = "https"
	}
	client := &http.Client{
		Timeout: 2 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
		},
	}
	url := fmt.Sprintf("%s://%s:%d/", scheme, ip, port)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return
	}
	resp, err := client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	server := strings.ToLower(resp.Header.Get("Server"))
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8*1024))
	combined := server + " " + strings.ToLower(string(body))

	// Tapo cameras return a specific JSON error structure — detect before generic keywords.
	if strings.Contains(combined, `"error_code"`) && strings.Contains(combined, `"code"`) {
		if dev.Manufacturer == "" {
			dev.Manufacturer = "TP-Link"
		}
		if dev.Metadata == nil {
			dev.Metadata = make(map[string]string)
		}
		dev.Metadata["apiType"] = "tapo"
	}

	// Fingerprint manufacturer from keywords.
	if dev.Manufacturer == "" {
		for _, kw := range cameraKeywords {
			if strings.Contains(combined, kw.keyword) {
				dev.Manufacturer = kw.manufacturer
				break
			}
		}
	}

	// Extract page title as model hint.
	if dev.Model == "" {
		bodyStr := string(body)
		if start := strings.Index(strings.ToLower(bodyStr), "<title>"); start >= 0 {
			start += 7
			if end := strings.Index(strings.ToLower(bodyStr)[start:], "</title>"); end >= 0 {
				title := strings.TrimSpace(bodyStr[start : start+end])
				if title != "" && !strings.EqualFold(title, "loading...") && !strings.EqualFold(title, "redirect") {
					dev.Model = title
				}
			}
		}
	}
}

// rtspProbe sends an RTSP OPTIONS to the given port and populates server info.
func rtspProbe(ctx context.Context, ip string, port int, dev *Device) {
	dialCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	d := net.Dialer{}
	conn, err := d.DialContext(dialCtx, "tcp", fmt.Sprintf("%s:%d", ip, port))
	if err != nil {
		return
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(2 * time.Second))

	fmt.Fprintf(conn, "OPTIONS * RTSP/1.0\r\nCSeq: 1\r\nUser-Agent: camera-scan\r\n\r\n")

	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		line := scanner.Text()
		lower := strings.ToLower(line)
		if strings.HasPrefix(lower, "server:") {
			server := strings.TrimSpace(line[7:])
			if dev.Metadata == nil {
				dev.Metadata = make(map[string]string)
			}
			dev.Metadata["rtspServer"] = server
			serverLower := strings.ToLower(server)
			if dev.Manufacturer == "" {
				for _, kw := range cameraKeywords {
					if kw.manufacturer != "" && strings.Contains(serverLower, kw.keyword) {
						dev.Manufacturer = kw.manufacturer
						break
					}
				}
			}
		}
		if line == "" {
			break
		}
	}

	if dev.RTSPURL == "" {
		dev.RTSPURL = fmt.Sprintf("rtsp://%s:%d/stream1", ip, port)
	}
}
