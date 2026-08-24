package services

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/mysayasan/kopiv2/domain/notification"
	"github.com/mysayasan/kopiv2/infra/onvif"
	"github.com/mysayasan/kopiv2/infra/safego"
	"github.com/mysayasan/kopiv2/infra/telemetry"
)

// The camera event listener: what the CAMERA noticed (W3-5b).
//
// Everything this appliance detects, it detects itself — it pulls a frame and runs a model
// over it. This is the other direction. A camera has its own opinions and its own senses,
// and the ones no amount of video analysis can substitute for are the DIGITAL INPUTS wired
// into its terminal block: a door contact, a PIR, a beam across a gate, a panic button
// under a counter. Those are facts about the physical world arriving on a wire.
//
// WHAT MAKES THIS HARD IS THAT THE FAILURE MODE IS SILENCE. ONVIF's PullPoint transport is a
// subscription with a lease: the camera drops it without a word if it is not renewed, and a
// door contact that has stopped reporting looks exactly like a door nobody has opened. The
// monitor therefore treats a subscription it cannot keep alive as an ALERTABLE FAULT rather
// than as something to retry quietly — the same principle as the recording-continuity and
// tamper monitors, and for the same reason: what fails silently has to be instrumented, or
// it is indistinguishable from working.

// MetricCameraEventsTotal counts events delivered, by kind.
const MetricCameraEventsTotal = "mymatasan_camera_events_total"

// MetricCameraEventSubscriptions gauges how many cameras are currently subscribed.
const MetricCameraEventSubscriptions = "mymatasan_camera_event_subscriptions"

// eventReconcileInterval is how often the monitor re-reads its settings and the camera list,
// so enabling the listener or adding a camera takes effect without a restart.
const eventReconcileInterval = 30 * time.Second

// eventBackoffMax bounds the retry wait for a camera whose subscription keeps failing. A
// camera that is switched off should not be dialled every second for a week.
const eventBackoffMax = 2 * time.Minute

// CameraEventMonitor keeps one PullPoint subscription per capable camera and turns what
// arrives into alerts, notifications and metrics.
type CameraEventMonitor struct {
	cameras  eventCameraClient
	client   eventClient
	settings IOnvifEventSettingsService
	notifier INotificationPublisher
	metrics  telemetry.Metrics

	mu      sync.Mutex
	workers map[int64]context.CancelFunc
}

// eventCameraClient is the slice of the camera service this needs.
type eventCameraClient interface {
	Get(ctx context.Context, limit uint64, offset uint64) ([]*CameraDetail, uint64, error)
	GetCameraCapabilities(ctx context.Context, id uint64) (*CameraCapabilities, error)
	EventEndpoint(ctx context.Context, id uint64) (string, onvif.Credentials, error)
	DisplayName(ctx context.Context, id int64) string
}

// eventClient is the slice of the ONVIF client this needs.
type eventClient interface {
	CreatePullPointSubscription(ctx context.Context, req onvif.EventRequest) (*onvif.EventSubscription, error)
	PullMessages(ctx context.Context, req onvif.PullRequest) ([]onvif.Event, *onvif.EventSubscription, error)
	RenewSubscription(ctx context.Context, req onvif.PullRequest, leaseSeconds int) (*onvif.EventSubscription, error)
	Unsubscribe(ctx context.Context, req onvif.PullRequest) error
}

func NewCameraEventMonitor(
	cameras eventCameraClient,
	client eventClient,
	settings IOnvifEventSettingsService,
	notifier INotificationPublisher,
	metrics telemetry.Metrics,
) *CameraEventMonitor {
	return &CameraEventMonitor{
		cameras: cameras, client: client, settings: settings,
		notifier: notifier, metrics: metrics,
		workers: map[int64]context.CancelFunc{},
	}
}

func (m *CameraEventMonitor) Start(ctx context.Context) {
	safego.Supervise(ctx, "mymatasan.events.reconcile", m.run)
}

func (m *CameraEventMonitor) run(ctx context.Context) {
	ticker := time.NewTicker(eventReconcileInterval)
	defer ticker.Stop()
	m.Reconcile(ctx)
	for {
		select {
		case <-ctx.Done():
			m.stopAll()
			return
		case <-ticker.C:
			m.Reconcile(ctx)
		}
	}
}

