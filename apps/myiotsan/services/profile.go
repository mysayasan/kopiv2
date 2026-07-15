package services

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mysayasan/kopiv2/apps/myiotsan/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// ProfileService owns device types and the datapoints they report.
//
// This is the abstraction mymatasan does not have, and it is the difference between a product
// and a demo: without it, onboarding a hundred identical door sensors means configuring a
// hundred devices by hand. With it, the hundredth device is a name and a profile.
type ProfileService struct {
	profiles dbsql.IGenericRepo[entities.DeviceProfile]
	keys     dbsql.IGenericRepo[entities.TelemetryKey]
	commands dbsql.IGenericRepo[entities.ProfileCommand]
}

func NewProfileService(db dbsql.IDbCrud) *ProfileService {
	return &ProfileService{
		profiles: dbsql.NewGenericRepo[entities.DeviceProfile](db),
		keys:     dbsql.NewGenericRepo[entities.TelemetryKey](db),
		commands: dbsql.NewGenericRepo[entities.ProfileCommand](db),
	}
}

// ErrProfileBuiltin is returned when a shipped profile is deleted. Builtins can be used and
// copied but not removed, so a site cannot break its own onboarding by tidying up.
var ErrProfileBuiltin = errors.New("a built-in profile cannot be deleted; copy it and edit the copy")

// ProfileDetail is a profile with the datapoints it declares — and the commands it accepts.
//
// A device can be told to do exactly what is in Commands and nothing else. There is no generic
// "publish this payload to that topic" endpoint anywhere in this app, which would be a remote
// shell for the building's electrics.
type ProfileDetail struct {
	Profile  *entities.DeviceProfile    `json:"profile"`
	Keys     []*entities.TelemetryKey   `json:"keys"`
	Commands []*entities.ProfileCommand `json:"commands"`
}

func (s *ProfileService) List(ctx context.Context) ([]*entities.DeviceProfile, error) {
	rows, _, err := s.profiles.Get(ctx, "", 500, 0, nil,
		[]sqldataenums.Sorter{{FieldName: "Name", Sort: sqldataenums.ASC}})
	return rows, err
}

func (s *ProfileService) Detail(ctx context.Context, id int64) (*ProfileDetail, error) {
	profile, err := s.profiles.GetById(ctx, "", uint64(id))
	if err != nil || profile == nil {
		return nil, fmt.Errorf("profile not found")
	}
	keys, err := s.KeysFor(ctx, id)
	if err != nil {
		return nil, err
	}
	cmds, _, err := s.commands.Get(ctx, "", 100, 0,
		[]sqldataenums.Filter{{FieldName: "ProfileId", Compare: sqldataenums.Equal, Value: id}},
		[]sqldataenums.Sorter{{FieldName: "Name", Sort: sqldataenums.ASC}})
	if err != nil && !isNoResultErr(err) {
		return nil, err
	}
	return &ProfileDetail{Profile: profile, Keys: keys, Commands: cmds}, nil
}

// KeysFor returns a profile's telemetry keys. This is on the ingest path (every payload needs
// its bindings) and is therefore cached by the ingest pipeline rather than read per message.
func (s *ProfileService) KeysFor(ctx context.Context, profileId int64) ([]*entities.TelemetryKey, error) {
	rows, _, err := s.keys.Get(ctx, "", 200, 0,
		[]sqldataenums.Filter{{FieldName: "ProfileId", Compare: sqldataenums.Equal, Value: profileId}},
		[]sqldataenums.Sorter{{FieldName: "Key", Sort: sqldataenums.ASC}})
	if err != nil && isNoResultErr(err) {
		return nil, nil
	}
	return rows, err
}

// SaveProfileRequest creates or replaces a profile and its keys in one call. Keys are replaced
// wholesale rather than diffed: a profile is a small declarative document, and an edit that
// half-applies is worse than one that replaces.
type SaveProfileRequest struct {
	Slug          string               `json:"slug"`
	Name          string               `json:"name"`
	Vendor        string               `json:"vendor"`
	Description   string               `json:"description"`
	TopicTemplate string               `json:"topicTemplate"`
	PayloadFormat string               `json:"payloadFormat"`
	Transport     string               `json:"transport"`
	ModbusMode    string               `json:"modbusMode"`
	ModbusBase    int                  `json:"modbusBase"`
	PollSeconds   int                  `json:"pollSeconds"`
	Keys          []SaveTelemetryKey   `json:"keys"`
	Commands      []SaveProfileCommand `json:"commands"`
}

