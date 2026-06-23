package notification

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// MqttOptions configures the outbound MQTT delivery channel. It is the industrial
// transport: each notification is published as JSON to a broker topic.
type MqttOptions struct {
	// BrokerURL is the broker endpoint, e.g. tcp://host:1883 or ssl://host:8883.
	// Empty disables the channel (a no-op channel is returned).
	BrokerURL string
	// Topic is the publish topic. It may contain {{token}} placeholders expanded
	// from the notification's Data (e.g. matasan/alerts/{{cameraName}}/{{label}}).
	Topic string
	// ClientID is the MQTT client id. Empty lets the library assign one.
	ClientID string
	// QoS is the publish quality of service (0, 1, or 2). Defaults to 1.
	QoS byte
	// Retain sets the broker retain flag on each message.
	Retain bool
	// Username/Password authenticate to the broker (optional).
	Username string
	Password string
	// MinSeverity skips notifications below this severity. Empty means no floor.
	MinSeverity Severity
	// TLS materials (PEM contents). CACert verifies the broker; ClientCert +
	// ClientKey enable mutual TLS (client-certificate auth). InsecureSkipVerify
	// disables broker certificate verification (not recommended).
	CACert             string
	ClientCert         string
	ClientKey          string
	InsecureSkipVerify bool
	// QueueSize bounds the internal buffer. Defaults to 256.
	QueueSize int
	// PublishTimeout bounds each publish. Defaults to 8s.
	PublishTimeout time.Duration
	// Logger receives delivery warnings.
	Logger Logger
}

// NewMqttChannel returns an MQTT channel that publishes notifications as JSON to
// a broker topic. When the broker URL or topic is empty the channel is a no-op.
// The broker connection is established in the background with automatic reconnect,
// so a broker that is temporarily unreachable does not block startup or delivery.
func NewMqttChannel(opts MqttOptions) Channel {
	if strings.TrimSpace(opts.BrokerURL) == "" || strings.TrimSpace(opts.Topic) == "" {
		return noopChannel{name: "mqtt"}
	}
	qos := opts.QoS
	if qos > 2 {
		qos = 1
	}
	timeout := opts.PublishTimeout
	if timeout <= 0 {
		timeout = 8 * time.Second
	}

	co := mqtt.NewClientOptions()
	co.AddBroker(strings.TrimSpace(opts.BrokerURL))
	if id := strings.TrimSpace(opts.ClientID); id != "" {
		co.SetClientID(id)
	}
	if u := strings.TrimSpace(opts.Username); u != "" {
		co.SetUsername(u)
		co.SetPassword(opts.Password)
	}
	co.SetAutoReconnect(true)
	co.SetConnectRetry(true)
	co.SetConnectTimeout(timeout)
	co.SetOrderMatters(false)
	if tlsCfg, err := mqttTLSConfig(opts); err != nil {
		warn(opts.Logger, "notification.mqtt", "tls config failed: %v", err)
	} else if tlsCfg != nil {
		co.SetTLSConfig(tlsCfg)
	}

	client := mqtt.NewClient(co)
	// Connect in the background; ConnectRetry keeps trying if the broker is down.
	client.Connect()

	s := &mqttSender{
		client:  client,
		topic:   strings.TrimSpace(opts.Topic),
		qos:     qos,
		retain:  opts.Retain,
		timeout: timeout,
		logger:  opts.Logger,
	}
	return &mqttChannel{
		asyncSender: newAsyncSender("mqtt", opts.MinSeverity, opts.QueueSize, s.publish),
		client:      client,
	}
}

// mqttChannel couples the async queue with the broker client so Close drains the
// queue and then disconnects.
type mqttChannel struct {
	*asyncSender
	client mqtt.Client
}

func (c *mqttChannel) Close() error {
	_ = c.asyncSender.Close()
	if c.client != nil && c.client.IsConnected() {
		c.client.Disconnect(250)
	}
	return nil
}

type mqttSender struct {
	client  mqtt.Client
	topic   string
	qos     byte
	retain  bool
	timeout time.Duration
	logger  Logger
}

