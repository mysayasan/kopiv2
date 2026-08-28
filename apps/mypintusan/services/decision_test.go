package services

import (
	"testing"
	"time"

	"github.com/mysayasan/kopiv2/apps/mypintusan/entities"
)

// kl is the site timezone used throughout. Schedules and holidays are local concepts, and running
// these tests in UTC would hide exactly the class of bug the Location field exists to prevent.
var kl = time.FixedZone("Asia/Kuala_Lumpur", 8*3600)

// testPIN is a stand-in verifier: the "hash" is the PIN itself. Keeping bcrypt out of these tests
// is the point of injecting the verifier — the decision path's job is the ORDER of the gates, not
// the strength of the hash.
func testPIN(hash, presented string) bool { return hash != "" && hash == presented }

// baseline builds a snapshot that grants: healthy reader, active card, active holder, 24/7 grant.
// Each test then breaks exactly one thing, so a failure names its own cause.
func baseline() Snapshot {
	return Snapshot{
		Now:      time.Date(2026, 8, 4, 10, 0, 0, 0, kl), // a Tuesday, mid-morning
		Location: kl,
		Door: entities.Door{
			Id: 1, Name: "Front", Class: entities.ClassInterior, LockKind: entities.LockFailSecure,
			UnlockSeconds: 5, ExtendedUnlockSeconds: 15, Enabled: true,
			OfflinePolicy: entities.OfflineCached, AntiPassback: entities.APBOff,
		},
		Reader:       entities.Reader{Id: 1, DoorId: 1, Direction: entities.DirectionIn, Enabled: true},
		ReaderOnline: true,
		Credential: &entities.Credential{
			Id: 10, HolderId: 20, Kind: entities.CredCard, Format: entities.FormatWiegand26,
			FacilityCode: 7, CardNumber: "1234", Status: entities.CredActive,
		},
		RawCredential: "7:1234",
		Holder: &entities.Holder{
			Id: 20, Ref: "E-001", Name: "Aisyah", Kind: entities.HolderStaff,
			Status: entities.HolderActive,
		},
		Grants:    []entities.Grant{{Id: 1, GroupId: 5, DoorId: 1, ScheduleId: 100}},
		Schedules: map[int64]entities.Schedule{100: {Id: 100, Name: "Always", Always: true}},
		Windows:   map[int64][]entities.ScheduleWindow{},
	}
}

// The class default for Secure Channel. It was documented on the field for months and applied
// nowhere: a critical door created without mentioning it came out FALSE, and a card on a
// plaintext reader opened it.

func TestSecureChannelDefault_OnForTheClassesThatFaceOutward(t *testing.T) {
	for _, class := range []string{entities.ClassPerimeter, entities.ClassCritical} {
		if !entities.SecureChannelDefault(class) {
			t.Errorf("%s doors must require a Secure Channel by default", class)
		}
	}
}

func TestSecureChannelDefault_OffForInterior(t *testing.T) {
	// The escape hatch, and why it exists: a cupboard on a spur of cheap non-SC readers. Turning
	// this on by default would take those doors out of service on upgrade.
	if entities.SecureChannelDefault(entities.ClassInterior) {
		t.Error("interior doors must not require a Secure Channel by default")
	}
	if entities.SecureChannelDefault("") {
		t.Error("an unset class must not silently require a Secure Channel")
	}
}

func TestDecideGrantsTheHappyPath(t *testing.T) {
	d := Decide(baseline(), testPIN)
	if !d.Granted {
		t.Fatalf("baseline was denied: %s (%s)", d.Reason, d.Detail)
	}
	if d.Reason != entities.ReasonOK {
		t.Errorf("reason = %q, want ok", d.Reason)
	}
	if d.StrikeSeconds != 5 {
		t.Errorf("strike = %ds, want 5", d.StrikeSeconds)
	}
	if d.Duress {
		t.Error("a normal entry was flagged as duress")
	}
}

