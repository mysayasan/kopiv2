package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/mysayasan/kopiv2/infra/onvif"
	"github.com/mysayasan/kopiv2/infra/safego"
)

// Relay outputs: the one place this appliance acts on the world (W3-5b).
//
// Everything else the product does is observation — it watches, records, and tells somebody.
// A relay output drives a siren, a strobe, a gate, a door strike or a light. It is the
// difference between recording an intrusion and responding to one, and it is the only code
// path here that can do something a person then has to physically undo.
//
// SO IT IS A CHOKEPOINT, deliberately, in the same shape as mypintusan's door actuation:
// every route to a relay — an operator's button, a detection rule, anything added later —
// comes through Fire(). Not because the layering demands it, but because the things that
// have to be true of an actuation (it is audited, it is rate-limited, it can always be
// undone) are things that get implemented once and forgotten in the second caller.
//
// THE RULE THAT MATTERS MOST: TURNING SOMETHING OFF IS NEVER REFUSED. The rate limit, the
// automation guard and every other check apply to switching a relay ON. A limiter that can
// block an OFF is a siren nobody can silence — and it would do so exactly when the siren is
// sounding, which is when the limiter is most likely to have been tripped. See Fire().

// Relay actions.
const (
	RelayActionPulse = "pulse"
	RelayActionOn    = "on"
	RelayActionOff   = "off"
)

// Pulse bounds. A pulse under a second is a click nobody hears; the cap is what stops a
// mistyped rule holding a gate open for a day.
const (
	relayMinPulseSeconds = 1
	relayMaxPulseSeconds = 300
	relayDefaultPulse    = 5
)

// relayMinAutomaticInterval is the shortest gap between two AUTOMATIC activations of the
// same output. It is a backstop under the per-rule cooldown, not a substitute for it: a
// siren re-triggered on every frame is a siren that never finishes its pulse and cannot be
// distinguished from one that is stuck on.
const relayMinAutomaticInterval = 10 * time.Second

// RelayView is one output as the screen needs it.
type RelayView struct {
	onvif.RelayOutput
	// HeldByUs is true while THIS appliance is responsible for switching the relay back
	// off — which is only the case for a bistable output the device refused to run as a
	// timed pulse. It is surfaced because it is the one state where a restart of this
	// process would leave the relay energised.
	HeldByUs bool `json:"heldByUs"`
	// HeldUntil is when we intend to release it (unix seconds), 0 when we are not holding.
	HeldUntil int64 `json:"heldUntil,omitempty"`
}

// RelayFireRequest is one actuation.
type RelayFireRequest struct {
	CameraId int64
	Token    string
	// Action is pulse, on or off.
	Action string
	// PulseSeconds applies to a pulse.
	PulseSeconds int
	// Reason is what gets audited and logged — a rule name, or what the operator was doing.
	Reason string
	// Automatic marks an actuation that no person asked for right now. Only automatic
	// actuations are rate-limited; an operator pressing a button twice is an operator who
	// meant it.
	Automatic bool
}

// IRelayService is the relay surface.
type IRelayService interface {
	Relays(ctx context.Context, cameraId int64) ([]RelayView, error)
	Fire(ctx context.Context, req RelayFireRequest) error
	// ReleaseAll drops every relay this appliance is holding. Called on shutdown.
	ReleaseAll(ctx context.Context)
}

// relayCameraClient is the slice of the camera service this needs.
type relayCameraClient interface {
	RelayOutputs(ctx context.Context, id uint64) ([]onvif.RelayOutput, error)
	SetRelayState(ctx context.Context, id uint64, token string, active bool) error
	SetRelayPulseMode(ctx context.Context, id uint64, token string, seconds int) error
	DisplayName(ctx context.Context, id int64) string
}

type relayService struct {
	cameras  relayCameraClient
	notifier INotificationPublisher
	audit    RelayAuditor
	now      func() time.Time

	mu sync.Mutex
	// lastFired bounds automatic actuation per output.
	lastFired map[string]time.Time
	// held records the outputs this process is responsible for switching off, and when.
	held map[string]*relayHold
}

