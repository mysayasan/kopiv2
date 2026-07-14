package services

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mysayasan/kopiv2/apps/myiotsan/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
	"golang.org/x/crypto/bcrypt"
)

// Enrollment is how a device that does not exist yet gets into the inventory.
//
// THE TENSION IT RESOLVES. The broker's security model is that a device not in iot_device
// cannot connect — that is what makes the device table the credential store, and what makes
// deleting a device actually revoke it. But a building with two hundred door contacts cannot
// be onboarded by typing two hundred device keys by hand, and a device that does not exist yet
// cannot announce itself.
//
// So the hole is opened deliberately, and it is built to be safe to open:
//
//   - TIME-BOXED. An admin opens a window; it expires on its own. There is no way to leave it
//     open by forgetting about it, which is how every "temporary" provisioning mode ends up
//     permanent.
//   - SECRET-GATED. The window mints a one-time key with real entropy, shown once. There is no
//     default and nothing to guess.
//   - QUARANTINED, which is the property that actually matters. An enrolling client's payloads
//     are recorded as a CANDIDATE and nowhere else: no telemetry is stored, no rule is
//     evaluated. Somebody who gets into the window can leave junk for an admin to decline. They
//     cannot forge a reading, move a chart, or trigger or suppress an alert.
//   - CAPPED. A flood of candidates cannot fill the disk.
//   - AUDITED. Opening a window is a security event in the notification feed, so it cannot be
//     done quietly.
//
// Adoption is the deliberate act: the admin sees what the thing actually sends, is told what it
// probably is, and mints it a real credential.
type Enrollment struct {
	candidates dbsql.IGenericRepo[entities.DiscoveredDevice]
	profiles   *ProfileService
	logf       func(format string, args ...any)

	mu        sync.RWMutex
	keyHash   []byte
	expiresAt time.Time
	openedBy  int64
}

// maxCandidates caps the candidate table. A window is minutes long and a real site has tens of
// devices announcing; anything past this is noise or abuse, and the cap is what stops it from
// becoming a disk-filling attack.
const maxCandidates = 500

// maxPayloadSample is how much of an observed payload is kept. Enough to see what the device
// sends; not enough to be a telemetry store by accident.
const maxPayloadSample = 2000

func NewEnrollment(db dbsql.IDbCrud, profiles *ProfileService, logf func(string, ...any)) *Enrollment {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &Enrollment{
		candidates: dbsql.NewGenericRepo[entities.DiscoveredDevice](db),
		profiles:   profiles,
		logf:       logf,
	}
}

// WindowStatus is what the UI shows about the enrollment window.
type WindowStatus struct {
	Open      bool  `json:"open"`
	ExpiresAt int64 `json:"expiresAt"`
	// Key is returned ONLY by Open, and only once. It is never readable afterwards.
	Key string `json:"key,omitempty"`
}

// Open starts an enrollment window and returns the key to flash into the devices.
//
// Re-opening replaces the previous window: there is only ever one, so an admin cannot
// accidentally leave two keys valid.
func (e *Enrollment) Open(ctx context.Context, ttl time.Duration, actor int64) (WindowStatus, error) {
	if ttl <= 0 || ttl > time.Hour {
		// An hour is already generous for walking a building with a phone. A window measured in
		// days is not a window, it is a permanently open door with a nicer name.
		ttl = 10 * time.Minute
	}
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return WindowStatus{}, err
	}
	key := base64.RawURLEncoding.EncodeToString(buf)
	hash, err := bcrypt.GenerateFromPassword([]byte(key), bcrypt.DefaultCost)
	if err != nil {
		return WindowStatus{}, err
	}

	e.mu.Lock()
	e.keyHash = hash
	e.expiresAt = time.Now().Add(ttl)
	e.openedBy = actor
	expires := e.expiresAt
	e.mu.Unlock()

	e.logf("enrollment window opened for %s by user %d — unknown devices presenting the key will be admitted, quarantined", ttl, actor)
	return WindowStatus{Open: true, ExpiresAt: expires.Unix(), Key: key}, nil
}

// Close ends the window immediately.
func (e *Enrollment) Close() {
	e.mu.Lock()
	e.keyHash = nil
	e.expiresAt = time.Time{}
	e.mu.Unlock()
	e.logf("enrollment window closed")
}

