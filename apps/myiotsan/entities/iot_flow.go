package entities

// IotFlow is a saved, executable data-flow graph — the myiotsan equivalent of a Node-RED flow.
// It is authored on the visual canvas as a set of NODES (inputs that emit on a new reading,
// transforms that reshape the message — including arbitrary sandboxed JavaScript — logic that
// routes it, and outputs that notify or actuate) joined by WIRES. The runtime compiles the graph
// and runs it against the live telemetry stream.
//
// The whole node/wire graph is stored as one JSON document in Graph rather than as child rows.
// A graph is a picture with positions and per-node config; it is edited and saved wholesale, and
// it travels as one portable unit — the same reason a drawn detection zone is stored as a
// serialized string in mymatasan. Node device references inside the graph use NATURAL keys
// (deviceKey + telemetry key), never database ids, so a flow stays portable across installs and a
// template binds by name.
//
// A flow is shipped (Builtin) or site-authored, exactly like a DeviceProfile: builtins are seeded
// from a catalog and can be used and copied but not deleted, so a site cannot break a shipped
// sample by tidying up.
//
// CRUX — the safety invariant: nothing in a flow can actuate except a dedicated OUTPUT node, and
// the command output routes through the ONE guarded chokepoint (CommandService.Issue), so even an
// arbitrary-JavaScript node can only shape a value that Issue then re-validates against every gate
// (actuation-enabled, admin, declared bounds, rate limit, audit, never auto-retried). A flow is
// convenience and computation, not a new authority.
type IotFlow struct {
	Id   int64  `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	Slug string `json:"slug" form:"slug" query:"slug" ukey:"slug" validate:"required"`
	Name string `json:"name" form:"name" query:"name" validate:"required"`
	// Description and Category are for the human browsing the flow list ("Solar", "Comfort", …).
	Description string `json:"description" form:"description" query:"description"`
	Category    string `json:"category" form:"category" query:"category"`
	// Enabled is a soft off switch: a disabled flow is kept and editable but is not compiled into
	// the runtime, so it consumes no readings and fires no outputs.
	Enabled bool `json:"enabled" form:"enabled" query:"enabled"`
	// Graph is the node/wire document as JSON — see flowGraph in services. It is opaque to the
	// storage layer; the service validates it on save and the runtime compiles it.
	Graph string `json:"graph" form:"graph" query:"graph"`
	// Builtin marks a shipped sample flow. Builtins can be used and copied but not deleted.
	Builtin   bool  `json:"builtin" form:"builtin" query:"builtin"`
	CreatedBy int64 `json:"createdBy" form:"createdBy" query:"createdBy"`
	CreatedAt int64 `json:"createdAt" form:"createdAt" query:"createdAt"`
	UpdatedBy int64 `json:"updatedBy" form:"updatedBy" query:"updatedBy"`
	UpdatedAt int64 `json:"updatedAt" form:"updatedAt" query:"updatedAt"`
}
