package services

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mysayasan/kopiv2/apps/myiotsan/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
	"github.com/mysayasan/kopiv2/infra/iot/modbus"
	"github.com/mysayasan/kopiv2/infra/telemetry"
)

// modbusWriteFunc is the guarded-write seam (modbus.WriteConfirm in production, a fake in tests):
// write value to a holding register and confirm it by reading it back, NEVER re-issuing.
type modbusWriteFunc func(endpoint string, unit byte, reg int, value uint16, timeout time.Duration) error

// Actuation: the point where a bug stops being a wrong number on a chart and becomes a relay
// that physically fires.
//
// A camera is read-mostly. An IoT device gets WRITTEN to, and a bad write is dangerous in a way
// a bad PTZ move is not — it opens a door, trips a breaker, sets a thermostat to 200 degrees.
// So the command path is built to be hard to use by accident, and every one of these is a rule
// rather than a suggestion:
//
//	1. READ-ONLY BY DEFAULT.   A device cannot be commanded unless ActuationEnabled was
//	                           explicitly turned on for it. Adoption never sets it.
//	2. ADMIN ONLY.             Not an operator power. See services.Policy.
//	3. ONLY DECLARED COMMANDS. A device can be told to do exactly what its profile declares.
//	                           There is no "publish this payload to that topic" escape hatch,
//	                           which would be a remote shell for the building's electrics.
//	4. BOUNDS ARE SERVER-SIDE. A setpoint outside the profile's Min..Max is REFUSED here. "The
//	                           frontend validates it" is not a safety property.
//	5. RATE-LIMITED.           A relay is a physical object with a duty cycle. Something that
//	                           can chatter it a thousand times a second can destroy it.
//	6. AUDITED.                Every attempt is a row naming the actor, and a notification.
//	7. NEVER AUTO-RETRIED.     The important one — see below.
//
// WHY A TIMEOUT ENDS A COMMAND RATHER THAN RETRYING IT. Re-sending a relay write is a SECOND
// PHYSICAL ACTION. If the first one landed but its confirmation was lost, a retry fires the
// relay again — the door opens twice, the breaker cycles twice — and nothing at this layer can
// tell those two cases apart. So an unconfirmed command fails, says plainly that it was not
// confirmed, and leaves the decision to a human. Automatic retry is a convenience that can open
// a door twice, and no convenience is worth that.

// CommandService issues and tracks device commands.
type CommandService struct {
	commands    dbsql.IGenericRepo[entities.DeviceCommand]
	profileCmd  dbsql.IGenericRepo[entities.ProfileCommand]
	attrs       dbsql.IGenericRepo[entities.DeviceAttribute]
	devices     *DeviceService
	publish     func(topic string, payload []byte, retain bool, qos byte) error
	modbusWrite modbusWriteFunc
	audit       func(ctx context.Context, msg string, data map[string]any)
	metrics     telemetry.Metrics
	logf        func(format string, args ...any)

	// rate limits the physical duty cycle, per device.
	mu       sync.Mutex
	lastSent map[int64]time.Time
}

const (
	// minCommandInterval is the floor between two commands to the same device. A relay has a
	// duty cycle; something that can chatter it is something that can destroy it.
	minCommandInterval = 2 * time.Second
	// confirmTimeout is how long a device has to report the state back before the command is
	// declared unconfirmed. It is NOT a retry timer.
	confirmTimeout = 30 * time.Second
	// modbusWriteTimeout bounds the INLINE guarded Modbus write+read-back. It is short on purpose:
	// the write is synchronous (a flow's command node waits on it), a healthy device confirms a
	// setpoint in well under a second, and — like every actuation here — it is NEVER retried, so a
	// slow confirmation fails and a human decides rather than the register being written twice.
	modbusWriteTimeout = 5 * time.Second
	// desiredTTL is how long a desired state stays actionable. A month-old "unlock" must not be
	// applied to a door controller that finally reconnects — see entities.DeviceAttribute.
	desiredTTL = 5 * time.Minute
)