// TestDecideDenials walks every denial gate. The reason code matters as much as the denial: it is
// what an operator sees at 3am and what an alert rule matches on.
func TestDecideDenials(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Snapshot)
		want   string
	}{
		{"door disabled", func(s *Snapshot) { s.Door.Enabled = false }, entities.ReasonDoorDisabled},
		{"reader offline", func(s *Snapshot) { s.ReaderOnline = false }, entities.ReasonReaderOffline},
		{"secure channel required but absent", func(s *Snapshot) {
			s.Door.RequireSecureChannel = true
			s.ReaderSecure = false
		}, entities.ReasonSecureChannel},
		{"lockdown", func(s *Snapshot) { s.Lockdown = true }, entities.ReasonLockdown},
		{"unknown card", func(s *Snapshot) { s.Credential = nil }, entities.ReasonUnknownCredential},
		{"credential revoked", func(s *Snapshot) { s.Credential.Status = entities.CredRevoked }, entities.ReasonCredentialRevoked},
		{"credential reported stolen", func(s *Snapshot) { s.Credential.Status = entities.CredStolen }, entities.ReasonCredentialRevoked},
		{"credential suspended", func(s *Snapshot) { s.Credential.Status = entities.CredSuspended }, entities.ReasonCredentialInactive},
		{"credential not yet valid", func(s *Snapshot) {
			s.Credential.ValidFrom = s.Now.Add(24 * time.Hour).Unix()
		}, entities.ReasonCredentialExpired},
		{"credential expired", func(s *Snapshot) {
			s.Credential.ValidUntil = s.Now.Add(-time.Hour).Unix()
		}, entities.ReasonCredentialExpired},
		{"orphan credential", func(s *Snapshot) { s.Holder = nil }, entities.ReasonUnknownCredential},
		{"holder suspended", func(s *Snapshot) { s.Holder.Status = entities.HolderSuspended }, entities.ReasonHolderSuspended},
		{"holder terminated", func(s *Snapshot) { s.Holder.Status = entities.HolderTerminated }, entities.ReasonHolderSuspended},
		{"holder expired", func(s *Snapshot) {
			s.Holder.ValidUntil = s.Now.Add(-time.Minute).Unix()
		}, entities.ReasonHolderExpired},
		{"no grant", func(s *Snapshot) { s.Grants = nil }, entities.ReasonNoGrant},
		{"grant points at a missing schedule", func(s *Snapshot) {
			s.Schedules = map[int64]entities.Schedule{}
		}, entities.ReasonOutOfSchedule},
		{"hard anti-passback violation", func(s *Snapshot) {
			s.Door.AntiPassback = entities.APBHard
			s.AntiPassbackViolation = true
		}, entities.ReasonAntipassback},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := baseline()
			c.mutate(&s)
			d := Decide(s, testPIN)
			if d.Granted {
				t.Fatalf("access was granted; expected denial %q", c.want)
			}
			if d.Reason != c.want {
				t.Errorf("reason = %q, want %q (detail: %s)", d.Reason, c.want, d.Detail)
			}
			if d.StrikeSeconds != 0 {
				t.Errorf("a denial returned a %ds strike", d.StrikeSeconds)
			}
		})
	}
}

// TestSecureChannelIsPerDoorNotPerReader pins the hardware plan §3.2 rule: the reader declares what
// it CAN do, the door decides what it MUST do. An interior door with an unencrypted session is fine;
// the same reader on a door that requires encryption is not.
func TestSecureChannelIsPerDoorNotPerReader(t *testing.T) {
	s := baseline()
	s.ReaderSecure = false

	s.Door.RequireSecureChannel = false
	if d := Decide(s, testPIN); !d.Granted {
		t.Errorf("an interior door denied a cleartext session: %s", d.Reason)
	}

	s.Door.RequireSecureChannel = true
	if d := Decide(s, testPIN); d.Granted {
		t.Error("a door requiring Secure Channel granted without one — this is the downgrade failure")
	}

	// And with the session established, the same door grants.
	s.ReaderSecure = true
	if d := Decide(s, testPIN); !d.Granted {
		t.Errorf("a door requiring Secure Channel denied WITH one: %s", d.Reason)
	}
}