// Reconcile starts a listener for every capable camera and stops the ones that should no
// longer be running. Exported so a test can drive it without a ticker.
func (m *CameraEventMonitor) Reconcile(ctx context.Context) {
	cfg := m.currentSettings(ctx)
	if !cfg.Enabled {
		m.stopAll()
		m.gauge(0)
		return
	}
	cameras, _, err := m.cameras.Get(ctx, 500, 0)
	if err != nil {
		return
	}

	wanted := map[int64]bool{}
	overflow := 0
	for _, cam := range cameras {
		if cam == nil || ctx.Err() != nil {
			continue
		}
		id := cam.Camera.Id
		if id <= 0 || strings.TrimSpace(cam.XAddr) == "" {
			continue
		}
		if len(wanted) >= cfg.MaxCameras {
			overflow++
			continue
		}
		wanted[id] = true
	}
	if overflow > 0 {
		// REPORTED, not silently truncated. A listener that quietly covers the first
		// thirty-two cameras is worse than one that is switched off, because the screen
		// says it is running and the doors it is not watching look like doors nobody has
		// opened.
		log.Printf("events: %d camera(s) beyond the limit of %d are NOT being listened to",
			overflow, cfg.MaxCameras)
		m.announce(ctx, notification.Notification{
			Category: notification.CategorySystem,
			Severity: notification.Warning,
			Title:    "Camera events limited",
			Body: fmt.Sprintf("%d camera(s) are not being listened to: the limit is %d. "+
				"Their inputs and relays will not report anything.", overflow, cfg.MaxCameras),
			Source: "camera-events",
			Data:   map[string]any{"skipped": overflow, "maxCameras": cfg.MaxCameras},
		})
	}

	m.mu.Lock()
	for id, cancel := range m.workers {
		if !wanted[id] {
			cancel()
			delete(m.workers, id)
		}
	}
	for id := range wanted {
		if _, running := m.workers[id]; running {
			continue
		}
		workerCtx, cancel := context.WithCancel(ctx)
		m.workers[id] = cancel
		cameraId := id
		safego.Go(fmt.Sprintf("mymatasan.events.camera.%d", cameraId), func() {
			m.listen(workerCtx, cameraId)
		})
	}
	count := len(m.workers)
	m.mu.Unlock()
	m.gauge(float64(count))
}

// listen keeps one camera subscribed for as long as its context lives.
func (m *CameraEventMonitor) listen(ctx context.Context, cameraId int64) {
	backoff := 2 * time.Second
	// brokenSince is when this camera last had a working subscription. It is what turns
	// silence into a fault: a camera that has not managed to subscribe for longer than the
	// configured window is reported, once, rather than retried forever in the log.
	var brokenSince time.Time
	reported := false

	for ctx.Err() == nil {
		cfg := m.currentSettings(ctx)
		if !cfg.Enabled {
			return
		}
		// Only cameras that ADVERTISE the event service are dialled. Asking one that does
		// not is a guaranteed failure every backoff, forever, and would fill the log — and
		// eventually the notification feed — with a fault that is a fact about the model.
		if caps, err := m.cameras.GetCameraCapabilities(ctx, uint64(cameraId)); err == nil && caps != nil && !caps.Events {
			return
		}

		err := m.session(ctx, cameraId, cfg)
		if ctx.Err() != nil {
			return
		}
		if err == nil {
			backoff = 2 * time.Second
			brokenSince = time.Time{}
			reported = false
			continue
		}
		if brokenSince.IsZero() {
			brokenSince = time.Now()
		}
		if !reported && time.Since(brokenSince) >= time.Duration(cfg.LostAfterSeconds)*time.Second {
			reported = true
			m.announceLost(ctx, cameraId, err)
		}
		log.Printf("events: cam%d: %v (retrying in %s)", cameraId, err, backoff)
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > eventBackoffMax {
			backoff = eventBackoffMax
		}
	}
}

