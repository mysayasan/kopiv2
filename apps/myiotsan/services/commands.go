package services

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mysayasan/kopiv2/apps/myiotsan/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

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
	commands   dbsql.IGenericRepo[entities.DeviceCommand]
	profileCmd dbsql.IGenericRepo[entities.ProfileCommand]
	attrs      dbsql.IGenericRepo[entities.DeviceAttribute]
	devices    *DeviceService
	publish    func(topic string, payload []byte, retain bool, qos byte) error
	audit      func(ctx context.Context, msg string, data map[string]any)
	logf       func(format string, args ...any)

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
	// desiredTTL is how long a desired state stays actionable. A month-old "unlock" must not be
	// applied to a door controller that finally reconnects — see entities.DeviceAttribute.
	desiredTTL = 5 * time.Minute
)

func NewCommandService(
	db dbsql.IDbCrud,
	devices *DeviceService,
	publish func(topic string, payload []byte, retain bool, qos byte) error,
	audit func(ctx context.Context, msg string, data map[string]any),
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
		audit:      audit,
		logf:       logf,
		lastSent:   map[int64]time.Time{},
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

// validateValue enforces the profile's bounds.
func validateValue(decl *entities.ProfileCommand, v float64) error {
	switch strings.ToLower(strings.TrimSpace(decl.Kind)) {
	case "switch":
		if v != 0 && v != 1 {
			return fmt.Errorf("a switch takes 0 or 1, not %s", trimNum(v))
		}
	case "setpoint":
		// A zero Min AND Max means the profile declared no bounds. That is treated as a REFUSAL
		// rather than as "anything goes": an unbounded setpoint on a physical device is not a
		// configuration, it is an omission, and the safe reading of an omission is no.
		if decl.Min == 0 && decl.Max == 0 {
			return fmt.Errorf("this setpoint declares no safe range, so no value can be accepted — set its min and max on the device type")
		}
		if v < decl.Min || v > decl.Max {
			return fmt.Errorf("%s is outside the safe range %s..%s for this command",
				trimNum(v), trimNum(decl.Min), trimNum(decl.Max))
		}
	}
	return nil
}

// renderPayload builds the message body. {value} is substituted; a template that does not
// mention it sends itself verbatim (a device whose command topic IS the instruction).
func renderPayload(decl *entities.ProfileCommand, v float64) string {
	tpl := strings.TrimSpace(decl.PayloadTemplate)
	if tpl == "" {
		return trimNum(v)
	}
	return strings.ReplaceAll(tpl, "{value}", trimNum(v))
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
		s.audit(ctx, fmt.Sprintf("UNCONFIRMED: %s on %q was not confirmed by the device", c.Name, c.DeviceName),
			map[string]any{"deviceId": c.DeviceId, "command": c.Name, "commandId": c.Id})
		s.logf("command %d unconfirmed after %s — failed, NOT retried", c.Id, confirmTimeout)
	}
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