// TestSoftAntiPassbackGrants covers the default mode. Hard APB on a door with a flaky contact locks
// out the entire staff on day one, so soft — log it, grant anyway — is the right default and must
// actually behave that way.
func TestSoftAntiPassbackGrants(t *testing.T) {
	s := baseline()
	s.Door.AntiPassback = entities.APBSoft
	s.AntiPassbackViolation = true

	if d := Decide(s, testPIN); !d.Granted {
		t.Errorf("soft anti-passback denied: %s", d.Reason)
	}
}

// TestDuressGrantsAndFlags is the differentiator. The door must open exactly as it always does —
// same grant, same strike time — while the alarm goes out of band. Anything the coercer can observe
// defeats the feature.
func TestDuressGrantsAndFlags(t *testing.T) {
	s := baseline()
	s.Credential.PinHash = "1234"
	s.Credential.DuressPinHash = "1235"

	normal := func() Decision { s.PresentedPIN = "1234"; return Decide(s, testPIN) }
	under := func() Decision { s.PresentedPIN = "1235"; return Decide(s, testPIN) }

	n, u := normal(), under()
	if !n.Granted || !u.Granted {
		t.Fatalf("normal granted=%v, duress granted=%v; both must open the door", n.Granted, u.Granted)
	}
	if !u.Duress {
		t.Error("the duress PIN was not flagged")
	}
	if n.Duress {
		t.Error("the normal PIN was flagged as duress")
	}
	// Observable behaviour must be identical.
	if n.StrikeSeconds != u.StrikeSeconds {
		t.Errorf("duress unlocked for %ds vs %ds normally — the difference is visible to a coercer",
			u.StrikeSeconds, n.StrikeSeconds)
	}

	s.PresentedPIN = "9999"
	if d := Decide(s, testPIN); d.Granted || d.Reason != entities.ReasonBadPin {
		t.Errorf("a wrong PIN gave granted=%v reason=%q", d.Granted, d.Reason)
	}
	s.PresentedPIN = ""
	if d := Decide(s, testPIN); d.Granted || d.Reason != entities.ReasonBadPin {
		t.Errorf("a missing PIN gave granted=%v reason=%q", d.Granted, d.Reason)
	}
}

// TestExtendedUnlockFollowsTheHolder covers the accessibility provision: it is modelled on the
// person so it follows them around the site rather than being re-entered at every door.
func TestExtendedUnlockFollowsTheHolder(t *testing.T) {
	s := baseline()
	s.Holder.ExtendedUnlock = true
	if d := Decide(s, testPIN); d.StrikeSeconds != 15 {
		t.Errorf("strike = %ds, want the 15s extended time", d.StrikeSeconds)
	}
}