// SaveProfileCommand declares something a device of this type can be TOLD to do — and the
// bounds within which it may be told to do it. Min/Max on a setpoint are a safety property,
// enforced server-side when a command is issued.
type SaveProfileCommand struct {
	Name            string  `json:"name"`
	Label           string  `json:"label"`
	Kind            string  `json:"kind"`
	TopicTemplate   string  `json:"topicTemplate"`
	PayloadTemplate string  `json:"payloadTemplate"`
	Min             float64 `json:"min"`
	Max             float64 `json:"max"`
	// Options enumerates a "mode" command's allowed values (JSON [{value,label}]); empty otherwise.
	Options string `json:"options"`
	// ConfirmKey is the telemetry key the device reports the resulting state on. Without it a
	// command can only ever be "sent", never "confirmed" — and "sent" is not "it happened".
	ConfirmKey string `json:"confirmKey"`
}

// SaveTelemetryKey declares one datapoint.
type SaveTelemetryKey struct {
	Key              string  `json:"key"`
	Label            string  `json:"label"`
	Unit             string  `json:"unit"`
	DataType         string  `json:"dataType"`
	JsonPath         string  `json:"jsonPath"`
	Deadband         float64 `json:"deadband"`
	HeartbeatSeconds int     `json:"heartbeatSeconds"`
	Min              float64 `json:"min"`
	Max              float64 `json:"max"`
	// Modbus binding (register-map profiles only); see entities.TelemetryKey.
	Register    int     `json:"register"`
	RegKind     string  `json:"regKind"`
	ScaleFactor float64 `json:"scaleFactor"`
	WordSwap    bool    `json:"wordSwap"`
}

func (s *ProfileService) Create(ctx context.Context, req SaveProfileRequest, actor int64) (*ProfileDetail, error) {
	slug := strings.TrimSpace(req.Slug)
	if slug == "" {
		return nil, fmt.Errorf("a profile slug is required")
	}
	now := time.Now().Unix()
	p := entities.DeviceProfile{
		Slug:          slug,
		Name:          strings.TrimSpace(req.Name),
		Vendor:        strings.TrimSpace(req.Vendor),
		Description:   strings.TrimSpace(req.Description),
		TopicTemplate: strings.TrimSpace(req.TopicTemplate),
		PayloadFormat: defaultString(req.PayloadFormat, "json"),
		Transport:     strings.TrimSpace(req.Transport),
		ModbusMode:    strings.TrimSpace(req.ModbusMode),
		ModbusBase:    req.ModbusBase,
		PollSeconds:   req.PollSeconds,
		Builtin:       false,
		CreatedBy:     actor,
		CreatedAt:     now,
		UpdatedBy:     actor,
		UpdatedAt:     now,
	}
	if p.Name == "" {
		p.Name = slug
	}
	id, err := s.profiles.Create(ctx, "", p)
	if err != nil {
		return nil, err
	}
	p.Id = int64(id)
	if err := s.replaceKeys(ctx, p.Id, req.Keys, actor); err != nil {
		return nil, err
	}
	if err := s.replaceCommands(ctx, p.Id, req.Commands, actor); err != nil {
		return nil, err
	}
	return s.Detail(ctx, p.Id)
}

func (s *ProfileService) Update(ctx context.Context, id int64, req SaveProfileRequest, actor int64) (*ProfileDetail, error) {
	existing, err := s.profiles.GetById(ctx, "", uint64(id))
	if err != nil || existing == nil {
		return nil, fmt.Errorf("profile not found")
	}
	existing.Name = strings.TrimSpace(req.Name)
	existing.Vendor = strings.TrimSpace(req.Vendor)
	existing.Description = strings.TrimSpace(req.Description)
	existing.TopicTemplate = strings.TrimSpace(req.TopicTemplate)
	existing.PayloadFormat = defaultString(req.PayloadFormat, "json")
	existing.Transport = strings.TrimSpace(req.Transport)
	existing.ModbusMode = strings.TrimSpace(req.ModbusMode)
	existing.ModbusBase = req.ModbusBase
	existing.PollSeconds = req.PollSeconds
	existing.UpdatedBy = actor
	existing.UpdatedAt = time.Now().Unix()
	if _, err := s.profiles.UpdateById(ctx, "", *existing); err != nil {
		return nil, err
	}
	if err := s.replaceKeys(ctx, id, req.Keys, actor); err != nil {
		return nil, err
	}
	if err := s.replaceCommands(ctx, id, req.Commands, actor); err != nil {
		return nil, err
	}
	return s.Detail(ctx, id)
}

func (s *ProfileService) Delete(ctx context.Context, id int64) error {
	existing, err := s.profiles.GetById(ctx, "", uint64(id))
	if err != nil || existing == nil {
		return fmt.Errorf("profile not found")
	}
	if existing.Builtin {
		return ErrProfileBuiltin
	}
	if _, err := s.keys.Delete(ctx, "",
		[]sqldataenums.Filter{{FieldName: "ProfileId", Compare: sqldataenums.Equal, Value: id}}); err != nil && !isNoResultErr(err) {
		return err
	}
	_, err = s.profiles.DeleteById(ctx, "", uint64(id))
	return err
}

