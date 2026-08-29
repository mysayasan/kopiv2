package services

import (
	"time"

	"github.com/mysayasan/kopiv2/apps/mypintusan/entities"
)

// The decision path: everything that stands between a badge being presented and a strike firing.
//
// It is a PURE FUNCTION over a snapshot, deliberately. The alternative — a service that queries the
// database as it goes — cannot be exercised against the awkward cases (a holiday that falls on a
// night shift, a cache that expired four seconds ago, a duress PIN at a critical door running
// offline) without standing up all of them for real. This shape makes every one of those a table
// row, and it means the same code decides identically whether it is running online against the
// database or offline against a cached replica.
//
// The order of the checks is fixed by MYPINTUSAN_DATA_MODEL.md §5.1 and is not arbitrary: cheap and
// security-critical gates come first, so a lockdown or an out-of-service reader is never overridden
// by anything later, and an unknown card never reaches a schedule lookup.

// Snapshot is everything the decision needs, gathered by the caller.
type Snapshot struct {
	Now time.Time
	// Location is the site's timezone. Schedules and holidays are LOCAL concepts: a night shift
	// that runs 22:00–06:00 and a public holiday are both wrong if evaluated in UTC.
	Location *time.Location

	Door   entities.Door
	Reader entities.Reader

	// ReaderOnline and ReaderSecure describe the reader RIGHT NOW, as reported by the OSDP bus.
	// ReaderSecure is the observed fact of an established session, not the profile's claim that it
	// could establish one.
	ReaderOnline bool
	ReaderSecure bool

	// Lockdown overrides every grant and every schedule. It does NOT override egress, which is
	// hardware and cannot be overridden by software by design.
	Lockdown bool

	// Offline means this decision is being served from a cached replica. CacheAge is how stale
	// that replica is.
	Offline  bool
	CacheAge time.Duration

	// Credential is nil for a card that matched nothing. RawCredential is retained either way.
	Credential    *entities.Credential
	RawCredential string
	Holder        *entities.Holder

	// PresentedPIN is set when a keypad entry accompanies the credential. Empty means card-only.
	PresentedPIN string

	// Grants are the grants for THIS door that the holder's groups reach. The caller does the join;
	// passing the pre-filtered set keeps this function from needing a group index.
	Grants []entities.Grant
	// Schedules and Windows resolve a grant's ScheduleId.
	Schedules map[int64]entities.Schedule
	Windows   map[int64][]entities.ScheduleWindow
	// Holiday is today's holiday for this site, or nil.
	Holiday *entities.Holiday

	// AntiPassbackViolation is computed by the caller from the last passage. P3 feature; a false
	// value here is the correct behaviour for a door that does not run APB.
	AntiPassbackViolation bool
}

// Decision is the outcome, ready to be written to the access log.
type Decision struct {
	Granted bool
	Reason  string
	Detail  string
	// Duress means grant AND raise a silent alarm. The reader must behave exactly as it does on a
	// normal grant — no different LED, no different buzzer — or the coercer standing there learns
	// that an alarm was sent.
	Duress bool
	// StrikeSeconds is how long to energise the strike. 0 when denied.
	StrikeSeconds int
	// Offline records that this was served from cache, for the audit row.
	Offline bool
}

// PINVerifier checks a presented PIN against a stored hash. Injected so the decision path never
// links a password-hashing library and stays a pure function; production passes bcrypt.
type PINVerifier func(hash, presented string) bool