// session subscribes once and pulls until something goes wrong.
func (m *CameraEventMonitor) session(ctx context.Context, cameraId int64, cfg OnvifEventSettings) error {
	endpoint, credentials, err := m.cameras.EventEndpoint(ctx, uint64(cameraId))
	if err != nil {
		return err
	}
	sub, err := m.client.CreatePullPointSubscription(ctx, onvif.EventRequest{
		EventServiceURL: endpoint,
		Credentials:     credentials,
		LeaseSeconds:    cfg.LeaseSeconds,
	})
	if err != nil {
		return err
	}
	pull := onvif.PullRequest{
		SubscriptionURL: sub.Address,
		Credentials:     credentials,
		TimeoutSeconds:  cfg.PullTimeoutSeconds,
		MessageLimit:    64,
	}
	defer func() {
		// Best effort, and worth doing: a device has a small fixed number of subscription
		// slots, and an appliance that restarts a few times without releasing them runs
		// out and can no longer subscribe at all.
		releaseCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = m.client.Unsubscribe(releaseCtx, pull)
	}()

	// baselineDone marks the moment the camera has finished telling us the CURRENT state of
	// everything it publishes. See the Initialized handling in handle().
	state := &cameraEventState{seen: map[string]bool{}}
	renewAt := time.Now().Add(renewAfter(sub.Lease(), cfg))

	for ctx.Err() == nil {
		if time.Now().After(renewAt) {
			renewed, rerr := m.client.RenewSubscription(ctx, pull, cfg.LeaseSeconds)
			if rerr != nil {
				return rerr
			}
			renewAt = time.Now().Add(renewAfter(renewed.Lease(), cfg))
		}
		events, updated, perr := m.client.PullMessages(ctx, pull)
		if perr != nil {
			return perr
		}
		if updated != nil && updated.Lease() > 0 {
			// The DEVICE's own idea of the deadline, which is the only one that is right on
			// a camera whose clock is wrong.
			renewAt = time.Now().Add(renewAfter(updated.Lease(), cfg))
		}
		for _, event := range events {
			m.handle(ctx, cameraId, event, cfg, state)
		}
	}
	return ctx.Err()
}

// cameraEventState is one subscription's memory.
type cameraEventState struct {
	// seen records which (topic, source) pairs the camera has already told us the state of.
	seen map[string]bool
}

// handle turns one event into an alert, a notification and a metric — or into nothing.
func (m *CameraEventMonitor) handle(ctx context.Context, cameraId int64, event onvif.Event, cfg OnvifEventSettings, state *cameraEventState) {
	kind := onvif.EventKind(event.Topic)

	// INITIALIZED IS NOT AN EVENT, and this is the trap the whole file is arranged around.
	// On subscribing, a camera sends the CURRENT state of every property it publishes — so
	// a building with four closed door contacts announces four closed door contacts the
	// instant we connect. Treated as alerts, every restart, every renewal failure and every
	// network blip would raise a burst of alarms for doors nobody touched, at exactly the
	// moments when an operator is least able to tell a real one from noise.
	//
	// They are not discarded either: they are what tells us the state to compare the NEXT
	// message against.
	key := kind + "|" + event.SourceToken()
	if strings.EqualFold(event.Operation, "Initialized") {
		state.seen[key] = true
		return
	}
	state.seen[key] = true

	// The camera's own motion and analytics are opt-in, and off by default. This appliance
	// already runs its own detection over the same picture, with rules, zones, schedules
	// and cooldowns the camera knows nothing about; a second unfiltered stream of
	// "something moved" that no rule governs would bury the first.
	if (kind == "motion" || kind == "analytics") && !cfg.IncludeMotion {
		return
	}

	m.count(kind)
	active, known := event.State()
	name := m.cameras.DisplayName(ctx, cameraId)
	if strings.TrimSpace(name) == "" {
		name = fmt.Sprintf("camera %d", cameraId)
	}
	title, body := describeCameraEvent(kind, name, event.SourceToken(), active, known)

	// THE NOTIFICATION FEED IS THE HOME FOR THIS, and not the AI alert log — which is
	// where the first version of this code tried to put it, and the live bench caught.
	//
	// `alert_event` is the DETECTION log: every row references the rule that produced it,
	// `ValidateAlertEvent` requires a rule id, and the screens over it filter and label by
	// rule. A camera's digital input has no rule and never will, so the write was refused
	// and the row never appeared — the notification arrived, the log stayed empty, and the
	// only symptom was a line in a log nobody reads. HALF OF WHAT THIS FUNCTION CLAIMED TO
	// DO SIMPLY DID NOT HAPPEN.
	//
	// The feed is also where the comparable events already live: the tamper monitor and
	// both health monitors publish notifications and write no alert rows, for exactly this
	// reason. The feed is filterable by camera and by category, so "what happened on this
	// camera at 02:14" is still one question with one answer.
	//
	// Known follow-up: an input cannot yet be bookmarked into a case file, because case
	// evidence points at alert events. That needs the case item to be able to reference a
	// notification, which is a change to W3-3a rather than to this file.

	m.announce(ctx, notification.Notification{
		// A door contact is a SENSOR READING, not a detection — which is what
		// CategoryDeviceAlert exists for, and why mymatasan now registers it as a category
		// a destination can subscribe to. Filing it under vision.alert would make "tell me
		// when a door opens, but not about every person the AI sees" unexpressible.
		Category: notification.CategoryDeviceAlert,
		Severity: eventSeverity(kind, active, known),
		Title:    title,
		Body:     body,
		Source:   "camera-events",
		CameraId: cameraId,
		RefType:  "camera",
		RefId:    cameraId,
		Data: map[string]any{
			"cameraId": cameraId, "cameraName": name,
			"topic": event.Topic, "kind": kind,
			"sourceToken": event.SourceToken(), "active": active, "stateKnown": known,
		},
	})
}