func NewCommandService(
	db dbsql.IDbCrud,
	devices *DeviceService,
	publish func(topic string, payload []byte, retain bool, qos byte) error,
	audit func(ctx context.Context, msg string, data map[string]any),
	metrics telemetry.Metrics,
	logf func(string, ...any),
) *CommandService {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	if audit == nil {
		audit = func(context.Context, string, map[string]any) {}
	}
	return &CommandService{
		commands:   dbsql.NewGenericRepo[entities.DeviceCommand](db),
		profileCmd: dbsql.NewGenericRepo[entities.ProfileCommand](db),
		attrs:      dbsql.NewGenericRepo[entities.DeviceAttribute](db),
		devices:    devices,
		publish:    publish,
		// The production guarded-write: dial the device, write the register, confirm by read-back.
		modbusWrite: func(endpoint string, unit byte, reg int, value uint16, timeout time.Duration) error {
			return modbus.WriteConfirm(modbus.DeviceConf{Endpoint: endpoint, Unit: unit}, reg, value, timeout)
		},
		audit:    audit,
		metrics:  metrics,
		logf:     logf,
		lastSent: map[int64]time.Time{},
	}
}

// IssueRequest asks a device to do something.
type IssueRequest struct {
	Name  string  `json:"name"`
	Value float64 `json:"value"`
}

// CommandsFor lists what a device can be told to do — which is exactly what its profile
// declares, and nothing else.
func (s *CommandService) CommandsFor(ctx context.Context, profileId int64) ([]*entities.ProfileCommand, error) {
	rows, _, err := s.profileCmd.Get(ctx, "", 100, 0,
		[]sqldataenums.Filter{{FieldName: "ProfileId", Compare: sqldataenums.Equal, Value: profileId}},
		[]sqldataenums.Sorter{{FieldName: "Name", Sort: sqldataenums.ASC}})
	if err != nil && isNoResultErr(err) {
		return nil, nil
	}
	return rows, err
}

// Issue runs every gate, then sends.
//
// It returns the recorded command whatever happens — a REFUSED command is still an audit row.
// "Somebody tried to unlock the front door at 03:00 and was refused" is exactly the kind of
// thing that must not be thrown away just because it did not succeed.
func (s *CommandService) Issue(ctx context.Context, deviceId int64, req IssueRequest, actor int64, actorName string) (*entities.DeviceCommand, error) {
	now := time.Now()
	cmd := entities.DeviceCommand{
		DeviceId:        deviceId,
		Name:            strings.TrimSpace(req.Name),
		Value:           req.Value,
		Status:          "failed",
		RequestedBy:     actor,
		RequestedByName: actorName,
		RequestedAt:     now.Unix(),
	}

	refuse := func(reason string) (*entities.DeviceCommand, error) {
		cmd.Error = reason
		if id, err := s.commands.Create(ctx, "", cmd); err == nil {
			cmd.Id = int64(id)
		}
		s.audit(ctx, fmt.Sprintf("REFUSED: %s on %q — %s", cmd.Name, cmd.DeviceName, reason),
			map[string]any{"deviceId": deviceId, "command": cmd.Name, "value": req.Value, "actor": actor})
		s.countCommand("refused")
		return &cmd, fmt.Errorf("%s", reason)
	}

	dev, err := s.devices.GetById(ctx, deviceId)
	if err != nil || dev == nil {
		return refuse("device not found")
	}
	cmd.DeviceName = dev.Name

	// GATE 1: read-only by default. The toggle is the whole point — a device does not become
	// commandable because somebody plugged it in.
	if !dev.ActuationEnabled {
		return refuse("actuation is not enabled for this device")
	}
	if !dev.Enabled {
		return refuse("the device is disabled")
	}

	// GATE 2: only what the profile declares.
	decl, err := s.declaration(ctx, dev.ProfileId, cmd.Name)
	if err != nil {
		return refuse(err.Error())
	}

	// GATE 3: bounds, server-side. A thermostat that accepts 200 degrees because a slider was
	// bypassed is a fire.
	if err := validateValue(decl, req.Value); err != nil {
		return refuse(err.Error())
	}

	// GATE 4: rate limit. A relay has a duty cycle.
	s.mu.Lock()
	last := s.lastSent[deviceId]
	if !last.IsZero() && now.Sub(last) < minCommandInterval {
		s.mu.Unlock()
		return refuse(fmt.Sprintf("too soon — wait %s between commands to the same device", minCommandInterval))
	}
	s.lastSent[deviceId] = now
	s.mu.Unlock()

	// Accepted. Record BEFORE sending: a command that was sent but never written down is a
	// physical action with no audit trail, which is the worst possible ordering.
	cmd.Status = "pending"
	cmd.Error = ""
	id, err := s.commands.Create(ctx, "", cmd)
	if err != nil {
		return nil, err
	}
	cmd.Id = int64(id)

	// SEND. Both transports arrive here having passed the identical gates above — that is the whole
	// point: a Modbus device is commanded through the same read-only-by-default, admin-only,
	// declared-bounds, rate-limited, audited, never-retried path an MQTT relay is. A Modbus command
	// WRITES a holding register and confirms by reading it back; an MQTT command PUBLISHES.
	if strings.EqualFold(strings.TrimSpace(decl.Transport), "modbus") {
		return s.sendModbus(ctx, cmd, dev, decl, req.Value, actor, now)
	}

	topic := strings.ReplaceAll(decl.TopicTemplate, "{deviceKey}", dev.DeviceKey)
	payload := renderPayload(decl, req.Value)

	if err := s.publish(topic, []byte(payload), false, 1); err != nil {
		cmd.Status = "failed"
		cmd.Error = "could not publish to the device: " + err.Error()
		_, _ = s.commands.UpdateById(ctx, "", cmd)
		s.audit(ctx, fmt.Sprintf("FAILED to send %s to %q: %v", cmd.Name, dev.Name, err),
			map[string]any{"deviceId": deviceId, "command": cmd.Name, "actor": actor})
		return &cmd, err
	}

	cmd.Status = "sent"
	cmd.SentAt = time.Now().Unix()
	_, _ = s.commands.UpdateById(ctx, "", cmd)

	// The desired half of the twin — actionable only for a bounded time. See DeviceAttribute.
	if decl.ConfirmKey != "" {
		s.setDesired(ctx, deviceId, decl.ConfirmKey, req.Value, now)
	}

	s.audit(ctx, fmt.Sprintf("SENT %s=%s to %q", cmd.Name, trimNum(req.Value), dev.Name),
		map[string]any{"deviceId": deviceId, "command": cmd.Name, "value": req.Value, "actor": actor})
	s.logf("command %d: %s=%v sent to %q on %q", cmd.Id, cmd.Name, req.Value, dev.DeviceKey, topic)
	return &cmd, nil
}