type relayHold struct {
	cameraId int64
	token    string
	until    time.Time
	cancel   context.CancelFunc
}

// RelayAuditor records an actuation. Every one, without exception — see Fire().
type RelayAuditor func(ctx context.Context, cameraId int64, token string, action string, reason string, automatic bool, err error)

func NewRelayService(cameras relayCameraClient, notifier INotificationPublisher, audit RelayAuditor) IRelayService {
	return &relayService{
		cameras: cameras, notifier: notifier, audit: audit,
		now:       time.Now,
		lastFired: map[string]time.Time{},
		held:      map[string]*relayHold{},
	}
}

func (s *relayService) Relays(ctx context.Context, cameraId int64) ([]RelayView, error) {
	outputs, err := s.cameras.RelayOutputs(ctx, uint64(cameraId))
	if err != nil {
		return nil, err
	}
	// Never nil: an empty list is a fact ("this camera has no outputs") and nil renders as
	// a missing field a client cannot tell from an error.
	views := make([]RelayView, 0, len(outputs))
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, out := range outputs {
		view := RelayView{RelayOutput: out}
		if hold := s.held[relayKey(cameraId, out.Token)]; hold != nil {
			view.HeldByUs = true
			view.HeldUntil = hold.until.Unix()
		}
		views = append(views, view)
	}
	return views, nil
}

func (s *relayService) Fire(ctx context.Context, req RelayFireRequest) error {
	token := strings.TrimSpace(req.Token)
	if req.CameraId <= 0 || token == "" {
		return errors.New("a relay needs a camera and an output")
	}
	action := strings.ToLower(strings.TrimSpace(req.Action))
	if action == "" {
		action = RelayActionPulse
	}
	key := relayKey(req.CameraId, token)

	// ---- OFF: the path that must always work ---------------------------------------
	//
	// Before any guard, any rate limit, any lookup that could fail. Whatever state the
	// service is in and whoever last touched this output, a person must be able to switch
	// it off. Anything placed above this line becomes a way for the appliance to refuse to
	// stop a siren.
	if action == RelayActionOff {
		s.releaseHold(key)
		err := s.cameras.SetRelayState(ctx, uint64(req.CameraId), token, false)
		s.record(ctx, req, RelayActionOff, err)
		return err
	}

	if req.Automatic {
		s.mu.Lock()
		last, seen := s.lastFired[key]
		tooSoon := seen && s.now().Sub(last) < relayMinAutomaticInterval
		s.mu.Unlock()
		if tooSoon {
			// Not an error to the caller: a rule firing repeatedly is doing its job, and
			// turning that into a failing alert path would make the rule look broken.
			log.Printf("relay: cam%d %s: automatic actuation skipped (%s) — fired less than %s ago",
				req.CameraId, token, req.Reason, relayMinAutomaticInterval)
			return nil
		}
	}

	pulse := req.PulseSeconds
	if action == RelayActionPulse {
		if pulse <= 0 {
			pulse = relayDefaultPulse
		}
		if pulse < relayMinPulseSeconds || pulse > relayMaxPulseSeconds {
			return fmt.Errorf("hold the output for %d to %d seconds", relayMinPulseSeconds, relayMaxPulseSeconds)
		}
	}

	// Which relay this is decides how it has to be driven, so it is read rather than
	// assumed. A device that cannot be asked is not actuated: switching an output whose
	// mode is unknown is how a siren gets left on.
	outputs, err := s.cameras.RelayOutputs(ctx, uint64(req.CameraId))
	if err != nil {
		s.record(ctx, req, action, err)
		return err
	}
	var target *onvif.RelayOutput
	for i := range outputs {
		if outputs[i].Token == token {
			target = &outputs[i]
			break
		}
	}
	if target == nil {
		err := fmt.Errorf("this camera has no output %q", token)
		s.record(ctx, req, action, err)
		return err
	}

	if action == RelayActionOn {
		if target.Bistable {
			// Deliberately allowed, and deliberately not given a timer: "on" means on, and
			// an operator who asked for it gets an Off button next to it. A rule cannot ask
			// for this — see the rule hook, which only ever pulses.
			err := s.cameras.SetRelayState(ctx, uint64(req.CameraId), token, true)
			s.noteFired(key, err)
			s.record(ctx, req, action, err)
			return err
		}
		// A monostable output physically cannot stay on; asking for it would silently
		// produce a pulse of the device's own length instead.
		err := errors.New("this output returns to idle by itself, so it can only be pulsed")
		s.record(ctx, req, action, err)
		return err
	}

	// ---- a pulse -------------------------------------------------------------------
	//
	// THE RESPONSIBILITY FOR SWITCHING OFF GOES TO THE DEVICE WHENEVER THE DEVICE WILL
	// TAKE IT. A monostable output releases itself after its own delay, so our process
	// dying mid-pulse is harmless. A bistable one does not, and then WE are holding a
	// siren on with a timer in memory — which a restart, a crash or a power cut loses.
	// So a bistable output is first asked to become a timed one; only if the camera
	// refuses do we hold it ourselves, and then we say so on screen (RelayView.HeldByUs).
	deviceReleases := !target.Bistable && target.DelaySeconds > 0
	if !deviceReleases {
		if serr := s.cameras.SetRelayPulseMode(ctx, uint64(req.CameraId), token, pulse); serr == nil {
			deviceReleases = true
		} else {
			log.Printf("relay: cam%d %s: camera will not run a timed pulse (%v) — holding it from here",
				req.CameraId, token, serr)
		}
	}

	if err := s.cameras.SetRelayState(ctx, uint64(req.CameraId), token, true); err != nil {
		s.record(ctx, req, action, err)
		return err
	}
	s.noteFired(key, nil)
	s.record(ctx, req, action, nil)

	if !deviceReleases {
		s.hold(req.CameraId, token, time.Duration(pulse)*time.Second)
	}
	return nil
}

