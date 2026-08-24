package entities

// CaseFile is one investigation: a titled, assignable folder that footage, sightings and
// alerts are collected into, worked on, and eventually closed with a stated outcome.
//
// WHY THE PRODUCT NEEDS IT. Everything mymatasan records is organised by CAMERA and by
// TIME, which is how the appliance thinks and not how an incident does. An incident is a
// person walking through four cameras over eleven minutes, an alert that fired in the
// middle of it, and the two things somebody noticed afterwards. Before this, the only
// container for that was an operator's memory and a folder of downloaded .mp4 files —
// which is also the moment the chain of custody breaks, because a file on somebody's
// desktop has no record of where it came from or who took it.
//
// A case answers "what happened, who looked into it, what did they conclude, and what is
// the evidence" — and its export bundle is that answer in one verifiable file.
//
// THE NAME. The table is `case_file`, not `case`: CASE is a reserved word in SQL on every
// engine this suite runs on, and the bootstrapper derives the table name from the struct
// name (strcase.ToSnake). A struct called Case would produce DDL that fails to parse on
// sqlite and MariaDB and produces something surprising on Postgres. Renaming it here is
// free; discovering it in a customer's migration is not.
//
// FOOTAGE IS HELD WHILE A CASE IS OPEN. See services/case_hold.go — retention, the
// per-camera "Purge now" action and the disk-pressure sweeper all refuse to destroy
// footage that an open case points at. That is the property that makes a case worth
// opening at all: on a box with seven-day retention, a case without it is empty a week
// after the incident it documents.
type CaseFile struct {
	Id int64 `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	// Title is what the case is called in a list. Required — an untitled case is a case
	// nobody else can pick up.
	Title string `json:"title" form:"title" query:"title" validate:"required"`
	// Summary is the free-text description of the incident.
	Summary string `json:"summary" form:"summary" query:"summary"`
	// Status is CaseStatusOpen or CaseStatusClosed. Indexed with UpdatedAt because the
	// default listing is "the open cases, most recently touched first", and because the
	// footage hold query scans open cases on every retention sweep.
	Status string `json:"status" form:"status" query:"status" idx:"status_updated"`
	// AssignedTo is the local user who owns the case (0 = unassigned). AssignedName is
	// DENORMALISED on purpose, as are the actor names below: the record of who was
	// handling an investigation must survive that person's account being deleted. A
	// join that renders "user 7" after an offboarding is not a record of anything.
	AssignedTo   int64  `json:"assignedTo" form:"assignedTo" query:"assignedTo"`
	AssignedName string `json:"assignedName" form:"assignedName" query:"assignedName"`

	OpenedBy   int64  `json:"openedBy" form:"openedBy" query:"openedBy"`
	OpenedName string `json:"openedName" form:"openedName" query:"openedName"`
	OpenedAt   int64  `json:"openedAt" form:"openedAt" query:"openedAt"`

	// Outcome is what the case concluded, recorded at closure and required there. A case
	// closed with no stated outcome is indistinguishable from a case somebody tidied away,
	// which is exactly the distinction an auditor is asking about.
	Outcome    string `json:"outcome" form:"outcome" query:"outcome"`
	ClosedBy   int64  `json:"closedBy" form:"closedBy" query:"closedBy"`
	ClosedName string `json:"closedName" form:"closedName" query:"closedName"`
	ClosedAt   int64  `json:"closedAt" form:"closedAt" query:"closedAt"`

	UpdatedAt int64 `json:"updatedAt" form:"updatedAt" query:"updatedAt" idx:"status_updated"`
}

// Case status values. Two and no more: an "in progress" state that nothing in the product
// behaves differently for is a status field people forget to move, and the assignment
// already carries "somebody is on this".
const (
	CaseStatusOpen   = "open"
	CaseStatusClosed = "closed"
)