// Status reports the window without ever revealing the key.
func (e *Enrollment) Status() WindowStatus {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if !e.isOpen() {
		return WindowStatus{Open: false}
	}
	return WindowStatus{Open: true, ExpiresAt: e.expiresAt.Unix()}
}

// isOpen must be called with at least a read lock held.
func (e *Enrollment) isOpen() bool {
	return e.keyHash != nil && time.Now().Before(e.expiresAt)
}

// VerifyKey checks an enrolling client's credential. A closed or expired window verifies
// nothing — the expiry is enforced here, on every connect, not by a sweeper that might not have
// run yet.
func (e *Enrollment) VerifyKey(password string) bool {
	e.mu.RLock()
	hash := e.keyHash
	open := e.isOpen()
	e.mu.RUnlock()

	if !open || len(hash) == 0 {
		return false
	}
	return bcrypt.CompareHashAndPassword(hash, []byte(password)) == nil
}

// Observe records what a quarantined client published.
//
// This is the ONLY thing an enrolling client's payload is allowed to do. It does not become
// telemetry and it is not seen by any rule.
func (e *Enrollment) Observe(ctx context.Context, deviceKey, topic string, payload []byte) {
	keys := payloadKeys(payload)
	now := time.Now().Unix()

	existing, err := e.candidates.GetByUnique(ctx, "", "device_key", deviceKey)
	if err != nil && !isNoResultErr(err) {
		return
	}

	if existing != nil {
		existing.LastSeenAt = now
		existing.MessageCount++
		existing.Topic = topic
		existing.Payload = truncate(string(payload), maxPayloadSample)
		if len(keys) > 0 {
			existing.ObservedKeys = strings.Join(keys, ",")
			existing.SuggestedProfileId = e.suggestProfile(ctx, topic, keys)
		}
		_, _ = e.candidates.UpdateById(ctx, "", *existing)
		return
	}

	// A new candidate. Cap the table: a window is minutes long, and anything past the cap is
	// noise or abuse.
	if _, total, cerr := e.candidates.Get(ctx, "", 1, 0, nil, nil); cerr == nil && total >= maxCandidates {
		e.logf("enrollment: candidate cap (%d) reached — ignoring %q", maxCandidates, deviceKey)
		return
	}

	_, _ = e.candidates.Create(ctx, "", entities.DiscoveredDevice{
		DeviceKey:          deviceKey,
		Topic:              topic,
		Payload:            truncate(string(payload), maxPayloadSample),
		ObservedKeys:       strings.Join(keys, ","),
		SuggestedProfileId: e.suggestProfile(ctx, topic, keys),
		MessageCount:       1,
		FirstSeenAt:        now,
		LastSeenAt:         now,
	})
	e.logf("enrollment: new candidate %q on %q", deviceKey, topic)
}

// suggestProfile guesses what a candidate IS, from what it says.
//
// This is the difference between onboarding that scales and onboarding that does not. A device
// publishing {temperature, humidity, battery} is almost certainly a temp/humidity sensor, and
// the app should say so rather than making an installer read a payload and match it against a
// catalog by eye, two hundred times.
//
// Scoring is deliberately conservative. It is the fraction of the PROFILE's keys that the
// device actually sent — so a profile whose keys are all present wins, and a profile that
// merely shares "battery" with everything does not. Below the floor, it suggests nothing at
// all: a wrong suggestion an installer accepts without thinking is worse than no suggestion,
// because it silently mis-decodes every reading that device will ever send.
func (e *Enrollment) suggestProfile(ctx context.Context, topic string, observed []string) int64 {
	if len(observed) == 0 {
		return 0
	}
	profiles, err := e.profiles.List(ctx)
	if err != nil || len(profiles) == 0 {
		return 0
	}

	seen := map[string]bool{}
	for _, k := range observed {
		seen[strings.ToLower(k)] = true
	}

	var bestId int64
	bestScore := 0.0
	for _, p := range profiles {
		keys, err := e.profiles.KeysFor(ctx, p.Id)
		if err != nil || len(keys) == 0 {
			continue
		}
		matched := 0
		for _, k := range keys {
			// Match on the payload field the key actually binds to (the JSON path's first
			// segment), not the key's display name.
			field := strings.ToLower(k.Key)
			if k.JsonPath != "" {
				field = strings.ToLower(strings.Split(k.JsonPath, ".")[0])
			}
			if seen[field] {
				matched++
			}
		}
		score := float64(matched) / float64(len(keys))

		// The topic is corroborating evidence, not proof: a device on "zigbee2mqtt/..." is
		// probably a Zigbee device, which narrows things but does not identify them.
		if prefix := topicPrefix(p.TopicTemplate); prefix != "" && strings.HasPrefix(topic, prefix) {
			score += 0.15
		}
		if score > bestScore {
			bestScore, bestId = score, p.Id
		}
	}

	// The floor. Below it, say nothing rather than guess: an installer who accepts a wrong
	// suggestion has silently mis-decoded every reading that device will ever send.
	if bestScore < 0.6 {
		return 0
	}
	return bestId
}