// Decide runs the ordered gates and returns the outcome. It never mutates the snapshot.
func Decide(s Snapshot, verifyPIN PINVerifier) Decision {
	deny := func(reason, detail string) Decision {
		return Decision{Granted: false, Reason: reason, Detail: detail, Offline: s.Offline}
	}

	// GATE 1: the door itself.
	if !s.Door.Enabled {
		return deny(entities.ReasonDoorDisabled, "the door is disabled")
	}

	// GATE 2: reader health and Secure Channel.
	//
	// This is first among the runtime gates because a reader we cannot trust makes every later
	// check meaningless — the credential it reported may not be the credential presented. There is
	// no cleartext fallback: if the door requires a Secure Channel and there is not one, the answer
	// is no, and the reader is out of service.
	if !s.ReaderOnline {
		return deny(entities.ReasonReaderOffline, "the reader is not answering")
	}
	if s.Door.RequireSecureChannel && !s.ReaderSecure {
		return deny(entities.ReasonSecureChannel,
			"this door requires an encrypted reader session and none is established")
	}

	// GATE 3: lockdown. Above every grant and every schedule, and below nothing.
	if s.Lockdown {
		return deny(entities.ReasonLockdown, "the site is in lockdown")
	}

	// GATE 4: is this credential known, active and in date?
	if s.Credential == nil {
		// The raw value is still logged by the caller. An unknown card is not noise: it is either
		// somebody's first day, or somebody who should not be here.
		return deny(entities.ReasonUnknownCredential, "no credential matches the presented value")
	}
	cred := *s.Credential
	switch cred.Status {
	case entities.CredActive:
	case entities.CredRevoked:
		return deny(entities.ReasonCredentialRevoked, "the credential was revoked: "+cred.RevokeReason)
	case entities.CredLost, entities.CredStolen:
		// Deliberately distinguished from a plain revocation: a card reported stolen and then
		// presented at a door is an incident, and the reason code is what lets an alert rule say so.
		return deny(entities.ReasonCredentialRevoked, "the credential was reported "+cred.Status)
	default:
		return deny(entities.ReasonCredentialInactive, "the credential is "+cred.Status)
	}
	if !withinValidity(s.Now, cred.ValidFrom, cred.ValidUntil) {
		return deny(entities.ReasonCredentialExpired, "the credential is outside its validity window")
	}

	// GATE 5: is the holder known, active and in date?
	if s.Holder == nil {
		return deny(entities.ReasonUnknownCredential, "the credential is not attached to a holder")
	}
	holder := *s.Holder
	if holder.Status != entities.HolderActive {
		return deny(entities.ReasonHolderSuspended, "the holder is "+holder.Status)
	}
	if !withinValidity(s.Now, holder.ValidFrom, holder.ValidUntil) {
		return deny(entities.ReasonHolderExpired, "the holder is outside their validity window")
	}

	// GATE 6: the PIN, where one is required.
	//
	// Duress is resolved HERE rather than at the end, because a duress PIN must produce a GRANT.
	// Everything after this point runs identically for a duress entry — same grant lookup, same
	// schedule check — so that a coerced holder is not denied for a reason the coercer can see.
	duress := false
	if cred.PinHash != "" {
		if s.PresentedPIN == "" {
			return deny(entities.ReasonBadPin, "this credential requires a PIN")
		}
		switch {
		case verifyPIN != nil && cred.DuressPinHash != "" && verifyPIN(cred.DuressPinHash, s.PresentedPIN):
			duress = true
		case verifyPIN != nil && verifyPIN(cred.PinHash, s.PresentedPIN):
		default:
			return deny(entities.ReasonBadPin, "incorrect PIN")
		}
	}

	// GATE 7: does a grant exist for this holder's groups at this door?
	if len(s.Grants) == 0 {
		return deny(entities.ReasonNoGrant, "no access group grants this holder the door")
	}

	// GATE 8: is one of those grants in force right now, allowing for the holiday calendar?
	//
	// ANY grant that passes is enough — grants are additive. Evaluating them as a conjunction
	// would mean adding a group to a holder could REMOVE access, which is the opposite of what
	// every operator expects when they tick a box.
	allowed, blockedByHoliday := false, false
	for _, g := range s.Grants {
		ok, holidayBlocked := scheduleAllows(s, g.ScheduleId)
		if holidayBlocked {
			blockedByHoliday = true
		}
		if ok {
			allowed = true
			break
		}
	}
	if !allowed {
		if blockedByHoliday {
			return deny(entities.ReasonHoliday, "access is blocked by the holiday calendar")
		}
		return deny(entities.ReasonOutOfSchedule, "outside the permitted hours for this door")
	}

	// GATE 9: anti-passback. Soft mode logs and grants; hard mode denies.
	if s.AntiPassbackViolation && s.Door.AntiPassback == entities.APBHard {
		return deny(entities.ReasonAntipassback, "anti-passback: no matching exit was recorded")
	}

	// GATE 10: the offline rules, applied LAST among the denials.
	//
	// Deliberately last: a holder who would have been denied anyway must be denied for the REAL
	// reason. "offline-cache-expired" on a credential that was revoked last week would send an
	// operator to investigate the network instead of the revocation.
	if s.Offline {
		if s.Door.OfflinePolicy == entities.OfflineDeny {
			return deny(entities.ReasonOfflineDenied, "this door denies while offline")
		}
		ttl := time.Duration(s.Door.DefaultOfflineTTLSeconds()) * time.Second
		if s.CacheAge > ttl {
			// Past the TTL the door denies. There is no allow-all option anywhere in this path:
			// "fail open on network loss" is a documented attack — cut the uplink, walk in.
			return deny(entities.ReasonOfflineCacheStale, "the offline cache is older than this door allows")
		}
		if s.Door.Class == entities.ClassCritical && !holder.OfflineAllowed {
			return deny(entities.ReasonOfflineNotAllowed,
				"this holder is not permitted through a critical door while offline")
		}
	}

	reason := entities.ReasonOK
	if duress {
		reason = entities.ReasonDuress
	}
	return Decision{
		Granted:       true,
		Reason:        reason,
		Duress:        duress,
		StrikeSeconds: s.Door.StrikeSeconds(holder.ExtendedUnlock),
		Offline:       s.Offline,
	}
}