// sendModbus writes the command's holding register on the polled device and confirms it by
// read-back (modbus.WriteConfirm). It runs AFTER every gate in Issue, so it is the write half of the
// same guarded path the MQTT relay takes — nothing here re-checks a gate, and nothing here retries.
// A confirmed write is the strongest status the system has: the device reported the value back.
func (s *CommandService) sendModbus(ctx context.Context, cmd entities.DeviceCommand, dev *entities.IotDevice, decl *entities.ProfileCommand, value float64, actor int64, now time.Time) (*entities.DeviceCommand, error) {
	fail := func(reason string) (*entities.DeviceCommand, error) {
		cmd.Status = "failed"
		cmd.Error = reason
		_, _ = s.commands.UpdateById(ctx, "", cmd)
		s.audit(ctx, fmt.Sprintf("FAILED modbus %s on %q — %s", cmd.Name, dev.Name, reason),
			map[string]any{"deviceId": dev.Id, "command": cmd.Name, "value": value, "actor": actor})
		s.countCommand("failed")
		return &cmd, fmt.Errorf("%s", reason)
	}

	if strings.TrimSpace(dev.Endpoint) == "" {
		return fail("this device has no Modbus endpoint (host:port) to write to")
	}
	raw, err := encodeRegister(decl.RegKind, decl.ScaleFactor, value)
	if err != nil {
		return fail(err.Error())
	}

	cmd.SentAt = now.Unix()
	if err := s.modbusWrite(dev.Endpoint, byte(dev.Unit), decl.Register, raw, modbusWriteTimeout); err != nil {
		// A lost or unconfirmed write ENDS the command — it is never re-sent (a second register
		// write is a second physical action). The operator sees plainly that it was not confirmed.
		return fail("modbus write not confirmed: " + err.Error())
	}

	// WriteConfirm read the register back and saw the value — the device reported the state, which
	// is exactly what "confirmed" means. A Modbus command therefore confirms inline, where an MQTT
	// command has to wait for the twin's reported half.
	cmd.Status = "confirmed"
	cmd.ConfirmedAt = time.Now().Unix()
	_, _ = s.commands.UpdateById(ctx, "", cmd)
	s.countCommand("confirmed")
	s.audit(ctx, fmt.Sprintf("CONFIRMED modbus %s=%s to reg %d on %q", cmd.Name, trimNum(value), decl.Register, dev.Name),
		map[string]any{"deviceId": dev.Id, "command": cmd.Name, "value": value, "register": decl.Register, "actor": actor})
	s.logf("command %d: modbus %s=%v confirmed at reg %d on %q", cmd.Id, cmd.Name, value, decl.Register, dev.DeviceKey)
	return &cmd, nil
}