// TestScheduleWindows covers the weekly pattern, including the night shift that a naive
// start <= now <= end comparison denies for the whole of its duration.
func TestScheduleWindows(t *testing.T) {
	const tue, wed, fri, sat = 2, 3, 5, 6

	cases := []struct {
		name    string
		windows []entities.ScheduleWindow
		at      time.Time
		want    bool
	}{
		{"inside office hours", []entities.ScheduleWindow{{Weekday: tue, StartMin: 8 * 60, EndMin: 18 * 60}},
			time.Date(2026, 8, 4, 10, 0, 0, 0, kl), true},
		{"before office hours", []entities.ScheduleWindow{{Weekday: tue, StartMin: 8 * 60, EndMin: 18 * 60}},
			time.Date(2026, 8, 4, 7, 59, 0, 0, kl), false},
		{"exactly at open", []entities.ScheduleWindow{{Weekday: tue, StartMin: 8 * 60, EndMin: 18 * 60}},
			time.Date(2026, 8, 4, 8, 0, 0, 0, kl), true},
		{"exactly at close is OUT", []entities.ScheduleWindow{{Weekday: tue, StartMin: 8 * 60, EndMin: 18 * 60}},
			time.Date(2026, 8, 4, 18, 0, 0, 0, kl), false},
		{"wrong day", []entities.ScheduleWindow{{Weekday: wed, StartMin: 8 * 60, EndMin: 18 * 60}},
			time.Date(2026, 8, 4, 10, 0, 0, 0, kl), false},

		// The night shift. A Friday 22:00-06:00 window must cover Friday evening AND the small
		// hours of Saturday, and must NOT cover Friday morning.
		{"night shift, evening half", []entities.ScheduleWindow{{Weekday: fri, StartMin: 22 * 60, EndMin: 6 * 60}},
			time.Date(2026, 8, 7, 23, 30, 0, 0, kl), true},
		{"night shift, morning half next day", []entities.ScheduleWindow{{Weekday: fri, StartMin: 22 * 60, EndMin: 6 * 60}},
			time.Date(2026, 8, 8, 2, 0, 0, 0, kl), true},
		{"night shift does not cover its own morning", []entities.ScheduleWindow{{Weekday: fri, StartMin: 22 * 60, EndMin: 6 * 60}},
			time.Date(2026, 8, 7, 3, 0, 0, 0, kl), false},
		{"night shift ends", []entities.ScheduleWindow{{Weekday: fri, StartMin: 22 * 60, EndMin: 6 * 60}},
			time.Date(2026, 8, 8, 6, 30, 0, 0, kl), false},
		{"saturday evening is not covered", []entities.ScheduleWindow{{Weekday: fri, StartMin: 22 * 60, EndMin: 6 * 60}},
			time.Date(2026, 8, 8, 23, 0, 0, 0, kl), false},

		{"multiple windows, second matches", []entities.ScheduleWindow{
			{Weekday: tue, StartMin: 6 * 60, EndMin: 8 * 60},
			{Weekday: tue, StartMin: 13 * 60, EndMin: 17 * 60},
		}, time.Date(2026, 8, 4, 14, 0, 0, 0, kl), true},
		{"multiple windows, lunch gap", []entities.ScheduleWindow{
			{Weekday: tue, StartMin: 8 * 60, EndMin: 12 * 60},
			{Weekday: tue, StartMin: 13 * 60, EndMin: 17 * 60},
		}, time.Date(2026, 8, 4, 12, 30, 0, 0, kl), false},
		{"no windows at all", nil, time.Date(2026, 8, 4, 10, 0, 0, 0, kl), false},
		{"saturday covered by its own window", []entities.ScheduleWindow{{Weekday: sat, StartMin: 9 * 60, EndMin: 13 * 60}},
			time.Date(2026, 8, 8, 10, 0, 0, 0, kl), true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := baseline()
			s.Now = c.at
			s.Schedules = map[int64]entities.Schedule{100: {Id: 100, Name: "Office"}}
			s.Windows = map[int64][]entities.ScheduleWindow{100: c.windows}

			d := Decide(s, testPIN)
			if d.Granted != c.want {
				t.Errorf("granted = %v, want %v (reason %q) at %s",
					d.Granted, c.want, d.Reason, c.at.Format("Mon 15:04"))
			}
		})
	}
}