func (s *mqttSender) publish(n Notification) {
	payload, err := notificationJSON(n)
	if err != nil {
		warn(s.logger, "notification.mqtt", "marshal failed: %v", err)
		return
	}
	topic := expandTopicTemplate(s.topic, n)
	token := s.client.Publish(topic, s.qos, s.retain, payload)
	if !token.WaitTimeout(s.timeout) {
		warn(s.logger, "notification.mqtt", "publish to %q timed out", topic)
		return
	}
	if err := token.Error(); err != nil {
		warn(s.logger, "notification.mqtt", "publish to %q failed: %v", topic, err)
	}
}

// mqttTLSConfig builds a *tls.Config from the PEM materials, or nil when none are
// supplied and verification is not being skipped (so plain tcp:// brokers work).
func mqttTLSConfig(opts MqttOptions) (*tls.Config, error) {
	hasCA := strings.TrimSpace(opts.CACert) != ""
	hasClient := strings.TrimSpace(opts.ClientCert) != "" && strings.TrimSpace(opts.ClientKey) != ""
	if !hasCA && !hasClient && !opts.InsecureSkipVerify {
		return nil, nil
	}
	cfg := &tls.Config{InsecureSkipVerify: opts.InsecureSkipVerify} //nolint:gosec // opt-in only
	if hasCA {
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM([]byte(opts.CACert)) {
			return nil, errors.New("invalid CA certificate PEM")
		}
		cfg.RootCAs = pool
	}
	if hasClient {
		cert, err := tls.X509KeyPair([]byte(opts.ClientCert), []byte(opts.ClientKey))
		if err != nil {
			return nil, fmt.Errorf("invalid client certificate/key: %w", err)
		}
		cfg.Certificates = []tls.Certificate{cert}
	}
	return cfg, nil
}

var mqttTopicToken = regexp.MustCompile(`\{\{\s*([\w.]+)\s*\}\}`)

// expandTopicTemplate replaces {{token}} in a topic with the matching value from
// the notification's Data, falling back to the notification's own top-level fields
// (cameraId, category, severity, ...). Tokens that resolve to nothing leave no
// empty topic level: trailing/duplicate slashes from a missing token are dropped
// (e.g. "alerts/{{cameraId}}" with no camera → "alerts", not "alerts/").
func expandTopicTemplate(topic string, n Notification) string {
	if !strings.Contains(topic, "{{") {
		return topic
	}
	expanded := mqttTopicToken.ReplaceAllStringFunc(topic, func(match string) string {
		sub := mqttTopicToken.FindStringSubmatch(match)
		if len(sub) < 2 {
			return ""
		}
		if v, ok := n.Data[sub[1]]; ok {
			return fmt.Sprintf("%v", v)
		}
		return topicTopLevelField(n, sub[1])
	})
	// Drop empty levels left by tokens that resolved to nothing.
	parts := strings.Split(expanded, "/")
	kept := parts[:0]
	for _, p := range parts {
		if p != "" {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, "/")
}

// topicTopLevelField resolves a topic token against the notification's own fields
// when it isn't present in Data. Returns "" for unknown tokens or zero values.
func topicTopLevelField(n Notification, key string) string {
	switch key {
	case "cameraId":
		if n.CameraId != 0 {
			return fmt.Sprintf("%d", n.CameraId)
		}
	case "category":
		return n.Category
	case "severity":
		return string(n.Severity)
	case "refType":
		return n.RefType
	case "refId":
		if n.RefId != 0 {
			return fmt.Sprintf("%d", n.RefId)
		}
	case "id":
		return n.ID
	}
	return ""
}

// jsonPayload is the canonical JSON shape shared by the webhook and MQTT channels:
// the Notification plus the snapshot inlined as base64 when an attachment present.
type jsonPayload struct {
	Notification
	SnapshotBase64      string `json:"snapshotBase64,omitempty"`
	SnapshotContentType string `json:"snapshotContentType,omitempty"`
	SnapshotFilename    string `json:"snapshotFilename,omitempty"`
}

// notificationJSON renders a notification as the canonical delivery JSON used by
// the webhook and MQTT channels.
func notificationJSON(n Notification) ([]byte, error) {
	p := jsonPayload{Notification: n}
	if n.Attachment != nil && len(n.Attachment.Data) > 0 {
		p.SnapshotBase64 = base64.StdEncoding.EncodeToString(n.Attachment.Data)
		p.SnapshotContentType = n.Attachment.ContentType
		p.SnapshotFilename = n.Attachment.Filename
	}
	return json.Marshal(p)
}
