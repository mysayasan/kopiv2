package onvif

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ONVIF events: what the CAMERA noticed, rather than what we noticed about the camera
// (W3-5b).
//
// Every detection in this product so far has been ours — we pull a frame and run a model
// over it. A camera has its own opinions, and until now nothing asked for them: its
// built-in motion and analytics, its own tamper alarm, and — the part no amount of video
// analysis can substitute for — the DIGITAL INPUTS wired into its terminal block. A door
// contact, a PIR, a panic button under a counter, a beam across a gate. Those are facts
// about the physical world arriving on a wire, and the camera is already listening to
// them.
//
// THE TRANSPORT IS A SUBSCRIPTION WITH A LEASE, AND THAT IS THE WHOLE DIFFICULTY. ONVIF's
// PullPoint model has us ask the device to create a subscription, then long-poll it for
// messages, then RENEW it before it expires. A subscription that is not renewed is dropped
// by the camera without a word, and the symptom is not an error — it is silence. A door
// contact that stopped reporting looks exactly like a door that was never opened. Which is
// why the service layer treats a lapsed subscription as an alertable fault rather than a
// retry (see services/camera_events.go).

// Well-known ONVIF topics, as prefixes. Topic strings from real devices carry namespace
// prefixes and optional predicates, so callers match on CONTAINS rather than equality —
// see EventKind.
const (
	TopicDigitalInput = "Device/Trigger/DigitalInput"
	TopicRelayOutput  = "Device/Trigger/Relay"
	TopicMotion       = "VideoSource/MotionAlarm"
	TopicTamper       = "VideoSource/ImageTooBlurry"
	TopicSignalLoss   = "VideoSource/SignalLoss"
	TopicAnalytics    = "RuleEngine"
)

// EventSubscription is a live PullPoint subscription on one device.
type EventSubscription struct {
	// Address is the subscription-manager URL the DEVICE issued. Every later call — pull,
	// renew, unsubscribe — goes to this URL, not to the event service.
	Address string
	// TerminationTime is when the camera will drop this subscription if it is not renewed.
	TerminationTime time.Time
	// CurrentTime is the DEVICE's clock at the moment it answered.
	//
	// Kept because a camera whose clock is wrong — which is common, and is why this app has
	// a whole date/time management screen — reports a termination time that means nothing
	// against our clock. Renewal is driven by the difference between these two, not by
	// TerminationTime alone.
	CurrentTime time.Time
}

// Lease returns how long the subscription has left, measured on the DEVICE's clock.
func (s EventSubscription) Lease() time.Duration {
	if s.TerminationTime.IsZero() || s.CurrentTime.IsZero() {
		return 0
	}
	d := s.TerminationTime.Sub(s.CurrentTime)
	if d < 0 {
		return 0
	}
	return d
}

// Event is one normalized notification message.
type Event struct {
	// Topic is the device's topic string, e.g. "tns1:Device/Trigger/DigitalInput".
	Topic string
	// Operation is the WS-Notification PropertyOperation: Initialized, Changed or Deleted.
	//
	// INITIALIZED IS NOT AN EVENT. On subscribing, a camera sends the CURRENT state of
	// every property it publishes — so a building with four closed door contacts announces
	// four closed door contacts the instant we connect. Treated as events, every restart,
	// every renewal failure and every network blip would raise a burst of alarms for doors
	// nobody touched. The service layer uses these to learn the baseline and raises
	// nothing. See services/camera_events.go.
	Operation string
	// UTCTime is the device's timestamp for the message.
	UTCTime time.Time
	// Source identifies WHICH input, relay or video source: {"InputToken": "DIGIT_INPUT_1"}.
	Source map[string]string
	// Data is the payload: {"LogicalState": "true"}, {"State": "true"}, and so on.
	Data map[string]string
}

