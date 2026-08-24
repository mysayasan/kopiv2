package services

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mysayasan/kopiv2/infra/onvif"
)

// fakeRelayCamera is a camera with outputs that remembers how it was driven.
type fakeRelayCamera struct {
	mu       sync.Mutex
	outputs  []onvif.RelayOutput
	states   map[string]bool
	calls    []string
	listErr  error
	stateErr error
	// pulseModeErr models a camera that refuses to be reconfigured into a timed pulse,
	// which is what forces this appliance to hold the output itself.
	pulseModeErr error
}

func newFakeRelayCamera(outputs ...onvif.RelayOutput) *fakeRelayCamera {
	return &fakeRelayCamera{outputs: outputs, states: map[string]bool{}}
}

func (f *fakeRelayCamera) RelayOutputs(context.Context, uint64) ([]onvif.RelayOutput, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.outputs, nil
}

func (f *fakeRelayCamera) SetRelayState(_ context.Context, _ uint64, token string, active bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.stateErr != nil {
		f.calls = append(f.calls, "state-failed:"+token)
		return f.stateErr
	}
	f.states[token] = active
	f.calls = append(f.calls, map[bool]string{true: "on:", false: "off:"}[active]+token)
	return nil
}

func (f *fakeRelayCamera) SetRelayPulseMode(_ context.Context, _ uint64, token string, seconds int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.pulseModeErr != nil {
		return f.pulseModeErr
	}
	f.calls = append(f.calls, "pulse-mode:"+token)
	for i := range f.outputs {
		if f.outputs[i].Token == token {
			f.outputs[i].Bistable = false
			f.outputs[i].DelaySeconds = seconds
		}
	}
	return nil
}

func (f *fakeRelayCamera) DisplayName(context.Context, int64) string { return "Gate camera" }

func (f *fakeRelayCamera) trail() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return strings.Join(f.calls, ",")
}

func (f *fakeRelayCamera) state(token string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.states[token]
}

type auditedActuation struct {
	token     string
	action    string
	automatic bool
	failed    bool
}

func newRelayRig(t *testing.T, cam *fakeRelayCamera) (*relayService, *capturedNotifications, *[]auditedActuation, *time.Time) {
	t.Helper()
	notif := &capturedNotifications{}
	trail := &[]auditedActuation{}
	clock := time.Unix(1_700_000_000, 0).UTC()
	svc := NewRelayService(cam, notif, func(_ context.Context, _ int64, token, action, _ string, automatic bool, err error) {
		*trail = append(*trail, auditedActuation{token: token, action: action, automatic: automatic, failed: err != nil})
	}).(*relayService)
	svc.now = func() time.Time { return clock }
	return svc, notif, trail, &clock
}

// THE RULE THAT MATTERS MOST. A rate limiter that can block an OFF is a siren nobody can
// silence — and it would refuse exactly when the siren is sounding, which is when the
// limiter is most likely to have been tripped.
func TestSwitchingAnOutputOffIsNeverRefused(t *testing.T) {
	cam := newFakeRelayCamera(onvif.RelayOutput{Token: "R1", Bistable: true})
	svc, _, trail, _ := newRelayRig(t, cam)
	ctx := context.Background()

	// Fire automatically, which arms the rate limit.
	if err := svc.Fire(ctx, RelayFireRequest{CameraId: 1, Token: "R1", Action: RelayActionPulse, PulseSeconds: 60, Automatic: true}); err != nil {
		t.Fatalf("pulse: %v", err)
	}
	// A second automatic actuation is throttled...
	before := cam.trail()
	if err := svc.Fire(ctx, RelayFireRequest{CameraId: 1, Token: "R1", Action: RelayActionPulse, Automatic: true}); err != nil {
		t.Fatalf("throttled actuation must not error: %v", err)
	}
	if cam.trail() != before {
		t.Fatalf("an automatic actuation inside the interval reached the camera: %s", cam.trail())
	}

	// ...and OFF still goes through, immediately.
	if err := svc.Fire(ctx, RelayFireRequest{CameraId: 1, Token: "R1", Action: RelayActionOff, Automatic: true}); err != nil {
		t.Fatalf("off must never be refused: %v", err)
	}
	if cam.state("R1") {
		t.Fatal("the output is still on after being switched off")
	}
	// And it is audited like everything else.
	if len(*trail) == 0 || (*trail)[len(*trail)-1].action != RelayActionOff {
		t.Fatalf("the off was not audited: %+v", *trail)
	}
}

