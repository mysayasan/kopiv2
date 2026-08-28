package services

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mysayasan/kopiv2/apps/myiotsan/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
	iotmqtt "github.com/mysayasan/kopiv2/infra/iot/mqtt"
	"golang.org/x/crypto/bcrypt"
)

// DeviceService is the inventory, and — because a device's inventory record IS its credential
// record — it is also the broker's authenticator. A device that is not in this table cannot
// connect. There is no second credential store to drift out of sync, and deleting a device
// really does revoke it.
type DeviceService struct {
	repo dbsql.IGenericRepo[entities.IotDevice]
	logf func(format string, args ...any)
	// enroll, when set, is consulted for an UNKNOWN client id: if an enrollment window is open
	// and the client presents its key, the client is admitted as quarantined. Set after
	// construction because the enrollment service needs the profile service, which needs the db.
	enroll *Enrollment

	// lastSeen throttles the LastSeenAt write. Every publish proves the device is alive, but
	// writing that to the database on every publish would put a row UPDATE on the hot ingest
	// path — the exact cost the deadband exists to avoid. The offline detector needs
	// resolution in minutes, not milliseconds, so a bounded write is plenty.
	mu       sync.Mutex
	lastSeen map[int64]int64 // deviceId -> unix seconds last WRITTEN
}

// lastSeenWriteInterval bounds how often a device's liveness is persisted.
const lastSeenWriteInterval = 30 * time.Second

func NewDeviceService(db dbsql.IDbCrud, logf func(string, ...any)) *DeviceService {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &DeviceService{
		repo:     dbsql.NewGenericRepo[entities.IotDevice](db),
		logf:     logf,
		lastSeen: map[int64]int64{},
	}
}

var (
	// ErrDeviceKeyTaken is returned when a device key is already in use. The key is the
	// device's wire identity, so a duplicate would let one device impersonate another.
	ErrDeviceKeyTaken = errors.New("that device key is already in use")
	// ErrDeviceNotFound is returned for an unknown device.
	ErrDeviceNotFound = errors.New("device not found")
)

// CreateDeviceRequest is the body for provisioning a device.
type CreateDeviceRequest struct {
	Name      string `json:"name"`
	DeviceKey string `json:"deviceKey"`
	Protocol  string `json:"protocol"`
	ProfileId int64  `json:"profileId"`
	// Password is the device's broker credential. When empty, one is GENERATED and returned
	// exactly once — the same reasoning as the app's own bootstrap admin: a shipped or
	// defaulted device password is a fleet-wide backdoor.
	Password         string `json:"password"`
	Tag              string `json:"tag"`
	Location         string `json:"location"`
	Vendor           string `json:"vendor"`
	Model            string `json:"model"`
	Enabled          bool   `json:"enabled"`
	ActuationEnabled bool   `json:"actuationEnabled"`
	// Endpoint and Unit address a POLLED (Modbus) device: "host:port" and the unit/slave id.
	// Empty/zero for an MQTT device, which is reached by its DeviceKey over the broker instead.
	Endpoint string `json:"endpoint"`
	Unit     int    `json:"unit"`
	// Transport + serial line settings for a Modbus device (see entities.IotDevice).
	Transport string `json:"transport"`
	Baud      int    `json:"baud"`
	Parity    string `json:"parity"`
	DataBits  int    `json:"dataBits"`
	StopBits  int    `json:"stopBits"`
}

// UpdateDeviceRequest is the body for editing a device. Password is absent by design: rotating
// a credential is its own endpoint, so an ordinary edit cannot silently change one.
type UpdateDeviceRequest struct {
	Name             string `json:"name"`
	ProfileId        int64  `json:"profileId"`
	Tag              string `json:"tag"`
	Location         string `json:"location"`
	Vendor           string `json:"vendor"`
	Model            string `json:"model"`
	Enabled          bool   `json:"enabled"`
	ActuationEnabled bool   `json:"actuationEnabled"`
	Endpoint         string `json:"endpoint"`
	Unit             int    `json:"unit"`
	Transport        string `json:"transport"`
	Baud             int    `json:"baud"`
	Parity           string `json:"parity"`
	DataBits         int    `json:"dataBits"`
	StopBits         int    `json:"stopBits"`
}