// encodeRegister turns a human value into the raw register word to write, applying the read scale in
// reverse (raw = round(value / scaleFactor)) and the register kind's range check. Only single-register
// kinds (u16/i16) are written for now — multi-register (u32/i32) writes are refused rather than
// half-written. Getting encoding wrong writes a wrong number to real hardware, so it fails closed.
func encodeRegister(kind string, scale float64, value float64) (uint16, error) {
	if scale == 0 {
		scale = 1
	}
	raw := math.Round(value / scale)
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "", "u16":
		if raw < 0 || raw > 65535 {
			return 0, fmt.Errorf("value %s is outside the u16 register range", trimNum(value))
		}
		return uint16(raw), nil
	case "i16":
		if raw < -32768 || raw > 32767 {
			return 0, fmt.Errorf("value %s is outside the i16 register range", trimNum(value))
		}
		return uint16(int16(raw)), nil
	default:
		return 0, fmt.Errorf("a Modbus command can only write a u16 or i16 register, not %q", kind)
	}
}

// declaration finds the command a profile declares, or explains that it does not.
func (s *CommandService) declaration(ctx context.Context, profileId int64, name string) (*entities.ProfileCommand, error) {
	if name == "" {
		return nil, fmt.Errorf("no command named")
	}
	cmds, err := s.CommandsFor(ctx, profileId)
	if err != nil {
		return nil, fmt.Errorf("could not read the device type's commands")
	}
	for _, c := range cmds {
		if strings.EqualFold(c.Name, name) {
			return c, nil
		}
	}
	// Deliberately specific: the refusal explains that the DEVICE TYPE does not have this
	// command, which is a configuration answer, rather than a vague "bad request".
	return nil, fmt.Errorf("this device type declares no command called %q", name)
}

// validateValue enforces the profile's bounds. Every kind the app knows has a case here, and the
// DEFAULT is a refusal: an unrecognised kind is a misconfiguration, and a physical device is the
// wrong place to guess. (Before the home-automation kinds this switch had no default, so an unknown
// kind was published UNVALIDATED — that hole is closed here.)
func validateValue(decl *entities.ProfileCommand, v float64) error {
	switch strings.ToLower(strings.TrimSpace(decl.Kind)) {
	case "switch":
		if v != 0 && v != 1 {
			return fmt.Errorf("a switch takes 0 or 1, not %s", trimNum(v))
		}
	case "dimmer", "position":
		// A percentage. Fixed 0..100 — brightness and blind travel are both proportions, and a
		// value outside that is a bug, not a brighter light.
		if v < 0 || v > 100 {
			return fmt.Errorf("%s is outside 0..100 for a %s", trimNum(v), strings.ToLower(decl.Kind))
		}
	case "setpoint", "cct":
		// A zero Min AND Max means the profile declared no bounds. That is treated as a REFUSAL
		// rather than as "anything goes": an unbounded setpoint on a physical device is not a
		// configuration, it is an omission, and the safe reading of an omission is no. (cct is a
		// setpoint in Kelvin — same bounds discipline.)
		if decl.Min == 0 && decl.Max == 0 {
			return fmt.Errorf("this %s declares no safe range, so no value can be accepted — set its min and max on the device type", strings.ToLower(decl.Kind))
		}
		if v < decl.Min || v > decl.Max {
			return fmt.Errorf("%s is outside the safe range %s..%s for this command",
				trimNum(v), trimNum(decl.Min), trimNum(decl.Max))
		}
	case "mode":
		vals, err := modeValues(decl.Options)
		if err != nil {
			return err
		}
		for _, ok := range vals {
			if v == ok {
				return nil
			}
		}
		return fmt.Errorf("%s is not one of this command's allowed modes", trimNum(v))
	case "color":
		// A colour packed as 0xRRGGBB. Must be a whole number in range; the twin still confirms it
		// as one float, so a device that reports colour back per-channel cannot be confirmed by it
		// (declare no ConfirmKey there — "sent, never confirmed" is honest).
		if v != math.Trunc(v) || v < 0 || v > 0xFFFFFF {
			return fmt.Errorf("a colour must be a whole number 0..16777215 (0xRRGGBB), not %s", trimNum(v))
		}
	default:
		return fmt.Errorf("this device type declares an unknown command kind %q", decl.Kind)
	}
	return nil
}