// OFF must work even when the things an ON depends on are broken. Everything above the off
// branch in Fire() is a way for the appliance to refuse to stop a siren.
func TestOffWorksEvenWhenTheCameraCannotBeListed(t *testing.T) {
	cam := newFakeRelayCamera(onvif.RelayOutput{Token: "R1", Bistable: true})
	cam.listErr = errors.New("camera did not answer")
	svc, _, _, _ := newRelayRig(t, cam)

	if err := svc.Fire(context.Background(), RelayFireRequest{
		CameraId: 1, Token: "R1", Action: RelayActionOff,
	}); err != nil {
		t.Fatalf("off must not depend on being able to list the outputs: %v", err)
	}
	if !strings.Contains(cam.trail(), "off:R1") {
		t.Fatalf("the off never reached the camera: %s", cam.trail())
	}
}

// The responsibility for switching off goes to the DEVICE whenever the device will take it:
// a monostable output releases itself whether or not this appliance is still running.
func TestAPulsePrefersTheDevicesOwnTimer(t *testing.T) {
	cam := newFakeRelayCamera(onvif.RelayOutput{Token: "R1", Bistable: false, DelaySeconds: 5})
	svc, _, _, _ := newRelayRig(t, cam)

	if err := svc.Fire(context.Background(), RelayFireRequest{
		CameraId: 1, Token: "R1", Action: RelayActionPulse, PulseSeconds: 5,
	}); err != nil {
		t.Fatalf("pulse: %v", err)
	}
	if cam.trail() != "on:R1" {
		t.Fatalf("a self-releasing output should be switched on and left to the device: %s", cam.trail())
	}
	views, err := svc.Relays(context.Background(), 1)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if views[0].HeldByUs {
		t.Fatal("we must not be holding an output the device releases itself")
	}
}

// A bistable output is first ASKED to become a timed one, so a crash mid-pulse is harmless.
func TestABistableOutputIsAskedToBecomeATimedPulse(t *testing.T) {
	cam := newFakeRelayCamera(onvif.RelayOutput{Token: "R1", Bistable: true})
	svc, _, _, _ := newRelayRig(t, cam)

	if err := svc.Fire(context.Background(), RelayFireRequest{
		CameraId: 1, Token: "R1", Action: RelayActionPulse, PulseSeconds: 5,
	}); err != nil {
		t.Fatalf("pulse: %v", err)
	}
	if cam.trail() != "pulse-mode:R1,on:R1" {
		t.Fatalf("the camera was not asked to run the pulse itself: %s", cam.trail())
	}
	views, _ := svc.Relays(context.Background(), 1)
	if views[0].HeldByUs {
		t.Fatal("once the device runs the timer, this appliance is not holding the output")
	}
}

// ...and when the camera refuses, we hold it — and SAY we are holding it, because that is
// the one state where a restart of this process leaves the output energised.
func TestWhenTheCameraWillNotTimeThePulseWeHoldItAndSaySo(t *testing.T) {
	cam := newFakeRelayCamera(onvif.RelayOutput{Token: "R1", Bistable: true})
	cam.pulseModeErr = errors.New("not configurable")
	svc, _, _, _ := newRelayRig(t, cam)

	if err := svc.Fire(context.Background(), RelayFireRequest{
		CameraId: 1, Token: "R1", Action: RelayActionPulse, PulseSeconds: 30,
	}); err != nil {
		t.Fatalf("pulse: %v", err)
	}
	views, _ := svc.Relays(context.Background(), 1)
	if !views[0].HeldByUs {
		t.Fatal("an output this appliance is responsible for releasing must say so")
	}
	if views[0].HeldUntil == 0 {
		t.Fatal("the screen needs to know when we intend to release it")
	}

	// And shutting down releases it rather than leaving the building's siren sounding
	// because the process stopped.
	svc.ReleaseAll(context.Background())
	if cam.state("R1") {
		t.Fatal("a held output was left energised across a shutdown")
	}
	views, _ = svc.Relays(context.Background(), 1)
	if views[0].HeldByUs {
		t.Fatal("the hold should be gone after releasing")
	}
}

func TestPulseBoundsAndUnknownOutputs(t *testing.T) {
	cam := newFakeRelayCamera(onvif.RelayOutput{Token: "R1", Bistable: false, DelaySeconds: 5})
	svc, _, _, _ := newRelayRig(t, cam)
	ctx := context.Background()

	if err := svc.Fire(ctx, RelayFireRequest{CameraId: 1, Token: "R1", Action: RelayActionPulse, PulseSeconds: 99999}); err == nil {
		t.Fatal("a pulse longer than the cap must be refused, not clamped — it holds a gate open")
	}
	if err := svc.Fire(ctx, RelayFireRequest{CameraId: 1, Token: "nope", Action: RelayActionPulse}); err == nil {
		t.Fatal("an output the camera does not have must be refused")
	}
	if err := svc.Fire(ctx, RelayFireRequest{CameraId: 1, Token: "", Action: RelayActionPulse}); err == nil {
		t.Fatal("a fire with no output must be refused")
	}
	// "On" on a self-releasing output cannot be honoured, and saying so beats silently
	// producing a pulse of the device's own length instead.
	if err := svc.Fire(ctx, RelayFireRequest{CameraId: 1, Token: "R1", Action: RelayActionOn}); err == nil {
		t.Fatal("latching a monostable output must be refused")
	}
}