// withinValidity reports whether now falls inside [from, until]. Zero means unbounded at that end —
// 0 for ValidUntil is "no expiry", which is right for a permanent employee and wrong for every
// contractor and visitor, hence the UI nudge rather than a hard rule here.
func withinValidity(now time.Time, from, until int64) bool {
	t := now.Unix()
	if from > 0 && t < from {
		return false
	}
	if until > 0 && t > until {
		return false
	}
	return true
}

// scheduleAllows reports whether a schedule permits access now, and separately whether it was the
// holiday calendar that blocked it — so the denial can name the real cause.
func scheduleAllows(s Snapshot, scheduleId int64) (allowed, holidayBlocked bool) {
	sched, ok := s.Schedules[scheduleId]
	if !ok {
		return false, false
	}

	loc := s.Location
	if loc == nil {
		loc = time.Local
	}
	local := s.Now.In(loc)
	weekday := int(local.Weekday())

	// The holiday calendar rewrites the day before any window is consulted.
	if s.Holiday != nil {
		switch s.Holiday.Behaviour {
		case entities.HolidayDeny:
			return false, true
		case entities.HolidayFollowSunday:
			weekday = int(time.Sunday)
		case entities.HolidayIgnore:
			// Treat the day as ordinary.
		}
	}

	// An always-on schedule short-circuits, but only AFTER the holiday check — a 24/7 grant that
	// ignored a deny-holiday would be a hole in the calendar, and the calendar exists precisely to
	// close the building on days nobody is meant to be in it.
	if sched.Always {
		return true, false
	}

	minutes := local.Hour()*60 + local.Minute()
	for _, w := range s.Windows[scheduleId] {
		if windowCovers(w, weekday, minutes) {
			return true, false
		}
	}
	return false, false
}

// windowCovers reports whether a weekly window covers this weekday and minute-of-day.
//
// A window whose end is BEFORE its start WRAPS PAST MIDNIGHT — 22:00–06:00 is the night shift, and
// the obvious start <= now && now <= end comparison denies every one of those holders for the whole
// of their shift. The wrapped tail belongs to the FOLLOWING day, so a Friday 22:00–06:00 window
// also covers Saturday 02:00.
//
// A window that ends at the exact minute it starts covers NOTHING, and this is the only place in
// GATE 8 that can fail OPEN. It used to fall into the wrapping branch below, where
// `minutes >= StartMin` is true from the first minute of the day and the previous day's tail
// catches the rest — so 09:00–09:00 matched every hour of every day, on all seven weekdays, while
// displaying on the schedules screen as an overnight window. Nobody types a zero-length window
// meaning "always"; they type it as a slip, or their client sends field names this API does not
// read and every window arrives as 0–0. `bench_pintusan_offline.py` did exactly that — it posted
// `startMinute`/`endMinute` — and granted 24/7 for its whole life while reading as office hours.
// The create handler now refuses one, and this line is what protects the rows already stored on
// installs that accepted them.
func windowCovers(w entities.ScheduleWindow, weekday, minutes int) bool {
	if w.StartMin == w.EndMin {
		return false
	}
	if w.StartMin < w.EndMin {
		return w.Weekday == weekday && minutes >= w.StartMin && minutes < w.EndMin
	}
	// Wrapping window: the evening part on its own day, the morning part on the next.
	if w.Weekday == weekday && minutes >= w.StartMin {
		return true
	}
	prev := (weekday + 6) % 7
	return w.Weekday == prev && minutes < w.EndMin
}