// modeValues parses a mode command's Options ([{"value":int,"label":string}]) to the set of allowed
// integer values. An empty or malformed Options is a refusal — a mode with no declared options can
// accept nothing, the same "omission means no" rule the unbounded setpoint follows.
func modeValues(options string) ([]float64, error) {
	options = strings.TrimSpace(options)
	if options == "" {
		return nil, fmt.Errorf("this mode command declares no options, so no value can be accepted — set its options on the device type")
	}
	var opts []struct {
		Value float64 `json:"value"`
	}
	if err := json.Unmarshal([]byte(options), &opts); err != nil || len(opts) == 0 {
		return nil, fmt.Errorf("this mode command's options are not a valid list")
	}
	out := make([]float64, len(opts))
	for i, o := range opts {
		out[i] = o.Value
	}
	return out, nil
}

// packRGB / unpackRGB carry a colour through the single-float command model. 0xRRGGBB (max
// 16,777,215) is exactly representable in a float64 mantissa, so the audit value and the twin
// equality check stay unchanged from every other kind.
func packRGB(r, g, b int) float64 { return float64((r&0xFF)<<16 | (g&0xFF)<<8 | (b & 0xFF)) }

func unpackRGB(v float64) (r, g, b int) {
	n := int(v)
	return (n >> 16) & 0xFF, (n >> 8) & 0xFF, n & 0xFF
}

// renderPayload builds the message body. {value} is substituted for every kind; a "color" command
// also substitutes {r}/{g}/{b} with the unpacked channels. A template that mentions no token sends
// itself verbatim (a device whose command topic IS the instruction).
func renderPayload(decl *entities.ProfileCommand, v float64) string {
	tpl := strings.TrimSpace(decl.PayloadTemplate)
	if tpl == "" {
		return trimNum(v)
	}
	out := strings.ReplaceAll(tpl, "{value}", trimNum(v))
	if strings.EqualFold(strings.TrimSpace(decl.Kind), "color") {
		r, g, b := unpackRGB(v)
		out = strings.ReplaceAll(out, "{r}", strconv.Itoa(r))
		out = strings.ReplaceAll(out, "{g}", strconv.Itoa(g))
		out = strings.ReplaceAll(out, "{b}", strconv.Itoa(b))
	}
	return out
}

