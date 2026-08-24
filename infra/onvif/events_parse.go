package onvif

import (
	"encoding/xml"
	"strings"
	"time"
)

// Response parsing for the event calls in events.go.

type pullPointEnvelopeXML struct {
	Body pullPointBodyXML `xml:"Body"`
}

type pullPointBodyXML struct {
	Response subscriptionResponseXML `xml:"CreatePullPointSubscriptionResponse"`
}

type subscriptionResponseXML struct {
	Reference       subscriptionRefXML `xml:"SubscriptionReference"`
	CurrentTime     string             `xml:"CurrentTime"`
	TerminationTime string             `xml:"TerminationTime"`
}

type subscriptionRefXML struct {
	Address string `xml:"Address"`
}

type renewEnvelopeXML struct {
	Body renewBodyXML `xml:"Body"`
}

type renewBodyXML struct {
	Response renewResponseXML `xml:"RenewResponse"`
}

type renewResponseXML struct {
	CurrentTime     string `xml:"CurrentTime"`
	TerminationTime string `xml:"TerminationTime"`
}

type pullMessagesEnvelopeXML struct {
	Body pullMessagesBodyXML `xml:"Body"`
}

type pullMessagesBodyXML struct {
	Response pullMessagesResponseXML `xml:"PullMessagesResponse"`
}

type pullMessagesResponseXML struct {
	CurrentTime          string                   `xml:"CurrentTime"`
	TerminationTime      string                   `xml:"TerminationTime"`
	NotificationMessages []notificationMessageXML `xml:"NotificationMessage"`
}

type notificationMessageXML struct {
	Topic   string          `xml:"Topic"`
	Message eventMessageXML `xml:"Message>Message"`
}

type eventMessageXML struct {
	UTCTime           string           `xml:"UtcTime,attr"`
	PropertyOperation string           `xml:"PropertyOperation,attr"`
	Source            []simpleItemsXML `xml:"Source"`
	Data              []simpleItemsXML `xml:"Data"`
}

type simpleItemsXML struct {
	Items []simpleItemXML `xml:"SimpleItem"`
}

type simpleItemXML struct {
	Name  string `xml:"Name,attr"`
	Value string `xml:"Value,attr"`
}

// ParsePullPointSubscription parses CreatePullPointSubscriptionResponse.
func ParsePullPointSubscription(data []byte) (*EventSubscription, error) {
	var envelope pullPointEnvelopeXML
	if err := xml.Unmarshal(data, &envelope); err != nil {
		return nil, err
	}
	res := envelope.Body.Response
	return &EventSubscription{
		Address:         strings.TrimSpace(res.Reference.Address),
		CurrentTime:     parseEventTime(res.CurrentTime),
		TerminationTime: parseEventTime(res.TerminationTime),
	}, nil
}

// ParseRenewResponse parses RenewResponse.
func ParseRenewResponse(data []byte) (*EventSubscription, error) {
	var envelope renewEnvelopeXML
	if err := xml.Unmarshal(data, &envelope); err != nil {
		return nil, err
	}
	res := envelope.Body.Response
	return &EventSubscription{
		CurrentTime:     parseEventTime(res.CurrentTime),
		TerminationTime: parseEventTime(res.TerminationTime),
	}, nil
}

// ParsePullMessages parses PullMessagesResponse into normalized events, plus the lease the
// device reports alongside them.
//
// The lease matters as much as the messages: a device that answers a poll is also telling
// us how long it will keep the subscription, and reading it here is what lets the renewal
// loop follow the DEVICE's idea of the deadline rather than its own.
func ParsePullMessages(data []byte) ([]Event, *EventSubscription, error) {
	var envelope pullMessagesEnvelopeXML
	if err := xml.Unmarshal(data, &envelope); err != nil {
		return nil, nil, err
	}
	res := envelope.Body.Response
	sub := &EventSubscription{
		CurrentTime:     parseEventTime(res.CurrentTime),
		TerminationTime: parseEventTime(res.TerminationTime),
	}
	events := make([]Event, 0, len(res.NotificationMessages))
	for _, msg := range res.NotificationMessages {
		topic := strings.TrimSpace(msg.Topic)
		if topic == "" {
			continue
		}
		event := Event{
			Topic:     topic,
			Operation: strings.TrimSpace(msg.Message.PropertyOperation),
			UTCTime:   parseEventTime(msg.Message.UTCTime),
			Source:    simpleItems(msg.Message.Source),
			Data:      simpleItems(msg.Message.Data),
		}
		// A message with no data carries no fact. Devices emit these on some topics as
		// keep-alives, and passing them on would produce alert rows that say a door did
		// something without saying what.
		if len(event.Data) == 0 {
			continue
		}
		events = append(events, event)
	}
	return events, sub, nil
}

func simpleItems(groups []simpleItemsXML) map[string]string {
	out := map[string]string{}
	for _, group := range groups {
		for _, item := range group.Items {
			name := strings.TrimSpace(item.Name)
			if name == "" {
				continue
			}
			out[name] = strings.TrimSpace(item.Value)
		}
	}
	return out
}

// parseEventTime reads an ONVIF timestamp. Returns the zero time on anything unparseable,
// which every caller treats as "the device did not say" rather than as 1 January year 1.
func parseEventTime(value string) time.Time {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05Z", "2006-01-02T15:04:05"} {
		if t, err := time.Parse(layout, trimmed); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}