func (s *relayService) ReleaseAll(ctx context.Context) {
	s.mu.Lock()
	holds := make([]*relayHold, 0, len(s.held))
	for key, hold := range s.held {
		holds = append(holds, hold)
		delete(s.held, key)
		if hold.cancel != nil {
			hold.cancel()
		}
	}
	s.mu.Unlock()
	// Best effort, and on the way out: a relay this process energised must not be left
	// energised because the process is stopping. The ones the DEVICE releases need nothing
	// here, which is the whole reason the pulse prefers them.
	for _, hold := range holds {
		if err := s.cameras.SetRelayState(ctx, uint64(hold.cameraId), hold.token, false); err != nil {
			log.Printf("relay: cam%d %s: could not release on shutdown: %v", hold.cameraId, hold.token, err)
		}
	}
}

// hold energises-and-schedules: this process now owes the world an "off".
func (s *relayService) hold(cameraId int64, token string, after time.Duration) {
	key := relayKey(cameraId, token)
	s.releaseHold(key)
	ctx, cancel := context.WithCancel(context.Background())
	entry := &relayHold{cameraId: cameraId, token: token, until: s.now().Add(after), cancel: cancel}
	s.mu.Lock()
	s.held[key] = entry
	s.mu.Unlock()

	safego.Go("mymatasan.relay.release", func() {
		timer := time.NewTimer(after)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		s.mu.Lock()
		current := s.held[key]
		if current == entry {
			delete(s.held, key)
		}
		s.mu.Unlock()
		if current != entry {
			return
		}
		releaseCtx, releaseCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer releaseCancel()
		if err := s.cameras.SetRelayState(releaseCtx, uint64(cameraId), token, false); err != nil {
			// A failed release is the worst outcome this file has, so it is announced
			// rather than logged: something is energised, nothing is going to switch it
			// off, and only a person can now.
			log.Printf("relay: cam%d %s: RELEASE FAILED: %v", cameraId, token, err)
			s.announceStuck(releaseCtx, cameraId, token, err)
		}
	})
}