// ProvisionedDevice is a created device plus the credential to show the installer once.
type ProvisionedDevice struct {
	Device *entities.IotDevice `json:"device"`
	// Password is populated ONLY on create (or an explicit rotate), and only in that response.
	// It is never stored in the clear and can never be read back.
	Password string `json:"password,omitempty"`
}

func (s *DeviceService) List(ctx context.Context, limit, offset uint64) ([]*entities.IotDevice, uint64, error) {
	if limit == 0 {
		limit = 200
	}
	return s.repo.Get(ctx, "", limit, offset, nil,
		[]sqldataenums.Sorter{{FieldName: "Name", Sort: sqldataenums.ASC}})
}

func (s *DeviceService) GetById(ctx context.Context, id int64) (*entities.IotDevice, error) {
	dev, err := s.repo.GetById(ctx, "", uint64(id))
	if err != nil {
		if isNoResultErr(err) {
			return nil, ErrDeviceNotFound
		}
		return nil, err
	}
	if dev == nil {
		return nil, ErrDeviceNotFound
	}
	return dev, nil
}

// GetByKey resolves a device by its wire identity.
func (s *DeviceService) GetByKey(ctx context.Context, key string) (*entities.IotDevice, error) {
	dev, err := s.repo.GetByUnique(ctx, "", "device_key", strings.TrimSpace(key))
	if err != nil {
		if isNoResultErr(err) {
			return nil, ErrDeviceNotFound
		}
		return nil, err
	}
	if dev == nil {
		return nil, ErrDeviceNotFound
	}
	return dev, nil
}

func (s *DeviceService) Create(ctx context.Context, req CreateDeviceRequest, actor int64) (*ProvisionedDevice, error) {
	key := strings.TrimSpace(req.DeviceKey)
	if key == "" {
		return nil, fmt.Errorf("a device key is required — it is how the device identifies itself")
	}
	if existing, err := s.GetByKey(ctx, key); err == nil && existing != nil {
		return nil, ErrDeviceKeyTaken
	} else if err != nil && !errors.Is(err, ErrDeviceNotFound) {
		return nil, err
	}

	password := strings.TrimSpace(req.Password)
	generated := false
	if password == "" {
		var err error
		if password, err = generatePassword(); err != nil {
			return nil, fmt.Errorf("generate device password: %w", err)
		}
		generated = true
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("hash device password: %w", err)
	}

	protocol := strings.TrimSpace(strings.ToLower(req.Protocol))
	if protocol == "" {
		protocol = "mqtt"
	}
	if !supportedProtocols[protocol] {
		return nil, fmt.Errorf("unsupported protocol %q: a device reaches this hub over %s",
			req.Protocol, supportedProtocolList())
	}
	now := time.Now().Unix()
	dev := entities.IotDevice{
		Name:             strings.TrimSpace(req.Name),
		DeviceKey:        key,
		Protocol:         protocol,
		ProfileId:        req.ProfileId,
		PasswordHash:     string(hash),
		Tag:              strings.TrimSpace(req.Tag),
		Location:         strings.TrimSpace(req.Location),
		Vendor:           strings.TrimSpace(req.Vendor),
		Model:            strings.TrimSpace(req.Model),
		Enabled:          req.Enabled,
		ActuationEnabled: req.ActuationEnabled,
		Endpoint:         strings.TrimSpace(req.Endpoint),
		Unit:             req.Unit,
		Transport:        strings.TrimSpace(req.Transport),
		Baud:             req.Baud,
		Parity:           strings.TrimSpace(req.Parity),
		DataBits:         req.DataBits,
		StopBits:         req.StopBits,
		Health:           "unknown",
		CreatedBy:        actor,
		CreatedAt:        now,
		UpdatedBy:        actor,
		UpdatedAt:        now,
	}
	if dev.Name == "" {
		dev.Name = key
	}
	id, err := s.repo.Create(ctx, "", dev)
	if err != nil {
		return nil, err
	}
	dev.Id = int64(id)
	dev.PasswordHash = ""

	out := &ProvisionedDevice{Device: &dev}
	// Return the credential exactly once. Only when we generated it — echoing back one the
	// caller already typed just puts it in another log.
	if generated {
		out.Password = password
	}
	return out, nil
}

