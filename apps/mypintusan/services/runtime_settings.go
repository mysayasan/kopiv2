package services

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	sharedentities "github.com/mysayasan/kopiv2/domain/entities"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// Runtime settings: config.json SEEDS the first run, the database owns it afterwards.
//
// This is mymatasan's pattern (see its services/runtime_settings.go), and it is the only shape that
// works for the market this app is sold into. A door system is bought by a facilities manager, not
// an engineer: nobody is going to SSH in and hand-edit an RS-485 bus list or paste a 32-character
// AES key into a JSON file. Once the app has booted once, every one of these values has to be
// reachable from a screen.
//
// So config.json is a SEED, not the source of truth. On first boot the defaults below are written
// into the runtime_setting row; from then on the row wins and config.json is ignored. Reset() puts
// the seeded values back, which is what makes a bad edit recoverable without touching a file.

const accessSettingsKey = "access"

// AccessSettings is everything about how this controller behaves that an operator may change.
type AccessSettings struct {
	// Timezone is the site's IANA zone. Schedules and holidays are LOCAL concepts — a night shift
	// and a public holiday are both wrong if evaluated in UTC.
	Timezone string `json:"timezone"`
	// TickSeconds drives the door timers (relock, held-open). It bounds how LATE an alarm can be,
	// not whether it fires.
	TickSeconds int `json:"tickSeconds"`
	// PINWindowSeconds is how long a keypad entry waits to be paired with a card.
	PINWindowSeconds int `json:"pinWindowSeconds"`
	// Offline marks this controller as serving from a cached replica.
	Offline bool `json:"offline"`
	// Buses are the RS-485 segments and the readers on them.
	Buses []BusSettings `json:"buses"`
}

// BusSettings is one segment.
type BusSettings struct {
	Port               string           `json:"port"`
	SlotMillis         int              `json:"slotMillis"`
	ReplyTimeoutMillis int              `json:"replyTimeoutMillis"`
	Readers            []ReaderSettings `json:"readers"`
}

// ReaderSettings binds one PD address to its Secure Channel policy.
type ReaderSettings struct {
	Address int `json:"address"`
	// SCBK is the 16-byte base key as hex.
	//
	// `json:"-"` on the way OUT is deliberate — see MarshalJSON below. A site key must never be
	// readable from an API response: anyone who can read it can decrypt the bus and impersonate a
	// reader, which is the exact attack Secure Channel exists to stop.
	SCBK                 string `json:"scbk"`
	RequireSecureChannel bool   `json:"requireSecureChannel"`
	// Label is a human name for the reader, so a screen can say "Front door, inside" rather than
	// "bus tcp://... address 3".
	Label string `json:"label"`
}

// redactedReader is the wire form of a reader: everything except the key.
type redactedReader struct {
	Address              int    `json:"address"`
	RequireSecureChannel bool   `json:"requireSecureChannel"`
	Label                string `json:"label"`
	// HasSCBK tells the UI a key is installed without revealing it, so the screen can show
	// "encrypted" or "not yet rekeyed" and offer a Rekey button.
	HasSCBK bool `json:"hasScbk"`
	// UsingDefaultKey warns that the reader is still on the published install-mode key, which is
	// not secure at all. The UI is expected to nag until it is replaced.
	UsingDefaultKey bool `json:"usingDefaultKey"`
}

// MarshalJSON redacts the base key.
//
// Redaction lives on the TYPE rather than in each handler on purpose: a future endpoint that
// returns settings — an export, a diagnostic dump, a support bundle — gets the redaction for free
// instead of leaking the key because somebody forgot.
func (r ReaderSettings) MarshalJSON() ([]byte, error) {
	key := strings.TrimSpace(r.SCBK)
	return json.Marshal(redactedReader{
		Address:              r.Address,
		RequireSecureChannel: r.RequireSecureChannel,
		Label:                r.Label,
		HasSCBK:              key != "",
		UsingDefaultKey:      strings.EqualFold(key, defaultSCBKHex),
	})
}

// defaultSCBKHex is the published install-mode key, so the UI can flag a reader that is not really
// secure. Keeping the literal here rather than importing the osdp package keeps this file free of
// driver dependencies.
const defaultSCBKHex = "303132333435363738393a3b3c3d3e3f"

// IAccessSettingsService reads and writes the runtime settings.
type IAccessSettingsService interface {
	Get(ctx context.Context) (AccessSettings, error)
	Save(ctx context.Context, s AccessSettings, actor int64) (AccessSettings, error)
	Reset(ctx context.Context, actor int64) (AccessSettings, error)
}

type accessSettingsService struct {
	repo     dbsql.IGenericRepo[sharedentities.RuntimeSetting]
	defaults AccessSettings
}

// NewAccessSettingsService creates the service seeded with the config.json defaults.
func NewAccessSettingsService(repo dbsql.IGenericRepo[sharedentities.RuntimeSetting], defaults AccessSettings) IAccessSettingsService {
	return &accessSettingsService{repo: repo, defaults: normalizeAccessSettings(defaults)}
}