// TestZeroLengthWindowCoversNothing pins the one way GATE 8 could fail OPEN.
//
// A window whose end equals its start used to fall into the wrapping branch, where
// `minutes >= StartMin` is true from the first minute of the day and the previous day's tail
// catches the rest — so 09:00-09:00 granted at every hour of every day while the schedules screen
// labelled it "overnight". The live bench measured it opening a door at 02:51 that was meant to be
// shut until 09:00, and the way in was not a typo on a form: a client that sends field names this
// API does not read has every window arrive as 0-0, which is how `bench_pintusan_offline.py` ran
// its whole life on a 24/7 schedule it believed was office hours.
//
// The create handler now refuses one. This test is about the rows already stored on installs that
// accepted them, which no validation can reach.
func TestZeroLengthWindowCoversNothing(t *testing.T) {
	s := baseline()
	// A Tuesday, 10:00 local — inside nothing, unless the window matches everything.
	s.Schedules = map[int64]entities.Schedule{100: {Id: 100}}
	s.Windows = map[int64][]entities.ScheduleWindow{
		100: {{Weekday: 2, StartMin: 9 * 60, EndMin: 9 * 60}},
	}
	if d := Decide(s, testPIN); d.Granted {
		t.Error("a 09:00-09:00 window granted; a zero-length window must cover nothing at all")
	}

	// The all-zero window is the shape a mis-keyed request actually produces, and it is the one
	// that used to match all seven days.
	s.Windows = map[int64][]entities.ScheduleWindow{
		100: {{Weekday: 0, StartMin: 0, EndMin: 0}, {Weekday: 2, StartMin: 0, EndMin: 0}},
	}
	if d := Decide(s, testPIN); d.Granted {
		t.Error("an all-zero window granted; that is a window that never parsed, not 24/7 access")
	}

	// And the night shift it is one minute away from must still work, or the guard has taken the
	// wrapping window with it.
	s.Windows = map[int64][]entities.ScheduleWindow{
		100: {{Weekday: 2, StartMin: 22 * 60, EndMin: 22*60 - 1}},
	}
	s.Now = time.Date(2026, 8, 4, 23, 0, 0, 0, kl)
	if d := Decide(s, testPIN); !d.Granted {
		t.Errorf("a window that wraps almost the whole way round denied at 23:00 (%s)", d.Reason)
	}
}

// TestScheduleUsesSiteLocalTime is the timezone trap. The same instant is inside office hours in
// Kuala Lumpur and the middle of the night in UTC; evaluating in the wrong zone silently shifts
// every schedule on the site by the offset.
func TestScheduleUsesSiteLocalTime(t *testing.T) {
	// 02:00 UTC on a Tuesday is 10:00 local — inside an 08:00-18:00 window.
	at := time.Date(2026, 8, 4, 2, 0, 0, 0, time.UTC)

	s := baseline()
	s.Now = at
	s.Location = kl
	s.Schedules = map[int64]entities.Schedule{100: {Id: 100}}
	s.Windows = map[int64][]entities.ScheduleWindow{100: {{Weekday: 2, StartMin: 8 * 60, EndMin: 18 * 60}}}

	if d := Decide(s, testPIN); !d.Granted {
		t.Errorf("a local-morning entry was denied (%s) — the schedule was evaluated in the wrong zone", d.Reason)
	}

	// The same wall-clock instant evaluated as UTC falls outside the window.
	s.Location = time.UTC
	if d := Decide(s, testPIN); d.Granted {
		t.Error("evaluating in UTC granted; the test's own premise is broken")
	}
}

// TestHolidayCalendar covers the three behaviours. The calendar is per-site because Malaysian
// public holidays vary by state — a national list would be wrong for half of any multi-state
// customer.
func TestHolidayCalendar(t *testing.T) {
	officeHours := map[int64][]entities.ScheduleWindow{
		100: {{Weekday: 2, StartMin: 8 * 60, EndMin: 18 * 60}}, // Tuesday
	}

	cases := []struct {
		name      string
		behaviour string
		always    bool
		want      bool
		wantWhy   string
	}{
		{"deny closes the building", entities.HolidayDeny, false, false, entities.ReasonHoliday},
		{"deny beats a 24/7 schedule", entities.HolidayDeny, true, false, entities.ReasonHoliday},
		{"ignore treats it as an ordinary day", entities.HolidayIgnore, false, true, entities.ReasonOK},
		{"follow-sunday falls outside a Tuesday window", entities.HolidayFollowSunday, false, false, entities.ReasonOutOfSchedule},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := baseline()
			s.Schedules = map[int64]entities.Schedule{100: {Id: 100, Always: c.always}}
			s.Windows = officeHours
			s.Holiday = &entities.Holiday{Name: "Hari Kebangsaan", Date: "2026-08-04", Behaviour: c.behaviour}

			d := Decide(s, testPIN)
			if d.Granted != c.want {
				t.Fatalf("granted = %v, want %v (reason %q)", d.Granted, c.want, d.Reason)
			}
			if d.Reason != c.wantWhy {
				t.Errorf("reason = %q, want %q", d.Reason, c.wantWhy)
			}
		})
	}
}