func (s *DeviceService) Update(ctx context.Context, id int64, req UpdateDeviceRequest, actor int64) (*entities.IotDevice, error) {
	dev, err := s.GetById(ctx, id)
	if err != nil {
		return nil, err
	}
	dev.Name = strings.TrimSpace(req.Name)
	dev.ProfileId = req.ProfileId
	dev.Tag = strings.TrimSpace(req.Tag)
	dev.Location = strings.TrimSpace(req.Location)
	dev.Vendor = strings.TrimSpace(req.Vendor)
	dev.Model = strings.TrimSpace(req.Model)
	dev.Enabled = req.Enabled
	dev.ActuationEnabled = req.ActuationEnabled
	dev.Endpoint = strings.TrimSpace(req.Endpoint)
	dev.Unit = req.Unit
	dev.Transport = strings.TrimSpace(req.Transport)
	dev.Baud = req.Baud
	dev.Parity = strings.TrimSpace(req.Parity)
	dev.DataBits = req.DataBits
	dev.StopBits = req.StopBits
	dev.UpdatedBy = actor
	dev.UpdatedAt = time.Now().Unix()
	if dev.Name == "" {
		dev.Name = dev.DeviceKey
	}
	if _, err := s.repo.UpdateById(ctx, "", *dev); err != nil {
		return nil, err
	}
	dev.PasswordHash = ""
	return dev, nil
}