func (s *ProfileService) replaceKeys(ctx context.Context, profileId int64, keys []SaveTelemetryKey, actor int64) error {
	if _, err := s.keys.Delete(ctx, "",
		[]sqldataenums.Filter{{FieldName: "ProfileId", Compare: sqldataenums.Equal, Value: profileId}}); err != nil && !isNoResultErr(err) {
		return err
	}
	now := time.Now().Unix()
	for _, k := range keys {
		name := strings.TrimSpace(k.Key)
		if name == "" {
			continue
		}
		row := entities.TelemetryKey{
			ProfileId:        profileId,
			Key:              name,
			Label:            defaultString(k.Label, name),
			Unit:             strings.TrimSpace(k.Unit),
			DataType:         defaultString(k.DataType, "number"),
			JsonPath:         strings.TrimSpace(k.JsonPath),
			Deadband:         k.Deadband,
			HeartbeatSeconds: k.HeartbeatSeconds,
			Min:              k.Min,
			Max:              k.Max,
			Register:         k.Register,
			RegKind:          strings.TrimSpace(k.RegKind),
			ScaleFactor:      k.ScaleFactor,
			WordSwap:         k.WordSwap,
			CreatedBy:        actor,
			CreatedAt:        now,
			UpdatedBy:        actor,
			UpdatedAt:        now,
		}
		if _, err := s.keys.Create(ctx, "", row); err != nil {
			return err
		}
	}
	return nil
}

// replaceCommands rewrites a profile's declared commands.
func (s *ProfileService) replaceCommands(ctx context.Context, profileId int64, cmds []SaveProfileCommand, actor int64) error {
	if _, err := s.commands.Delete(ctx, "",
		[]sqldataenums.Filter{{FieldName: "ProfileId", Compare: sqldataenums.Equal, Value: profileId}}); err != nil && !isNoResultErr(err) {
		return err
	}
	now := time.Now().Unix()
	for _, c := range cmds {
		name := strings.TrimSpace(c.Name)
		if name == "" {
			continue
		}
		if _, err := s.commands.Create(ctx, "", entities.ProfileCommand{
			ProfileId:       profileId,
			Name:            name,
			Label:           defaultString(c.Label, name),
			Kind:            defaultString(c.Kind, "switch"),
			TopicTemplate:   strings.TrimSpace(c.TopicTemplate),
			PayloadTemplate: strings.TrimSpace(c.PayloadTemplate),
			Min:             c.Min,
			Max:             c.Max,
			Options:         strings.TrimSpace(c.Options),
			ConfirmKey:      strings.TrimSpace(c.ConfirmKey),
			CreatedBy:       actor,
			CreatedAt:       now,
			UpdatedBy:       actor,
			UpdatedAt:       now,
		}); err != nil {
			return err
		}
	}
	return nil
}

// EnsureBuiltins seeds the shipped catalog. Existing profiles are left ALONE — a site that has
// tuned a builtin's deadbands must not have that overwritten on the next boot, which is the
// same rule the RBAC seeder follows.
func (s *ProfileService) EnsureBuiltins(ctx context.Context) error {
	for _, b := range builtinProfiles() {
		existing, err := s.profiles.GetByUnique(ctx, "", "slug", b.Slug)
		if err != nil && !isNoResultErr(err) {
			return err
		}
		if existing != nil {
			continue
		}
		now := time.Now().Unix()
		p := entities.DeviceProfile{
			Slug:          b.Slug,
			Name:          b.Name,
			Vendor:        b.Vendor,
			Description:   b.Description,
			TopicTemplate: b.TopicTemplate,
			PayloadFormat: "json",
			Transport:     b.Transport,
			ModbusMode:    b.ModbusMode,
			ModbusBase:    b.ModbusBase,
			PollSeconds:   b.PollSeconds,
			Builtin:       true,
			CreatedAt:     now,
			UpdatedAt:     now,
		}
		id, err := s.profiles.Create(ctx, "", p)
		if err != nil {
			return fmt.Errorf("seed profile %s: %w", b.Slug, err)
		}
		if err := s.replaceKeys(ctx, int64(id), b.Keys, 0); err != nil {
			return fmt.Errorf("seed profile keys for %s: %w", b.Slug, err)
		}
		if err := s.replaceCommands(ctx, int64(id), b.Commands, 0); err != nil {
			return fmt.Errorf("seed profile commands for %s: %w", b.Slug, err)
		}
	}
	return nil
}

func defaultString(v, fallback string) string {
	if s := strings.TrimSpace(v); s != "" {
		return s
	}
	return fallback
}
