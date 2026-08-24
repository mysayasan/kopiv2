package entities

// WallLayout is one named video wall: which cameras, in what order, in what grid, and how
// the wall behaves while nobody is touching it.
//
// WHY IT IS SERVER-SIDE. Live View already remembered a grid and a set of tiles — in a
// COOKIE. That is a per-browser preference, and a video wall is not a preference: it is how
// a control room is arranged. The cookie meant the wall existed only on the machine that
// built it, could not be handed to the operator on the next shift, could not be opened on a
// second monitor without being rebuilt by hand, and vanished when somebody cleared their
// browser. Moving it into the database is most of what makes this a video wall rather than a
// grid of tiles.
//
// It is deliberately NOT per user. A control room's walls are shared furniture — "Perimeter",
// "Loading bays", "Night" — and an operator arriving for a shift needs the same wall the last
// one was watching, not their own copy of it. Per-user walls would also mean an administrator
// cannot fix a wall that everyone is looking at.
type WallLayout struct {
	Id int64 `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	// Name is how the wall is chosen, so it is required and unique. Uniqueness is enforced
	// in the service rather than by a unique index, so a clash comes back as a sentence an
	// operator can act on instead of a driver error.
	Name string `json:"name" form:"name" query:"name" validate:"required"`
	// Grid is a layout id ("2x2", "3x3", ...). The set lives in services/wall.go and is
	// validated on save; an unknown grid is refused rather than stored and rendered as an
	// empty screen.
	Grid string `json:"grid" form:"grid" query:"grid"`
	// Cameras is the ORDERED camera id list, comma-separated.
	//
	// A join table would need an order column and buy nothing: this is a display
	// arrangement, not a relation anything queries. Nothing joins on it, nothing filters by
	// it, and the whole value is read and written as one unit every time.
	Cameras string `json:"cameras" form:"cameras" query:"cameras"`
	// CycleSeconds auto-advances the wall through its pages when the cameras do not fit the
	// grid. 0 = do not cycle. This is the "sequence" a guard station runs unattended.
	CycleSeconds int `json:"cycleSeconds" form:"cycleSeconds" query:"cycleSeconds"`
	// AutoPopSeconds pulls a camera onto the visible page when it raises an alert, for this
	// many seconds. 0 = never. It is the difference between a wall that shows a rotation and
	// a wall that shows what is happening.
	AutoPopSeconds int `json:"autoPopSeconds" form:"autoPopSeconds" query:"autoPopSeconds"`
	// IsDefault marks the wall a screen opens with when nothing else is chosen. At most one
	// wall may hold it; the service clears the others rather than letting two rows claim it,
	// because "the default" with two answers is a screen that opens differently depending on
	// row order.
	IsDefault bool `json:"isDefault" form:"isDefault" query:"isDefault"`

	CreatedBy   int64  `json:"createdBy" form:"createdBy" query:"createdBy"`
	CreatedName string `json:"createdName" form:"createdName" query:"createdName"`
	CreatedAt   int64  `json:"createdAt" form:"createdAt" query:"createdAt"`
	UpdatedAt   int64  `json:"updatedAt" form:"updatedAt" query:"updatedAt"`
}