// describeCameraEvent writes what happened for the person reading it, not for the person who
// wrote the parser. "Input 2 on Loading bay is now active" is a sentence; a topic string is
// not.
func describeCameraEvent(kind, cameraName, token string, active, known bool) (string, string) {
	where := cameraName
	if token != "" {
		where = fmt.Sprintf("%s (%s)", cameraName, token)
	}
	state := "changed"
	if known {
		state = map[bool]string{true: "active", false: "back to normal"}[active]
	}
	switch kind {
	case "input":
		return "Camera input " + state, fmt.Sprintf("A device wired to %s is %s.", where, state)
	case "relay":
		return "Camera output " + state, fmt.Sprintf("An output on %s is %s.", where, state)
	case "tamper":
		return "Camera reported tampering", fmt.Sprintf("%s reports its own view has been interfered with.", where)
	case "signal-loss":
		return "Camera lost its video signal", fmt.Sprintf("%s reports it has lost its video signal.", where)
	case "motion":
		return "Camera motion", fmt.Sprintf("%s reports motion in its own detector.", where)
	case "analytics":
		return "Camera analytics event", fmt.Sprintf("%s raised an event from its own analytics.", where)
	}
	return "Camera event", fmt.Sprintf("%s reported an event.", where)
}

func eventSeverity(kind string, active, known bool) notification.Severity {
	// A return to normal is information, never an alarm — otherwise a door closing wakes
	// somebody up as loudly as a door opening.
	if known && !active {
		return notification.Info
	}
	switch kind {
	case "tamper", "signal-loss":
		return notification.Critical
	case "input", "relay":
		return notification.Warning
	}
	return notification.Info
}

// renewAfter decides when to renew, from the lease the DEVICE reported.
//
// At two thirds of the lease, never later: a renewal that is merely "before expiry" races
// the camera's own clock, its rounding, and the round trip — and losing that race costs the
// whole subscription, silently.
func renewAfter(lease time.Duration, cfg OnvifEventSettings) time.Duration {
	if lease <= 0 {
		lease = time.Duration(cfg.LeaseSeconds) * time.Second
	}
	renew := lease * 2 / 3
	if renew < 5*time.Second {
		renew = 5 * time.Second
	}
	return renew
}

func (m *CameraEventMonitor) announceLost(ctx context.Context, cameraId int64, cause error) {
	name := m.cameras.DisplayName(ctx, cameraId)
	if strings.TrimSpace(name) == "" {
		name = fmt.Sprintf("camera %d", cameraId)
	}
	m.announce(ctx, notification.Notification{
		Category: notification.CategoryHealthCheck,
		Severity: notification.Warning,
		Title:    "Camera events stopped",
		Body: fmt.Sprintf(
			"%s has stopped reporting its own events (%v). Anything wired to its inputs — "+
				"a door contact, a beam, a panic button — is not being reported.", name, cause),
		Source:   "camera-events",
		CameraId: cameraId,
		RefType:  "camera",
		RefId:    cameraId,
		Data:     map[string]any{"cameraId": cameraId, "cameraName": name, "reason": cause.Error()},
	})
}

func (m *CameraEventMonitor) announce(ctx context.Context, n notification.Notification) {
	if m.notifier == nil {
		return
	}
	m.notifier.Publish(ctx, n)
}

func (m *CameraEventMonitor) count(kind string) {
	if m.metrics == nil {
		return
	}
	m.metrics.Inc(MetricCameraEventsTotal, telemetry.Labels{"kind": kind})
}

func (m *CameraEventMonitor) gauge(v float64) {
	if m.metrics == nil {
		return
	}
	m.metrics.Set(MetricCameraEventSubscriptions, nil, v)
}

func (m *CameraEventMonitor) currentSettings(ctx context.Context) OnvifEventSettings {
	if m.settings == nil {
		return DefaultOnvifEventSettings()
	}
	cfg, err := m.settings.Get(ctx)
	if err != nil {
		return DefaultOnvifEventSettings()
	}
	return cfg
}

func (m *CameraEventMonitor) stopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, cancel := range m.workers {
		cancel()
		delete(m.workers, id)
	}
}