func (s *relayService) releaseHold(key string) {
	s.mu.Lock()
	if hold := s.held[key]; hold != nil {
		if hold.cancel != nil {
			hold.cancel()
		}
		delete(s.held, key)
	}
	s.mu.Unlock()
}

func (s *relayService) noteFired(key string, err error) {
	if err != nil {
		return
	}
	s.mu.Lock()
	s.lastFired[key] = s.now()
	s.mu.Unlock()
}

func (s *relayService) record(ctx context.Context, req RelayFireRequest, action string, err error) {
	if s.audit == nil {
		return
	}
	// EVERY actuation, including the automatic ones and including the failures. Unlike a
	// PTZ preset recall — which an operator generates by the dozen and which moves nothing
	// but a camera — a relay changes the building. "Who set the siren off at 04:12, and did
	// the camera actually do it" has to be answerable.
	s.audit(ctx, req.CameraId, req.Token, action, req.Reason, req.Automatic, err)
}

func (s *relayService) announceStuck(ctx context.Context, cameraId int64, token string, cause error) {
	if s.notifier == nil {
		return
	}
	name := s.cameras.DisplayName(ctx, cameraId)
	if strings.TrimSpace(name) == "" {
		name = fmt.Sprintf("camera %d", cameraId)
	}
	s.notifier.Publish(ctx, relayStuckNotification(cameraId, name, token, cause))
}

func relayKey(cameraId int64, token string) string {
	return fmt.Sprintf("%d:%s", cameraId, token)
}

// RelayRule is a detection rule's "switch something on when this fires" setting.
type RelayRule struct {
	// CameraId is which camera's output. 0 means the rule's own camera.
	CameraId int64 `json:"cameraId"`
	// Token is the output to pulse.
	Token string `json:"token"`
	// PulseSeconds is how long to hold it.
	PulseSeconds int `json:"pulseSeconds"`
}

// ParseRuleRelay extracts the relay action from a rule's ruleConfig JSON, or nil.
//
// It rides in ruleConfig beside the destinations and the PTZ recall, for the same reasons:
// it is what happens BECAUSE the rule fired, it is edited with the rule, and it costs no
// migration on an appliance already in the field.
func ParseRuleRelay(ruleConfig string) *RelayRule {
	if strings.TrimSpace(ruleConfig) == "" {
		return nil
	}
	var parsed struct {
		Relay *RelayRule `json:"relay"`
	}
	if err := json.Unmarshal([]byte(ruleConfig), &parsed); err != nil {
		return nil
	}
	if parsed.Relay == nil || strings.TrimSpace(parsed.Relay.Token) == "" {
		return nil
	}
	return parsed.Relay
}

// RelayFirer is the slice of IRelayService the alert paths need.
type RelayFirer interface {
	Fire(ctx context.Context, req RelayFireRequest) error
}

// ApplyRuleRelay pulses an output because a rule fired.
//
// ONE implementation, called from both paths that raise an alert for a rule — the vision
// monitor and the manual create-alert API — for the same reason ApplyRulePTZRecall is:
// otherwise "what happens when this rule fires" has two answers depending on which code
// raised it, and the rule editor's Test button proves nothing about the half of the rule
// that sounds a siren.
//
// ALWAYS A PULSE, never an "on". A rule that could latch an output would be a rule that can
// leave a siren sounding until somebody finds the screen it is switched off from — and the
// rule that does it is, by construction, the one firing at 4am with nobody watching.
func ApplyRuleRelay(ctx context.Context, relays RelayFirer, ruleConfig string, ruleCameraId int64, ruleName string) error {
	if relays == nil {
		return nil
	}
	rule := ParseRuleRelay(ruleConfig)
	if rule == nil {
		return nil
	}
	target := rule.CameraId
	if target <= 0 {
		target = ruleCameraId
	}
	return relays.Fire(ctx, RelayFireRequest{
		CameraId:     target,
		Token:        rule.Token,
		Action:       RelayActionPulse,
		PulseSeconds: rule.PulseSeconds,
		Reason:       ruleName,
		Automatic:    true,
	})
}