func trimNum(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

// --- the twin ------------------------------------------------------------------------

func (s *CommandService) setDesired(ctx context.Context, deviceId int64, key string, v float64, now time.Time) {
	attr, _ := s.attrs.GetByUnique(ctx, "", "dev_key", deviceId, key)
	if attr == nil {
		_, _ = s.attrs.Create(ctx, "", entities.DeviceAttribute{
			DeviceId: deviceId, Key: key,
			Desired: v, HasDesired: true, DesiredAt: now.Unix(),
			DesiredExpiresAt: now.Add(desiredTTL).Unix(),
		})
		return
	}
	attr.Desired = v
	attr.HasDesired = true
	attr.DesiredAt = now.Unix()
	attr.DesiredExpiresAt = now.Add(desiredTTL).Unix()
	_, _ = s.attrs.UpdateById(ctx, "", *attr)
}

// OnReported is called for every reading. It updates the twin's reported half and CONFIRMS any
// command that was waiting on this key.
//
// This is what closes the loop, and it is the only thing that can. "We published a message" is
// not "the door locked" — only the device saying so is. A command that is never confirmed is
// never shown as succeeded.
func (s *CommandService) OnReported(ctx context.Context, deviceId int64, key string, value float64, nowSec int64) {
	attr, _ := s.attrs.GetByUnique(ctx, "", "dev_key", deviceId, key)
	if attr == nil {
		_, _ = s.attrs.Create(ctx, "", entities.DeviceAttribute{
			DeviceId: deviceId, Key: key,
			Reported: value, HasReported: true, ReportedAt: nowSec,
		})
		return
	}
	attr.Reported = value
	attr.HasReported = true
	attr.ReportedAt = nowSec

	// The device reports what we asked for: the desire is satisfied and is no longer pending.
	if attr.HasDesired && attr.Desired == value {
		attr.HasDesired = false
		s.confirmPending(ctx, deviceId, key, value, nowSec)
	}
	_, _ = s.attrs.UpdateById(ctx, "", *attr)
}

// confirmPending marks the sent commands on this key as confirmed.
func (s *CommandService) confirmPending(ctx context.Context, deviceId int64, key string, value float64, nowSec int64) {
	rows, _, err := s.commands.Get(ctx, "", 20, 0,
		[]sqldataenums.Filter{
			{FieldName: "DeviceId", Compare: sqldataenums.Equal, Value: deviceId},
			{FieldName: "Status", Compare: sqldataenums.Equal, Value: "sent"},
		},
		[]sqldataenums.Sorter{{FieldName: "RequestedAt", Sort: sqldataenums.DESC}})
	if err != nil {
		return
	}
	for _, c := range rows {
		if c.Value != value {
			continue
		}
		c.Status = "confirmed"
		c.ConfirmedAt = nowSec
		_, _ = s.commands.UpdateById(ctx, "", *c)
		s.countCommand("confirmed")
		s.logf("command %d confirmed by the device", c.Id)
	}
}

// SweepUnconfirmed ends commands the device never confirmed.
//
// It marks them FAILED. It does NOT resend them. Re-sending is a second physical action, and if
// the first one landed but its confirmation was lost, the retry fires the relay again — the
// door opens twice — and nothing here can distinguish that case from the first one never
// arriving. So the operator is told plainly that it was not confirmed, and THEY decide.
func (s *CommandService) SweepUnconfirmed(ctx context.Context) {
	cutoff := time.Now().Add(-confirmTimeout).Unix()
	rows, _, err := s.commands.Get(ctx, "", 200, 0,
		[]sqldataenums.Filter{
			{FieldName: "Status", Compare: sqldataenums.Equal, Value: "sent"},
			{FieldName: "SentAt", Compare: sqldataenums.LessThan, Value: cutoff},
		}, nil)
	if err != nil {
		return
	}
	for _, c := range rows {
		c.Status = "failed"
		c.Error = "the device never reported the new state — it may or may not have acted. Not retried automatically: re-sending could act twice."
		_, _ = s.commands.UpdateById(ctx, "", *c)
		s.countCommand("failed")
		s.audit(ctx, fmt.Sprintf("UNCONFIRMED: %s on %q was not confirmed by the device", c.Name, c.DeviceName),
			map[string]any{"deviceId": c.DeviceId, "command": c.Name, "commandId": c.Id})
		s.logf("command %d unconfirmed after %s — failed, NOT retried", c.Id, confirmTimeout)
	}
}

// countCommand records one command outcome. Kept off the publish path (commands are rare) so it
// can afford a labelled counter directly rather than being sampled.
func (s *CommandService) countCommand(outcome string) {
	if s.metrics == nil {
		return
	}
	s.metrics.Inc(MetricCommandsTotal, telemetry.Labels{"outcome": outcome})
}

// History lists a device's commands — the audit trail, newest first.
func (s *CommandService) History(ctx context.Context, deviceId int64, limit, offset uint64) ([]*entities.DeviceCommand, uint64, error) {
	if limit == 0 {
		limit = 100
	}
	var filters []sqldataenums.Filter
	if deviceId > 0 {
		filters = append(filters, sqldataenums.Filter{FieldName: "DeviceId", Compare: sqldataenums.Equal, Value: deviceId})
	}
	rows, total, err := s.commands.Get(ctx, "", limit, offset, filters,
		[]sqldataenums.Sorter{{FieldName: "RequestedAt", Sort: sqldataenums.DESC}})
	if err != nil && isNoResultErr(err) {
		return []*entities.DeviceCommand{}, 0, nil
	}
	return rows, total, err
}

// Twin returns a device's desired-vs-reported state.
//
// An EXPIRED desire is still shown — an operator must see that what they asked for never took
// effect. It is simply no longer actionable.
func (s *CommandService) Twin(ctx context.Context, deviceId int64) ([]*entities.DeviceAttribute, error) {
	rows, _, err := s.attrs.Get(ctx, "", 200, 0,
		[]sqldataenums.Filter{{FieldName: "DeviceId", Compare: sqldataenums.Equal, Value: deviceId}},
		[]sqldataenums.Sorter{{FieldName: "Key", Sort: sqldataenums.ASC}})
	if err != nil && isNoResultErr(err) {
		return []*entities.DeviceAttribute{}, nil
	}
	return rows, err
}