// SourceToken returns the first source value, which is what identifies the physical
// terminal an event came from. Devices spell the key differently (InputToken, RelayToken,
// VideoSourceConfigurationToken, ...), and a caller that hard-codes one gets nothing from
// the next vendor.
func (e Event) SourceToken() string {
	for _, key := range []string{"InputToken", "RelayToken", "Token", "VideoSourceToken", "VideoSourceConfigurationToken"} {
		if v := strings.TrimSpace(e.Source[key]); v != "" {
			return v
		}
	}
	for _, v := range e.Source {
		if trimmed := strings.TrimSpace(v); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// State returns the event's boolean payload and whether it had one.
//
// Devices spell it LogicalState, State, or IsMotion depending on the topic. A digital
// input "true" means the circuit is in its active state — which, for a door contact, may
// mean open or closed depending on how the installer wired it. That is a question about
// the building, not about the camera, and it is settled where a person can answer it.
func (e Event) State() (bool, bool) {
	for _, key := range []string{"LogicalState", "State", "IsMotion", "Value"} {
		if v, ok := e.Data[key]; ok {
			trimmed := strings.ToLower(strings.TrimSpace(v))
			switch trimmed {
			case "true", "1", "active", "on":
				return true, true
			case "false", "0", "inactive", "off":
				return false, true
			}
		}
	}
	return false, false
}

// EventKind classifies a topic into something the rest of the product can route on,
// matching on a suffix rather than an exact string because real topics carry namespace
// prefixes and sometimes trailing predicates.
func EventKind(topic string) string {
	t := strings.TrimSpace(topic)
	switch {
	case strings.Contains(t, TopicDigitalInput):
		return "input"
	case strings.Contains(t, TopicRelayOutput):
		return "relay"
	case strings.Contains(t, TopicMotion):
		return "motion"
	case strings.Contains(t, TopicSignalLoss):
		return "signal-loss"
	case strings.Contains(t, "VideoSource/Image") || strings.Contains(t, "Tamper"):
		return "tamper"
	case strings.Contains(t, TopicAnalytics):
		return "analytics"
	}
	return "other"
}

// EventRequest addresses a device's event service.
type EventRequest struct {
	DeviceServiceURL string
	EventServiceURL  string
	Credentials      Credentials
	// LeaseSeconds is how long to ask the camera to keep the subscription alive.
	LeaseSeconds int
}

// PullRequest long-polls an existing subscription.
type PullRequest struct {
	// SubscriptionURL is EventSubscription.Address.
	SubscriptionURL string
	Credentials     Credentials
	// TimeoutSeconds is how long the DEVICE should hold the request open waiting for
	// something to happen. The HTTP deadline is set longer than this, so a device that
	// answers exactly on time is not cut off by our own client.
	TimeoutSeconds int
	MessageLimit   int
}

// CreatePullPointSubscription asks the camera to open an event subscription for us.
func (c *Client) CreatePullPointSubscription(ctx context.Context, req EventRequest) (*EventSubscription, error) {
	endpoint, err := c.eventEndpoint(ctx, req)
	if err != nil {
		return nil, err
	}
	lease := req.LeaseSeconds
	if lease <= 0 {
		lease = 60
	}
	body, _, err := c.postSOAPWithHeader(ctx, endpoint,
		createPullPointBody(lease),
		wsaHeader("http://www.onvif.org/ver10/events/wsdl/EventPortType/CreatePullPointSubscriptionRequest", endpoint),
		req.Credentials, 0)
	if err != nil {
		return nil, eventError("subscribe to camera events", body, err)
	}
	sub, err := ParsePullPointSubscription(body)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(sub.Address) == "" {
		// Without the address the subscription exists on the camera and we can never pull
		// from it or cancel it — a leak on the device, invisible from here. Saying so beats
		// reporting success.
		return nil, errors.New("camera accepted the event subscription but returned no address for it")
	}
	return sub, nil
}

// PullMessages long-polls a subscription. It returns the messages and the refreshed lease
// the device reports alongside them.
func (c *Client) PullMessages(ctx context.Context, req PullRequest) ([]Event, *EventSubscription, error) {
	if strings.TrimSpace(req.SubscriptionURL) == "" {
		return nil, nil, errors.New("subscription URL is required")
	}
	timeout := req.TimeoutSeconds
	if timeout <= 0 {
		timeout = 20
	}
	limit := req.MessageLimit
	if limit <= 0 {
		limit = 64
	}
	// The HTTP deadline is the device's own timeout plus a margin. Setting them equal
	// makes a punctual device race our client, and every well-behaved empty poll then
	// looks like a network failure — which is the signal the service layer uses to decide
	// a subscription has been lost.
	body, _, err := c.postSOAPWithHeader(ctx, req.SubscriptionURL,
		pullMessagesBody(timeout, limit),
		wsaHeader("http://www.onvif.org/ver10/events/wsdl/PullPointSubscription/PullMessagesRequest", req.SubscriptionURL),
		req.Credentials, time.Duration(timeout+10)*time.Second)
	if err != nil {
		return nil, nil, eventError("read camera events", body, err)
	}
	events, sub, perr := ParsePullMessages(body)
	if perr != nil {
		return nil, nil, perr
	}
	return events, sub, nil
}

// RenewSubscription extends a subscription's lease.
func (c *Client) RenewSubscription(ctx context.Context, req PullRequest, leaseSeconds int) (*EventSubscription, error) {
	if strings.TrimSpace(req.SubscriptionURL) == "" {
		return nil, errors.New("subscription URL is required")
	}
	if leaseSeconds <= 0 {
		leaseSeconds = 60
	}
	body, _, err := c.postSOAPWithHeader(ctx, req.SubscriptionURL,
		renewBody(leaseSeconds),
		wsaHeader("http://docs.oasis-open.org/wsn/bw-2/SubscriptionManager/RenewRequest", req.SubscriptionURL),
		req.Credentials, 0)
	if err != nil {
		return nil, eventError("renew the camera event subscription", body, err)
	}
	return ParseRenewResponse(body)
}

// Unsubscribe releases a subscription.
//
// Best-effort by nature — if the camera cannot be reached the subscription lapses on its
// own lease — but worth attempting, because a device has a small fixed number of
// subscription slots and an appliance that restarts a few times without releasing them
// runs out and can no longer subscribe at all.
func (c *Client) Unsubscribe(ctx context.Context, req PullRequest) error {
	if strings.TrimSpace(req.SubscriptionURL) == "" {
		return nil
	}
	body, _, err := c.postSOAPWithHeader(ctx, req.SubscriptionURL,
		"\n    <wsnt:Unsubscribe/>",
		wsaHeader("http://docs.oasis-open.org/wsn/bw-2/SubscriptionManager/UnsubscribeRequest", req.SubscriptionURL),
		req.Credentials, 0)
	if err != nil {
		return eventError("release the camera event subscription", body, err)
	}
	return nil
}

// eventEndpoint resolves the device's event service URL.
func (c *Client) eventEndpoint(ctx context.Context, req EventRequest) (string, error) {
	if url := strings.TrimSpace(req.EventServiceURL); url != "" {
		return url, nil
	}
	deviceURL, err := NormalizeDeviceServiceURL(req.DeviceServiceURL)
	if err != nil {
		return "", err
	}
	services, err := c.GetServices(ctx, DeviceRequest{DeviceServiceURL: deviceURL, Credentials: req.Credentials})
	if err != nil {
		return "", err
	}
	for _, svc := range services {
		if strings.Contains(strings.ToLower(svc.Namespace), "/events") && strings.TrimSpace(svc.XAddr) != "" {
			return strings.TrimSpace(svc.XAddr), nil
		}
	}
	return "", errors.New("this camera does not offer an ONVIF event service")
}

func eventError(action string, body []byte, err error) error {
	if reason := ParseSOAPFault(body); reason != "" {
		return fmt.Errorf("%s failed: %s", action, reason)
	}
	return fmt.Errorf("%s failed: %w", action, err)
}

// wsaHeader renders the WS-Addressing header the subscription-manager calls need.
func wsaHeader(action string, to string) string {
	return fmt.Sprintf(`
    <wsa:Action s:mustUnderstand="1">%s</wsa:Action>
    <wsa:To s:mustUnderstand="1">%s</wsa:To>`, xmlEscape(action), xmlEscape(to))
}

func createPullPointBody(leaseSeconds int) string {
	return fmt.Sprintf(`
    <tev:CreatePullPointSubscription>
      <tev:InitialTerminationTime>PT%dS</tev:InitialTerminationTime>
    </tev:CreatePullPointSubscription>`, leaseSeconds)
}

func pullMessagesBody(timeoutSeconds int, limit int) string {
	return fmt.Sprintf(`
    <tev:PullMessages>
      <tev:Timeout>PT%dS</tev:Timeout>
      <tev:MessageLimit>%d</tev:MessageLimit>
    </tev:PullMessages>`, timeoutSeconds, limit)
}

func renewBody(leaseSeconds int) string {
	return fmt.Sprintf(`
    <wsnt:Renew>
      <wsnt:TerminationTime>PT%dS</wsnt:TerminationTime>
    </wsnt:Renew>`, leaseSeconds)
}