// Get returns the live settings, seeding them from config.json on first call.
func (s *accessSettingsService) Get(ctx context.Context) (AccessSettings, error) {
	// GetByUnique against the "key" ukey, which RuntimeSetting genuinely declares. Passing a key
	// group that matches no ukey is the bug that once made everyone a superadmin elsewhere in this
	// suite: it falls through to an unfiltered select and returns the FIRST ROW IN THE TABLE.
	row, err := s.repo.GetByUnique(ctx, "", "key", accessSettingsKey)
	if err != nil {
		if isNotFound(err) {
			return s.seed(ctx)
		}
		return AccessSettings{}, fmt.Errorf("read access settings: %w", err)
	}
	if row == nil {
		return s.seed(ctx)
	}

	var out AccessSettings
	if strings.TrimSpace(row.Value) != "" {
		if err := json.Unmarshal([]byte(row.Value), &out); err != nil {
			return AccessSettings{}, fmt.Errorf("parse access settings: %w", err)
		}
	}
	return normalizeAccessSettings(out), nil
}

// seed writes the config-derived defaults on first boot.
func (s *accessSettingsService) seed(ctx context.Context) (AccessSettings, error) {
	if _, err := s.write(ctx, s.defaults, 0); err != nil {
		return AccessSettings{}, err
	}
	return s.defaults, nil
}

// Save validates and persists. A bad edit is refused rather than stored, because these values are
// read at boot: a saved-but-invalid timezone would turn into a controller that will not start.
func (s *accessSettingsService) Save(ctx context.Context, in AccessSettings, actor int64) (AccessSettings, error) {
	in = normalizeAccessSettings(in)

	// Carry keys forward BEFORE validating. Order is load-bearing: the API redacts SCBK on read, so
	// a UI that round-trips settings sends it back empty. Validating first would then refuse the
	// save with "requires an encrypted session but has no security key" — meaning an operator could
	// never rename an encrypted reader, or change anything else on that screen, without re-typing
	// every site key from memory.
	//
	// Carrying them forward also stops the far worse outcome the redaction otherwise causes:
	// pressing Save with no edits at all would WIPE every key and drop every door to cleartext.
	current, err := s.Get(ctx)
	if err == nil {
		in = carryForwardKeys(current, in)
	}

	if err := validateAccessSettings(in); err != nil {
		return AccessSettings{}, err
	}

	return s.write(ctx, in, actor)
}

func (s *accessSettingsService) write(ctx context.Context, in AccessSettings, actor int64) (AccessSettings, error) {
	payload, err := json.Marshal(rawSettings(in))
	if err != nil {
		return AccessSettings{}, err
	}
	now := time.Now().UTC().Unix()

	existing, err := s.repo.GetByUnique(ctx, "", "key", accessSettingsKey)
	if err != nil && !isNotFound(err) {
		return AccessSettings{}, fmt.Errorf("read access settings: %w", err)
	}
	if existing != nil {
		existing.Value = string(payload)
		existing.UpdatedBy, existing.UpdatedAt = actor, now
		if _, err := s.repo.UpdateById(ctx, "", *existing); err != nil {
			return AccessSettings{}, fmt.Errorf("save access settings: %w", err)
		}
		return in, nil
	}

	if _, err := s.repo.Create(ctx, "", sharedentities.RuntimeSetting{
		Key: accessSettingsKey, Value: string(payload),
		CreatedBy: actor, CreatedAt: now, UpdatedBy: actor, UpdatedAt: now,
	}); err != nil {
		return AccessSettings{}, fmt.Errorf("create access settings: %w", err)
	}
	return in, nil
}

// Reset restores the config.json seed. It is the recovery path for a settings edit that made the
// controller unable to start — without it, a bad timezone would need a database edit.
func (s *accessSettingsService) Reset(ctx context.Context, actor int64) (AccessSettings, error) {
	return s.write(ctx, s.defaults, actor)
}

// rawSettings is the persisted form: the real struct, with keys intact.
//
// It exists because ReaderSettings.MarshalJSON redacts the key for API responses, and persisting
// through that same marshaller would write the redacted form to the database — destroying every
// site key on the first save.
type rawReader struct {
	Address              int    `json:"address"`
	SCBK                 string `json:"scbk"`
	RequireSecureChannel bool   `json:"requireSecureChannel"`
	Label                string `json:"label"`
}

type rawBus struct {
	Port               string      `json:"port"`
	SlotMillis         int         `json:"slotMillis"`
	ReplyTimeoutMillis int         `json:"replyTimeoutMillis"`
	Readers            []rawReader `json:"readers"`
}

