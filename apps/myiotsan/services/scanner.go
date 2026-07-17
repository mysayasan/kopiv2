package services

import (
	"context"
	"fmt"
	"hash/fnv"
	"strings"
	"time"

	"github.com/mysayasan/kopiv2/apps/myiotsan/entities"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
	"github.com/mysayasan/kopiv2/infra/iot/discover"
)

// ScanService is the ACTIVE half of discovery: instead of waiting for a device to announce itself
// over MQTT (the enrollment window), it sweeps the LAN for devices already present and turns each
// into the SAME quarantined candidate an announcing device becomes. So a scan never adds a device —
// it only proposes candidates an admin then adopts, exactly like the announce path.
//
// It is deliberately conservative: opt-in (an admin triggers it), bounded (host cap + per-scan
// timeout), READ-ONLY (no scanner here writes to a device), and audited. Scanning an industrial
// network can perturb fragile gear, so nothing here broadcasts a write or a control command.
type ScanService struct {
	candidates dbsql.IGenericRepo[entities.DiscoveredDevice]
	profiles   *ProfileService
	audit      func(ctx context.Context, msg string, data map[string]any)
	logf       func(string, ...any)
}

func NewScanService(db dbsql.IDbCrud, profiles *ProfileService, audit func(ctx context.Context, msg string, data map[string]any), logf func(string, ...any)) *ScanService {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	if audit == nil {
		audit = func(context.Context, string, map[string]any) {}
	}
	return &ScanService{
		candidates: dbsql.NewGenericRepo[entities.DiscoveredDevice](db),
		profiles:   profiles,
		audit:      audit,
		logf:       logf,
	}
}

// ScanRequest selects which scanners to run and the Modbus sweep parameters.
type ScanRequest struct {
	Types           []string `json:"types"` // any of: modbus, mdns, ssdp, ethernetip, bacnet
	CIDR            string   `json:"cidr"`  // for the Modbus sweep, e.g. "192.168.1.0/24" (space/comma separated ok)
	ModbusPort      int      `json:"modbusPort"`
	ModbusTransport string   `json:"modbusTransport"` // "tcp" (default) or "rtutcp"
	Units           []int    `json:"units"`           // Modbus unit ids to try (default [1])
}

// ScanResult is the summary the UI shows; the actual devices appear in the candidate list.
type ScanResult struct {
	Found  int            `json:"found"`
	ByType map[string]int `json:"byType"`
}

const (
	scanHostCap    = 1024
	scanTimeout    = 4 * time.Second
	scanModbusPerOp = 800 * time.Millisecond
)

// Scan runs the requested scanners, records what they find as candidates, and returns a summary.
func (s *ScanService) Scan(ctx context.Context, req ScanRequest, actor int64) (ScanResult, error) {
	want := map[string]bool{}
	for _, t := range req.Types {
		want[strings.ToLower(strings.TrimSpace(t))] = true
	}

	var founds []discover.Found
	if want["modbus"] {
		hosts, err := discover.Hosts(splitList(req.CIDR), scanHostCap)
		if err != nil {
			return ScanResult{}, err
		}
		if len(hosts) == 0 {
			return ScanResult{}, fmt.Errorf("a Modbus scan needs a network range (CIDR) or host list")
		}
		founds = append(founds, discover.ScanModbus(ctx, hosts, req.ModbusPort, req.ModbusTransport, req.Units, scanModbusPerOp, 32)...)
	}
	if want["mdns"] {
		founds = append(founds, discover.ScanMDNS(ctx, scanTimeout)...)
	}
	if want["ssdp"] {
		founds = append(founds, discover.ScanSSDP(ctx, scanTimeout)...)
	}
	if want["ethernetip"] {
		founds = append(founds, discover.ScanEtherNetIP(ctx, scanTimeout)...)
	}
	if want["bacnet"] {
		founds = append(founds, discover.ScanBACnet(ctx, scanTimeout)...)
	}

	sunspecID := s.profileIDBySlug(ctx, "generic-sunspec-solar")
	byType := map[string]int{}
	recorded := 0
	for _, f := range founds {
		byType[f.Source]++
		if s.recordCandidate(ctx, f, sunspecID) {
			recorded++
		}
	}
	s.audit(ctx, fmt.Sprintf("network scan found %d device(s)", len(founds)), map[string]any{"actor": actor, "byType": byType})
	s.logf("scan by user %d: %d found, %d recorded %v", actor, len(founds), recorded, byType)
	return ScanResult{Found: len(founds), ByType: byType}, nil
}

