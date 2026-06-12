package discovery

import (
	"context"
	"net"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	MethodSSDPUPnP = "ssdp"
	MethodMDNS     = "mdns"
	MethodPortScan = "portscan"
	MethodSADP     = "sadp"
)

// Device is the normalised result returned by all scanner implementations.
type Device struct {
	IP              string            `json:"ip"`
	Hostname        string            `json:"hostname,omitempty"`
	Manufacturer    string            `json:"manufacturer,omitempty"`
	Model           string            `json:"model,omitempty"`
	Serial          string            `json:"serial,omitempty"`
	FirmwareVersion string            `json:"firmwareVersion,omitempty"`
	Methods         []string          `json:"methods"`
	OpenPorts       []int             `json:"openPorts,omitempty"`
	RTSPURL         string            `json:"rtspUrl,omitempty"`
	HTTPPort        int               `json:"httpPort,omitempty"`
	HTTPSPort       int               `json:"httpsPort,omitempty"`
	Metadata        map[string]string `json:"metadata,omitempty"`
}

// Discover runs the specified discovery methods concurrently and returns
// deduplicated results merged by IP address.
// If methods is empty or contains "all", every available method is run.
// Valid method values: "ssdp", "mdns", "sadp", "portscan", "all".
func Discover(ctx context.Context, timeout time.Duration, methods ...string) []Device {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	run := make(map[string]bool, len(methods))
	for _, m := range methods {
		run[strings.ToLower(strings.TrimSpace(m))] = true
	}
	runAll := len(run) == 0 || run["all"]

	scanCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var mu sync.Mutex
	seen := make(map[string]*Device)

	add := func(devices []Device) {
		mu.Lock()
		defer mu.Unlock()
		for i := range devices {
			d := &devices[i]
			if d.IP == "" {
				continue
			}
			if existing, ok := seen[d.IP]; ok {
				mergeDevice(existing, d)
			} else {
				cp := *d
				seen[d.IP] = &cp
			}
		}
	}

	var wg sync.WaitGroup

	if runAll || run[MethodSSDPUPnP] {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if results, err := DiscoverSSDP(scanCtx, timeout); err == nil {
				add(results)
			}
		}()
	}

	if runAll || run[MethodMDNS] {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if results, err := DiscoverMDNS(scanCtx, timeout); err == nil {
				add(results)
			}
		}()
	}

	if runAll || run[MethodSADP] {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if results, err := DiscoverSADP(scanCtx, timeout); err == nil {
				add(results)
			}
		}()
	}

	if runAll || run[MethodPortScan] {
		cidrs := localSubnetCIDRs()
		for _, cidr := range cidrs {
			cidr := cidr
			wg.Add(1)
			go func() {
				defer wg.Done()
				if results, err := DiscoverPortScan(scanCtx, cidr); err == nil {
					add(results)
				}
			}()
		}
	}

	wg.Wait()

	result := make([]Device, 0, len(seen))
	for _, d := range seen {
		result = append(result, *d)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].IP < result[j].IP
	})
	return result
}

// MergeDevices merges extra into base, deduplicating by IP and combining fields.
func MergeDevices(base, extra []Device) []Device {
	seen := make(map[string]*Device, len(base))
	result := make([]Device, len(base))
	copy(result, base)
	for i := range result {
		seen[result[i].IP] = &result[i]
	}
	for i := range extra {
		d := &extra[i]
		if d.IP == "" {
			continue
		}
		if existing, ok := seen[d.IP]; ok {
			mergeDevice(existing, d)
		} else {
			result = append(result, *d)
			seen[d.IP] = &result[len(result)-1]
		}
	}
	return result
}

func mergeDevice(dst, src *Device) {
	if dst.Hostname == "" {
		dst.Hostname = src.Hostname
	}
	if dst.Manufacturer == "" {
		dst.Manufacturer = src.Manufacturer
	}
	if dst.Model == "" {
		dst.Model = src.Model
	}
	if dst.Serial == "" {
		dst.Serial = src.Serial
	}
	if dst.FirmwareVersion == "" {
		dst.FirmwareVersion = src.FirmwareVersion
	}
	if dst.RTSPURL == "" {
		dst.RTSPURL = src.RTSPURL
	}
	if dst.HTTPPort == 0 {
		dst.HTTPPort = src.HTTPPort
	}
	if dst.HTTPSPort == 0 {
		dst.HTTPSPort = src.HTTPSPort
	}
	// Merge methods (deduplicated).
	existing := make(map[string]bool, len(dst.Methods))
	for _, m := range dst.Methods {
		existing[m] = true
	}
	for _, m := range src.Methods {
		if !existing[m] {
			dst.Methods = append(dst.Methods, m)
		}
	}
	// Merge open ports (deduplicated).
	portSet := make(map[int]bool, len(dst.OpenPorts))
	for _, p := range dst.OpenPorts {
		portSet[p] = true
	}
	for _, p := range src.OpenPorts {
		if !portSet[p] {
			dst.OpenPorts = append(dst.OpenPorts, p)
		}
	}
	// Merge metadata (non-overwriting).
	if len(src.Metadata) > 0 {
		if dst.Metadata == nil {
			dst.Metadata = make(map[string]string)
		}
		for k, v := range src.Metadata {
			if _, ok := dst.Metadata[k]; !ok {
				dst.Metadata[k] = v
			}
		}
	}
}

// LocalSubnetCIDRs is the exported version of localSubnetCIDRs for use by API handlers.
func LocalSubnetCIDRs() []string { return localSubnetCIDRs() }

// localSubnetCIDRs returns all usable non-loopback IPv4 /24 subnets found on
// local interfaces. Scanning all of them avoids missing the right adapter when
// the machine has VPN, Docker, or VM bridge interfaces.
func localSubnetCIDRs() []string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	seen := make(map[string]bool)
	var cidrs []string
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			ip4 := ipNet.IP.To4()
			if ip4 == nil || ip4.IsLoopback() {
				continue
			}
			ones, bits := ipNet.Mask.Size()
			if bits != 32 {
				continue
			}
			var cidr string
			if ones < 24 {
				mask := net.CIDRMask(24, 32)
				cidr = (&net.IPNet{IP: ip4.Mask(mask), Mask: mask}).String()
			} else {
				cidr = (&net.IPNet{IP: ip4.Mask(ipNet.Mask), Mask: ipNet.Mask}).String()
			}
			if !seen[cidr] {
				seen[cidr] = true
				cidrs = append(cidrs, cidr)
			}
		}
	}
	return cidrs
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