// TestHolidayFollowSundayGrantsWhenSundayIsCovered is the other half of follow-sunday: it must
// actually consult the Sunday windows, not merely deny.
func TestHolidayFollowSundayGrantsWhenSundayIsCovered(t *testing.T) {
	s := baseline()
	s.Schedules = map[int64]entities.Schedule{100: {Id: 100}}
	s.Windows = map[int64][]entities.ScheduleWindow{
		100: {{Weekday: int(time.Sunday), StartMin: 9 * 60, EndMin: 12 * 60}},
	}
	s.Holiday = &entities.Holiday{Behaviour: entities.HolidayFollowSunday}

	if d := Decide(s, testPIN); !d.Granted {
		t.Errorf("follow-sunday denied at 10:00 with a 09:00-12:00 Sunday window: %s", d.Reason)
	}
}

// TestOfflineBehaviour covers the cache policy. There is deliberately no allow-all anywhere in
// this path: "fail open on network loss" is a documented attack.
func TestOfflineBehaviour(t *testing.T) {
	cases := []struct {
		name      string
		class     string
		policy    string
		age       time.Duration
		offlineOK bool
		want      bool
		wantWhy   string
	}{
		{"interior inside TTL", entities.ClassInterior, entities.OfflineCached, time.Hour, false, true, entities.ReasonOK},
		{"interior past 72h", entities.ClassInterior, entities.OfflineCached, 73 * time.Hour, false, false, entities.ReasonOfflineCacheStale},
		{"perimeter inside 24h", entities.ClassPerimeter, entities.OfflineCached, 23 * time.Hour, false, true, entities.ReasonOK},
		{"perimeter past 24h", entities.ClassPerimeter, entities.OfflineCached, 25 * time.Hour, false, false, entities.ReasonOfflineCacheStale},
		{"critical needs the explicit flag", entities.ClassCritical, entities.OfflineCached, time.Hour, false, false, entities.ReasonOfflineNotAllowed},
		{"critical with the flag", entities.ClassCritical, entities.OfflineCached, time.Hour, true, true, entities.ReasonOK},
		{"critical past 8h even with the flag", entities.ClassCritical, entities.OfflineCached, 9 * time.Hour, true, false, entities.ReasonOfflineCacheStale},
		{"deny policy refuses regardless", entities.ClassInterior, entities.OfflineDeny, time.Second, true, false, entities.ReasonOfflineDenied},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := baseline()
			s.Offline = true
			s.CacheAge = c.age
			s.Door.Class = c.class
			s.Door.OfflinePolicy = c.policy
			s.Holder.OfflineAllowed = c.offlineOK

			d := Decide(s, testPIN)
			if d.Granted != c.want {
				t.Fatalf("granted = %v, want %v (reason %q)", d.Granted, c.want, d.Reason)
			}
			if d.Reason != c.wantWhy {
				t.Errorf("reason = %q, want %q", d.Reason, c.wantWhy)
			}
			if !d.Offline {
				t.Error("the decision did not record that it was served from cache")
			}
		})
	}
}