type rawAccess struct {
	Timezone         string   `json:"timezone"`
	TickSeconds      int      `json:"tickSeconds"`
	PINWindowSeconds int      `json:"pinWindowSeconds"`
	Offline          bool     `json:"offline"`
	Buses            []rawBus `json:"buses"`
}

func rawSettings(s AccessSettings) rawAccess {
	out := rawAccess{
		Timezone: s.Timezone, TickSeconds: s.TickSeconds,
		PINWindowSeconds: s.PINWindowSeconds, Offline: s.Offline,
	}
	for _, b := range s.Buses {
		rb := rawBus{Port: b.Port, SlotMillis: b.SlotMillis, ReplyTimeoutMillis: b.ReplyTimeoutMillis}
		for _, r := range b.Readers {
			rb.Readers = append(rb.Readers, rawReader{
				Address: r.Address, SCBK: r.SCBK,
				RequireSecureChannel: r.RequireSecureChannel, Label: r.Label,
			})
		}
		out.Buses = append(out.Buses, rb)
	}
	return out
}

// carryForwardKeys copies any SCBK the caller omitted from the stored settings.
func carryForwardKeys(current, incoming AccessSettings) AccessSettings {
	stored := map[string]string{}
	for _, b := range current.Buses {
		for _, r := range b.Readers {
			if strings.TrimSpace(r.SCBK) != "" {
				stored[readerKey(b.Port, r.Address)] = r.SCBK
			}
		}
	}
	for bi, b := range incoming.Buses {
		for ri, r := range b.Readers {
			if strings.TrimSpace(r.SCBK) == "" {
				if k, ok := stored[readerKey(b.Port, r.Address)]; ok {
					incoming.Buses[bi].Readers[ri].SCBK = k
				}
			}
		}
	}
	return incoming
}

func readerKey(port string, address int) string {
	return fmt.Sprintf("%s/%d", strings.TrimSpace(port), address)
}

// normalizeAccessSettings fills defaults. It never rejects — validation is separate, so that a
// value read back from an older database is repaired rather than fatal.
func normalizeAccessSettings(s AccessSettings) AccessSettings {
	if strings.TrimSpace(s.Timezone) == "" {
		s.Timezone = "Local"
	}
	if s.TickSeconds <= 0 {
		s.TickSeconds = 1
	}
	if s.PINWindowSeconds <= 0 {
		s.PINWindowSeconds = 15
	}
	for i := range s.Buses {
		s.Buses[i].Port = strings.TrimSpace(s.Buses[i].Port)
		if s.Buses[i].SlotMillis <= 0 {
			s.Buses[i].SlotMillis = 50
		}
		if s.Buses[i].ReplyTimeoutMillis <= 0 {
			s.Buses[i].ReplyTimeoutMillis = 200
		}
	}
	return s
}

// validateAccessSettings refuses an edit that would break the controller.
func validateAccessSettings(s AccessSettings) error {
	if _, err := time.LoadLocation(tzOrLocal(s.Timezone)); err != nil {
		return fmt.Errorf("timezone %q is not a valid IANA zone", s.Timezone)
	}
	if s.TickSeconds > 60 {
		return fmt.Errorf("tick interval %ds is too coarse; every door alarm would be that late", s.TickSeconds)
	}

	seenPort := map[string]bool{}
	for _, b := range s.Buses {
		if b.Port == "" {
			return fmt.Errorf("a bus has no port")
		}
		if seenPort[b.Port] {
			return fmt.Errorf("bus %q is listed twice", b.Port)
		}
		seenPort[b.Port] = true

		seenAddr := map[int]bool{}
		for _, r := range b.Readers {
			if r.Address < 0 || r.Address > 0x7F {
				return fmt.Errorf("bus %q: reader address %d is outside 0-127", b.Port, r.Address)
			}
			// Two readers at one address is the out-of-box collision — readers ship at address 0 —
			// and it is the single most likely mistake during onboarding. Catching it here is much
			// kinder than letting the operator discover it as a door that never responds.
			if seenAddr[r.Address] {
				return fmt.Errorf("bus %q has two readers at address %d; each reader needs its own address",
					b.Port, r.Address)
			}
			seenAddr[r.Address] = true

			key := strings.TrimSpace(r.SCBK)
			if key != "" && len(key) != 32 {
				return fmt.Errorf("bus %q reader %d: the security key must be 32 hex characters",
					b.Port, r.Address)
			}
			if r.RequireSecureChannel && key == "" {
				return fmt.Errorf("bus %q reader %d requires an encrypted session but has no security key",
					b.Port, r.Address)
			}
		}
	}
	return nil
}

func tzOrLocal(name string) string {
	if n := strings.TrimSpace(name); n != "" && n != "Local" {
		return n
	}
	return "UTC"
}

// Location resolves the configured timezone.
func (s AccessSettings) Location() (*time.Location, error) {
	name := strings.TrimSpace(s.Timezone)
	if name == "" || name == "Local" {
		return time.Local, nil
	}
	return time.LoadLocation(name)
}
