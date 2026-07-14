package entities

// DiscoveredDevice is a device that has announced itself during an enrollment window but has
// NOT been adopted. It is a candidate, not an inventory record.
//
// It exists to resolve a real tension in the design. The broker's security model is that a
// device which is not in the inventory cannot connect at all — that is what makes the device
// table the credential store. But a building with two hundred door contacts cannot be
// onboarded by typing two hundred device keys by hand, and a device that does not exist yet
// obviously cannot announce itself.
//
// The resolution is a deliberate, time-boxed hole: an admin opens an enrollment window, which
// mints a one-time key. Unknown devices presenting that key are admitted — but QUARANTINED.
// A quarantined session:
//
//   - has its payloads recorded HERE, as a candidate, and nowhere else;
//   - has NO telemetry stored and NO rules evaluated against it.
//
// That second property is the one that matters. An attacker who gets into the window can
// create noise in this table; they cannot forge a sensor reading, cannot move a chart, and
// cannot suppress or trigger an alert. The worst case is a list of junk candidates an admin
// declines — which is a nuisance, not a breach.
type DiscoveredDevice struct {
	Id int64 `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	// DeviceKey is the client id the device announced itself as.
	DeviceKey string `json:"deviceKey" form:"deviceKey" query:"deviceKey" ukey:"device_key" validate:"required"`
	// Topic is where it published, which is half of what identifies its type (the profile's
	// topic template is the other half).
	Topic string `json:"topic" form:"topic" query:"topic"`
	// Payload is ONE observed message, kept so an admin can see what the thing actually sends
	// before deciding what it is. Capped — a candidate record is not a telemetry store.
	Payload string `json:"payload" form:"payload" query:"payload"`
	// ObservedKeys is the comma-separated list of field names seen in the payload. This is what
	// the profile suggestion matches against: a device sending {temperature, humidity, battery}
	// is almost certainly a temp/humidity sensor, and the app can say so rather than making an
	// installer guess.
	ObservedKeys string `json:"observedKeys" form:"observedKeys" query:"observedKeys"`
	// SuggestedProfileId is the best-matching profile, or 0 when nothing matched well enough.
	SuggestedProfileId int64 `json:"suggestedProfileId" form:"suggestedProfileId" query:"suggestedProfileId"`
	// MessageCount is how many payloads this candidate has sent. A device that has spoken forty
	// times is real; one that spoke once may have been a passing scan.
	MessageCount int64 `json:"messageCount" form:"messageCount" query:"messageCount"`
	FirstSeenAt  int64 `json:"firstSeenAt" form:"firstSeenAt" query:"firstSeenAt"`
	LastSeenAt   int64 `json:"lastSeenAt" form:"lastSeenAt" query:"lastSeenAt"`
}