// recordCandidate upserts a Found as a DiscoveredDevice, deduping by the synthetic device key so a
// re-scan refreshes rather than duplicates. Returns false only if the candidate cap is hit.
func (s *ScanService) recordCandidate(ctx context.Context, f discover.Found, sunspecID int64) bool {
	key := scanDeviceKey(f)
	now := time.Now().Unix()

	// An identified SunSpec Modbus device gets the generic-sunspec profile suggested; everything
	// else (unidentified Modbus, a TV, a PLC) is left for the admin to classify.
	suggested := int64(0)
	if f.Source == "modbus" && strings.TrimSpace(f.Vendor) != "" {
		suggested = sunspecID
	}

	existing, err := s.candidates.GetByUnique(ctx, "", "device_key", key)
	if err == nil && existing != nil {
		existing.LastSeenAt = now
		existing.MessageCount++
		existing.Source = f.Source
		existing.Address = f.Address
		existing.Payload = truncate(f.Detail, maxPayloadSample)
		existing.ObservedKeys = f.Name
		existing.Endpoint = f.Endpoint
		existing.Unit = f.Unit
		existing.Transport = f.Transport
		if suggested != 0 {
			existing.SuggestedProfileId = suggested
		}
		_, _ = s.candidates.UpdateById(ctx, "", *existing)
		return true
	}

	if _, total, cerr := s.candidates.Get(ctx, "", 1, 0, nil, nil); cerr == nil && total >= maxCandidates {
		s.logf("scan: candidate cap (%d) reached — ignoring %q", maxCandidates, key)
		return false
	}
	_, _ = s.candidates.Create(ctx, "", entities.DiscoveredDevice{
		DeviceKey:          key,
		Payload:            truncate(f.Detail, maxPayloadSample),
		ObservedKeys:       f.Name,
		SuggestedProfileId: suggested,
		Source:             f.Source,
		Address:            f.Address,
		Endpoint:           f.Endpoint,
		Unit:               f.Unit,
		Transport:          f.Transport,
		MessageCount:       1,
		FirstSeenAt:        now,
		LastSeenAt:         now,
	})
	return true
}

func (s *ScanService) profileIDBySlug(ctx context.Context, slug string) int64 {
	profiles, err := s.profiles.List(ctx)
	if err != nil {
		return 0
	}
	for _, p := range profiles {
		if p.Slug == slug {
			return p.Id
		}
	}
	return 0
}

// scanDeviceKey builds a stable, unique candidate key for a Found so re-scans dedupe. Modbus keys
// carry endpoint+unit (the thing that identifies a poll target); the LAN protocols key on address
// plus a hash of the name (one IP can advertise several mDNS services).
func scanDeviceKey(f discover.Found) string {
	san := func(s string) string {
		return strings.NewReplacer(":", "-", ".", "-", "/", "-", " ", "-").Replace(strings.TrimSpace(s))
	}
	switch f.Source {
	case "modbus":
		return fmt.Sprintf("modbus-%s-u%d", san(f.Endpoint), f.Unit)
	case "mdns", "ssdp":
		return fmt.Sprintf("%s-%s-%s", f.Source, san(f.Address), shortHash(f.Name))
	default:
		return fmt.Sprintf("%s-%s", f.Source, san(f.Address))
	}
}

func shortHash(s string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(s))
	return fmt.Sprintf("%08x", h.Sum32())
}

func splitList(s string) []string {
	fields := strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' || r == '\n' || r == '\t' })
	return fields
}
