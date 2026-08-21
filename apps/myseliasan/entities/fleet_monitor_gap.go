package entities

// FleetMonitorGap is a span during which NOTHING WAS WATCHING the fleet — the control
// plane was down, mid-upgrade, or (in a cluster) between leaders for longer than a
// heartbeat grace window.
//
// This table is the difference between an availability report and a fiction.
//
// Node state history is a log of observed TRANSITIONS, so a node that was online when
// the control plane stopped is still "online" in the log while the control plane is
// dead — and a naive reader would credit that whole outage to the node as uptime. The
// one period we can be certain we know nothing about is the period we were not running,
// and it is also exactly the period during which our own failure is most likely to have
// coincided with something else going wrong.
//
// So a gap is subtracted from the denominator and reported in its own right. The
// resulting figure is "available for 99.4% of the time we were watching, and we were
// watching 97% of the month", which an operator can act on and a customer can audit.
// Silently reporting 99.4% of a month a third of which was never observed is the sort
// of number that survives right up until somebody checks it.
//
// Detected rather than declared: the heartbeat sweep stamps a watermark every pass, and
// a sweep that finds the watermark older than the grace window writes the span it just
// discovered. That covers a crash, a kill -9, a host reboot and a long upgrade
// identically, because none of them get to run shutdown code.
type FleetMonitorGap struct {
	Id int64 `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	// StartedAt is the last sweep before monitoring stopped; EndedAt is the first sweep
	// after it resumed. The truth is somewhere inside that span, so the whole span is
	// treated as unmonitored — erring toward "we do not know", never toward "it was up".
	StartedAt int64 `json:"startedAt" form:"startedAt" query:"startedAt"`
	EndedAt   int64 `json:"endedAt" form:"endedAt" query:"endedAt"`
	// Reason is free text for the operator; the maths never reads it.
	Reason string `json:"reason" form:"reason" query:"reason"`
}