// TestOfflineDenialDoesNotMaskTheRealReason is why the offline gate runs LAST.
//
// A revoked card presented at a door running on a stale cache must be denied for the REVOCATION,
// not for the cache. Reporting "offline-cache-expired" would send an operator to investigate the
// network while the actual answer — that card was cancelled last week — goes unseen.
func TestOfflineDenialDoesNotMaskTheRealReason(t *testing.T) {
	s := baseline()
	s.Offline = true
	s.CacheAge = 100 * time.Hour // well past any TTL
	s.Credential.Status = entities.CredRevoked

	d := Decide(s, testPIN)
	if d.Granted {
		t.Fatal("a revoked credential was granted")
	}
	if d.Reason != entities.ReasonCredentialRevoked {
		t.Errorf("reason = %q, want %q — the offline gate masked the real cause",
			d.Reason, entities.ReasonCredentialRevoked)
	}
}

// TestGrantsAreAdditive pins the OR semantics. Evaluating grants as a conjunction would mean adding
// a group to a holder could REMOVE their access, which is the opposite of what anyone expects when
// they tick a box.
func TestGrantsAreAdditive(t *testing.T) {
	s := baseline()
	s.Grants = []entities.Grant{
		{Id: 1, GroupId: 5, DoorId: 1, ScheduleId: 100}, // never matches
		{Id: 2, GroupId: 6, DoorId: 1, ScheduleId: 200}, // matches
	}
	s.Schedules = map[int64]entities.Schedule{
		100: {Id: 100},
		200: {Id: 200, Always: true},
	}
	s.Windows = map[int64][]entities.ScheduleWindow{
		100: {{Weekday: 0, StartMin: 0, EndMin: 1}}, // a Sunday minute, never now
	}

	if d := Decide(s, testPIN); !d.Granted {
		t.Errorf("a holder with one matching grant was denied: %s", d.Reason)
	}
}

// TestUnknownCredentialRetainsTheRawValue guards the most valuable row in the log. A denied unknown
// credential at 03:00 on a perimeter door is either somebody's first day or somebody who should not
// be there, and the raw value is the only thing that tells them apart.
func TestUnknownCredentialRetainsTheRawValue(t *testing.T) {
	s := baseline()
	s.Credential = nil
	s.RawCredential = "7:9999"

	d := Decide(s, testPIN)
	if d.Granted || d.Reason != entities.ReasonUnknownCredential {
		t.Fatalf("granted=%v reason=%q", d.Granted, d.Reason)
	}
	// The decision does not carry the raw value — the caller writes it — so assert the snapshot
	// still holds it, i.e. Decide did not consume or clear it.
	if s.RawCredential != "7:9999" {
		t.Error("Decide mutated the snapshot's raw credential")
	}
}

// TestDecideDoesNotMutateSnapshot matters because the same snapshot is reused across a retry and
// across the audit write. A gate that quietly normalised a field would make the logged decision
// differ from the one that was made.
func TestDecideDoesNotMutateSnapshot(t *testing.T) {
	s := baseline()
	before := *s.Credential
	beforeHolder := *s.Holder

	Decide(s, testPIN)

	if *s.Credential != before {
		t.Error("Decide mutated the credential")
	}
	if *s.Holder != beforeHolder {
		t.Error("Decide mutated the holder")
	}
}

// TestDefaultOfflineTTL pins the per-class cache lifetimes and the override.
func TestDefaultOfflineTTL(t *testing.T) {
	cases := []struct {
		class    string
		override int
		want     int
	}{
		{entities.ClassInterior, 0, 72 * 3600},
		{entities.ClassPerimeter, 0, 24 * 3600},
		{entities.ClassCritical, 0, 8 * 3600},
		{entities.ClassCritical, 600, 600},
	}
	for _, c := range cases {
		d := entities.Door{Class: c.class, OfflineTTLSeconds: c.override}
		if got := d.DefaultOfflineTTLSeconds(); got != c.want {
			t.Errorf("%s (override %d) TTL = %d, want %d", c.class, c.override, got, c.want)
		}
	}
}