// An operator pressing a button twice is an operator who meant it. Only automatic
// actuations are throttled.
func TestOnlyAutomaticActuationsAreRateLimited(t *testing.T) {
	cam := newFakeRelayCamera(onvif.RelayOutput{Token: "R1", Bistable: false, DelaySeconds: 2})
	svc, _, _, _ := newRelayRig(t, cam)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if err := svc.Fire(ctx, RelayFireRequest{CameraId: 1, Token: "R1", Action: RelayActionPulse, PulseSeconds: 2}); err != nil {
			t.Fatalf("manual pulse %d: %v", i, err)
		}
	}
	if strings.Count(cam.trail(), "on:R1") != 3 {
		t.Fatalf("an operator's presses were throttled: %s", cam.trail())
	}
}

// Every actuation is audited, including the automatic ones and including the failures:
// "who set the siren off at 04:12, and did the camera actually do it" has to be answerable.
func TestEveryActuationIsAudited(t *testing.T) {
	cam := newFakeRelayCamera(onvif.RelayOutput{Token: "R1", Bistable: false, DelaySeconds: 2})
	svc, _, trail, _ := newRelayRig(t, cam)
	ctx := context.Background()

	_ = svc.Fire(ctx, RelayFireRequest{CameraId: 1, Token: "R1", Action: RelayActionPulse, PulseSeconds: 2, Automatic: true, Reason: "Gate rule"})
	cam.stateErr = errors.New("camera refused")
	_ = svc.Fire(ctx, RelayFireRequest{CameraId: 1, Token: "R1", Action: RelayActionOff})

	if len(*trail) != 2 {
		t.Fatalf("want 2 audited actuations, got %d: %+v", len(*trail), *trail)
	}
	if !(*trail)[0].automatic {
		t.Fatal("the automatic actuation must be recorded as automatic")
	}
	if !(*trail)[1].failed {
		t.Fatal("a FAILED actuation is exactly the one somebody will ask about later")
	}
}

func TestParseRuleRelay(t *testing.T) {
	cases := []struct {
		name   string
		config string
		want   *RelayRule
	}{
		{name: "absent", config: `{"classes":["person"]}`, want: nil},
		{name: "unparseable", config: "{not json", want: nil},
		// A relay action with no output names nothing to switch; treated as absent rather
		// than as an actuation of "", which the camera would refuse once per alert forever.
		{name: "no token", config: `{"relay":{"pulseSeconds":5}}`, want: nil},
		{
			name:   "full",
			config: `{"relay":{"cameraId":4,"token":"R1","pulseSeconds":10}}`,
			want:   &RelayRule{CameraId: 4, Token: "R1", PulseSeconds: 10},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseRuleRelay(tc.config)
			if tc.want == nil {
				if got != nil {
					t.Fatalf("want nil, got %+v", got)
				}
				return
			}
			if got == nil || *got != *tc.want {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

// A RULE CAN ONLY EVER PULSE. A rule that could latch an output would leave a siren
// sounding until somebody found the screen it is switched off from — and the rule that does
// it is, by construction, the one firing at 4am with nobody watching.
func TestARuleCanOnlyPulse(t *testing.T) {
	recorded := []RelayFireRequest{}
	firer := relayFirerFunc(func(_ context.Context, req RelayFireRequest) error {
		recorded = append(recorded, req)
		return nil
	})
	if err := ApplyRuleRelay(context.Background(), firer,
		`{"relay":{"token":"R1","pulseSeconds":9}}`, 7, "Gate rule"); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(recorded) != 1 {
		t.Fatalf("want one actuation, got %d", len(recorded))
	}
	if recorded[0].Action != RelayActionPulse {
		t.Fatalf("a rule must only ever pulse, got %q", recorded[0].Action)
	}
	if !recorded[0].Automatic {
		t.Fatal("a rule's actuation must be marked automatic, or it is not rate-limited")
	}
	// cameraId 0 means the rule's own camera.
	if recorded[0].CameraId != 7 {
		t.Fatalf("target camera = %d, want the rule's own (7)", recorded[0].CameraId)
	}
}

type relayFirerFunc func(ctx context.Context, req RelayFireRequest) error

func (f relayFirerFunc) Fire(ctx context.Context, req RelayFireRequest) error { return f(ctx, req) }