// topicPrefix extracts the fixed part of a topic template ("zigbee2mqtt/{deviceKey}" ->
// "zigbee2mqtt/").
func topicPrefix(template string) string {
	if i := strings.Index(template, "{"); i > 0 {
		return template[:i]
	}
	return ""
}

// payloadKeys lists the top-level field names in a JSON payload.
func payloadKeys(payload []byte) []string {
	var doc map[string]any
	if err := json.Unmarshal(payload, &doc); err != nil {
		return nil
	}
	keys := make([]string, 0, len(doc))
	for k := range doc {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}

// Candidates lists what is waiting to be adopted, chattiest first — a device that has spoken
// forty times is real; one that spoke once may have been a passing scan.
func (e *Enrollment) Candidates(ctx context.Context) ([]*entities.DiscoveredDevice, error) {
	rows, _, err := e.candidates.Get(ctx, "", 500, 0, nil,
		[]sqldataenums.Sorter{{FieldName: "MessageCount", Sort: sqldataenums.DESC}})
	if err != nil && isNoResultErr(err) {
		return []*entities.DiscoveredDevice{}, nil
	}
	return rows, err
}

// AdoptRequest turns a candidate into a real device.
type AdoptRequest struct {
	// ProfileId is what the admin decided it is — defaulting to the suggestion, but their call.
	ProfileId int64  `json:"profileId"`
	Name      string `json:"name"`
	Tag       string `json:"tag"`
	Location  string `json:"location"`
}

// Adopt promotes a candidate to a device and mints it a real credential.
//
// The credential is returned ONCE. The enrollment key got the device in the door; it does not
// stay valid for it, and it dies with the window — so a leaked enrollment key cannot be used to
// impersonate a device that was adopted with it.
func (e *Enrollment) Adopt(ctx context.Context, id int64, req AdoptRequest, devices *DeviceService, actor int64) (*ProvisionedDevice, error) {
	cand, err := e.candidates.GetById(ctx, "", uint64(id))
	if err != nil || cand == nil {
		return nil, fmt.Errorf("candidate not found")
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = cand.DeviceKey
	}
	profileId := req.ProfileId
	if profileId == 0 {
		profileId = cand.SuggestedProfileId
	}

	dev, err := devices.Create(ctx, CreateDeviceRequest{
		Name:      name,
		DeviceKey: cand.DeviceKey,
		Protocol:  "mqtt",
		ProfileId: profileId,
		Tag:       strings.TrimSpace(req.Tag),
		Location:  strings.TrimSpace(req.Location),
		Enabled:   true,
		// Read-only. Actuation is a separate, deliberate act — a device does not arrive able to
		// switch a relay just because somebody plugged it in.
		ActuationEnabled: false,
	}, actor)
	if err != nil {
		return nil, err
	}

	_, _ = e.candidates.DeleteById(ctx, "", uint64(id))
	e.logf("adopted %q as device %d", cand.DeviceKey, dev.Device.Id)
	return dev, nil
}

// Reject discards a candidate.
func (e *Enrollment) Reject(ctx context.Context, id int64) error {
	_, err := e.candidates.DeleteById(ctx, "", uint64(id))
	if err != nil && !isNoResultErr(err) {
		return err
	}
	return nil
}