// RotatePassword issues a new broker credential and returns it once.
func (s *DeviceService) RotatePassword(ctx context.Context, id int64, actor int64) (string, error) {
	dev, err := s.GetById(ctx, id)
	if err != nil {
		return "", err
	}
	password, err := generatePassword()
	if err != nil {
		return "", err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	dev.PasswordHash = string(hash)
	dev.UpdatedBy = actor
	dev.UpdatedAt = time.Now().Unix()
	if _, err := s.repo.UpdateById(ctx, "", *dev); err != nil {
		return "", err
	}
	return password, nil
}

// Delete removes a device. Its READINGS ARE LEFT IN PLACE deliberately: the history of what a
// sensor saw is evidence, and it must not evaporate because somebody decommissioned the
// hardware. Retention purges them on the normal schedule.
func (s *DeviceService) Delete(ctx context.Context, id int64) error {
	if _, err := s.GetById(ctx, id); err != nil {
		return err
	}
	_, err := s.repo.DeleteById(ctx, "", uint64(id))
	return err
}

// --- the broker's authenticator -------------------------------------------------------

// SetEnrollment wires the enrollment window in.
func (s *DeviceService) SetEnrollment(e *Enrollment) { s.enroll = e }

// AuthenticateDevice verifies a connecting client. It satisfies mqtt.Authenticator.
//
// Three outcomes:
//
//	a known, enabled device with the right password -> admitted as itself
//	an UNKNOWN client with an open window's key      -> admitted QUARANTINED (enrolling)
//	anything else                                    -> refused
//
// A disabled device is refused: the toggle has to mean something at the wire, or "disabled" is
// just a label in a table. And a disabled device does NOT fall through to enrollment — an
// admin who switched a device off must not have it walk back in through the side door.
func (s *DeviceService) AuthenticateDevice(ctx context.Context, clientId, password string) (iotmqtt.Principal, bool) {
	dev, err := s.GetByKey(ctx, clientId)
	if err != nil && !errors.Is(err, ErrDeviceNotFound) {
		// The credential could not be CHECKED — the database was unreachable. Refuse (fail
		// closed), but say so distinctly: telling an operator "bad password" when the truth is
		// "I could not look it up" sends them to debug the device instead of the appliance.
		s.logf("device auth: could not look up %q: %v", clientId, err)
		return iotmqtt.Principal{}, false
	}

	if dev == nil {
		// Unknown. The only way in is an open enrollment window, and what it buys is quarantine,
		// not trust: see Enrollment.
		if s.enroll != nil && s.enroll.VerifyKey(password) {
			return iotmqtt.Principal{Enrolling: true}, true
		}
		return iotmqtt.Principal{}, false
	}

	if !dev.Enabled {
		return iotmqtt.Principal{}, false
	}
	if bcrypt.CompareHashAndPassword([]byte(dev.PasswordHash), []byte(password)) != nil {
		return iotmqtt.Principal{}, false
	}
	return iotmqtt.Principal{DeviceId: dev.Id}, true
}

// AuthorizeTopic confines a client to its own topics. It satisfies mqtt.Authenticator.
//
// The rule is that the client's own key must appear in the topic. Without it, one compromised
// sensor could publish readings on behalf of every other sensor in the building — forging a
// "no smoke detected" for a device that is, in fact, on fire.
//
// It applies to an ENROLLING client exactly as it does to an adopted one. A device announcing
// itself may only speak on its own topic, so it cannot pollute another device's stream even as
// a candidate.
func (s *DeviceService) AuthorizeTopic(ctx context.Context, p iotmqtt.Principal, clientId, topic string, write bool) bool {
	key := strings.TrimSpace(clientId)
	if key == "" {
		return false
	}
	return strings.Contains(topic, key)
}

// TouchSeen records that a device is alive. It is called on EVERY publish and is therefore on
// the hot path, so the database write is throttled — see lastSeenWriteInterval. The offline
// detector wants minutes of resolution, not milliseconds.
//
// This is deliberately NOT deadbanded: a device faithfully reporting an unchanged value is
// alive, and gating its liveness behind the deadband would make a stable sensor look dead.
func (s *DeviceService) TouchSeen(ctx context.Context, deviceId int64, nowSec int64) {
	s.mu.Lock()
	last := s.lastSeen[deviceId]
	if nowSec-last < int64(lastSeenWriteInterval.Seconds()) {
		s.mu.Unlock()
		return
	}
	s.lastSeen[deviceId] = nowSec
	s.mu.Unlock()

	dev, err := s.GetById(ctx, deviceId)
	if err != nil || dev == nil {
		return
	}
	dev.LastSeenAt = nowSec
	dev.Health = "online"
	_, _ = s.repo.UpdateById(ctx, "", *dev)
}

// MarkHealth sets a device's reachability, used by the offline sweep.
func (s *DeviceService) MarkHealth(ctx context.Context, deviceId int64, health string) error {
	dev, err := s.GetById(ctx, deviceId)
	if err != nil {
		return err
	}
	if dev.Health == health {
		return nil
	}
	dev.Health = health
	_, err = s.repo.UpdateById(ctx, "", *dev)
	return err
}

// supportedProtocols is how a device can actually reach this hub, and it is a CLOSED SET on
// purpose.
//
// `Protocol` is read by no code anywhere: the broker admits a device on its key and password,
// and the Modbus poller decides to poll from its PROFILE's transport. So the field is a label —
// which is exactly why it needs a guard. Nothing downstream would ever object to a value with no
// transport behind it, and a device created with one is provisioned, enabled, admitted to the
// broker if it knows its password, and permanently silent: no route accepts its data, no error
// is raised anywhere, and `UpdateDeviceRequest` carries no Protocol field, so the mistake cannot
// even be corrected afterwards. The Add-device form offered exactly such a value ("HTTP (the
// device posts)") against an app that has never had an HTTP ingest route.
//
// Refusing at creation is the fail-closed half; the form no longer offering it is the other.
// Existing rows are untouched — Update never writes this field — so an install that already
// carries such a device keeps it, visible and correctable by deleting and re-adding.
var supportedProtocols = map[string]bool{
	// The embedded MQTT broker: the device dials in and publishes.
	"mqtt": true,
	// A polled device: the hub dials OUT to it. Which registers, and whether it is polled at
	// all, comes from the profile's transport — this only records what kind of thing it is.
	"modbus": true,
}

func supportedProtocolList() string {
	names := make([]string, 0, len(supportedProtocols))
	for name := range supportedProtocols {
		names = append(names, name)
	}
	sort.Strings(names)
	return strings.Join(names, " or ")
}

// generatePassword mints a device credential with real entropy.
func generatePassword() (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// isNoResultErr reports the repo's "row not present" sentinel, which it signals by message
// rather than by a typed error.
//
// It also swallows the repo's "total affected: 0" complaint, which it raises as an ERROR when a
// DELETE matches nothing. Deleting nothing is not a failure — it is the normal case for a
// delete-then-insert against an empty table, which is exactly what seeding the profile catalog
// on a fresh install does. Treating it as fatal panicked the app on its very first boot.
func isNoResultErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "no result found") || strings.Contains(msg, "total affected: 0")
}
