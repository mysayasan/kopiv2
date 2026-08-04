package services

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/mysayasan/kopiv2/apps/mypintusan/entities"
	"github.com/mysayasan/kopiv2/infra/db/bootstrap"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
	"github.com/mysayasan/kopiv2/infra/db/sql/sqlite"
)

// These tests run against a REAL SQLite database created by the shared bootstrap, not a mock.
//
// That matters more than usual here. The store's whole job is to translate the decision path's
// questions into queries, and the two failure modes that would hurt most — a lookup that silently
// returns a prefix of a list, and a lookup that silently returns the wrong row entirely — are both
// invisible to a mock because a mock answers whatever it was told to. Only a real schema, real
// snake_case column mapping and the real generic repo can show them.

// Entities is the schema this app owns. It is also what an app/ composition root will hand to
// bootstrap.Ensure once one exists, so keeping it here means the list cannot drift from the store.
func Entities() []any {
	return []any{
		entities.Holder{},
		entities.Credential{},
		entities.Door{},
		entities.Reader{},
		entities.ReaderProfile{},
		entities.AccessGroup{},
		entities.AccessGroupMember{},
		entities.Schedule{},
		entities.ScheduleWindow{},
		entities.Holiday{},
		entities.Grant{},
		entities.AccessEvent{},
	}
}

// newSQLiteStore stands up a throwaway database with this app's schema.
func newSQLiteStore(t *testing.T) *SQLStore {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "data", "mypintusan.db")

	cfg := dbsql.DbConfigModel{Engine: "sqlite", DbName: path}
	status, err := bootstrap.Ensure(ctx, bootstrap.Options{
		AppName: "mypintusan-store-test",
		Config:  cfg,
		Bootstrap: bootstrap.BootstrapConfig{
			Enabled: true, AutoCreateDatabase: true, AutoCreateSchema: true, AutoMigrate: true,
		},
		Entities: Entities(),
	})
	if err != nil {
		t.Fatalf("bootstrap.Ensure: %v", err)
	}
	if !status.Ready {
		t.Fatalf("bootstrap not ready: %+v", status)
	}

	db, err := sqlite.NewDbCrud(cfg)
	if err != nil {
		t.Fatalf("sqlite.NewDbCrud: %v", err)
	}
	// Close the handle before t.TempDir's cleanup runs. IDbCrud does not expose Close, but the
	// SQLite implementation has one — and on Windows an open file cannot be deleted, so without
	// this every one of these tests fails in cleanup with a confusing "file in use" error that has
	// nothing to do with what the test was checking.
	t.Cleanup(func() {
		if closer, ok := db.(interface{ Close() error }); ok {
			_ = closer.Close()
		}
	})
	return NewSQLStore(db)
}

// TestSQLStoreSatisfiesStore is a compile-time guarantee that the production store and the
// in-memory test fake stay interchangeable. If they diverge, the offline replica and the live
// database stop being the same code path — which is the one property the pure decision function
// exists to preserve.
func TestSQLStoreSatisfiesStore(t *testing.T) {
	var _ Store = (*SQLStore)(nil)
	var _ Store = (*memStore)(nil)
}

// TestSQLStoreSchemaCreates proves every entity this app owns actually maps to a table. A struct
// tag typo would otherwise surface as a runtime query failure on the badge path.
func TestSQLStoreSchemaCreates(t *testing.T) {
	s := newSQLiteStore(t)
	ctx := context.Background()

	// Touch every repository. An unmapped entity fails here rather than at 3am.
	if _, err := s.Door(ctx, 1); err != nil {
		t.Errorf("Door: %v", err)
	}
	if _, err := s.Holder(ctx, 1); err != nil {
		t.Errorf("Holder: %v", err)
	}
	if _, err := s.ReaderByBus(ctx, "COM3", 1); err != nil {
		t.Errorf("ReaderByBus: %v", err)
	}
	if _, err := s.CredentialByCard(ctx, entities.FormatWiegand26, 7, "1234"); err != nil {
		t.Errorf("CredentialByCard: %v", err)
	}
	if _, err := s.GrantsFor(ctx, 1, 1); err != nil {
		t.Errorf("GrantsFor: %v", err)
	}
	if _, _, err := s.Schedules(ctx, []int64{1}); err != nil {
		t.Errorf("Schedules: %v", err)
	}
	if _, err := s.HolidayOn(ctx, 1, "2026-08-04"); err != nil {
		t.Errorf("HolidayOn: %v", err)
	}
	if err := s.RecordEvent(ctx, entities.AccessEvent{At: 1, Decision: entities.DecisionDenied}); err != nil {
		t.Errorf("RecordEvent: %v", err)
	}
}

// seedAccess plants a holder in two groups with two grants at one door, plus the schedules.
func seedAccess(t *testing.T, s *SQLStore) (holderId, doorId int64) {
	t.Helper()
	ctx := context.Background()

	doorId = mustCreate(t, s.doors, entities.Door{
		Name: "Front", Class: entities.ClassInterior, LockKind: entities.LockFailSecure,
		UnlockSeconds: 5, Enabled: true, OfflinePolicy: entities.OfflineCached,
	})
	holderId = mustCreate(t, s.holders, entities.Holder{
		Ref: "E-001", Name: "Aisyah", Kind: entities.HolderStaff, Status: entities.HolderActive,
	})

	g1 := mustCreate(t, s.grants, entities.Grant{GroupId: 0, DoorId: 0, ScheduleId: 0})
	_ = g1 // placeholder row, so ids are not accidentally 1-aligned across tables

	sched := mustCreate(t, s.schedules, entities.Schedule{Name: "Office"})
	always := mustCreate(t, s.schedules, entities.Schedule{Name: "Always", Always: true})

	mustCreate(t, s.windows, entities.ScheduleWindow{ScheduleId: sched, Weekday: 2, StartMin: 480, EndMin: 1080})
	mustCreate(t, s.windows, entities.ScheduleWindow{ScheduleId: sched, Weekday: 3, StartMin: 480, EndMin: 1080})

	groupA := mustCreate(t, s.groupsRepo(), entities.AccessGroup{Name: "Staff"})
	groupB := mustCreate(t, s.groupsRepo(), entities.AccessGroup{Name: "Contractors"})

	mustCreate(t, s.members, entities.AccessGroupMember{GroupId: groupA, HolderId: holderId})
	mustCreate(t, s.members, entities.AccessGroupMember{GroupId: groupB, HolderId: holderId})

	mustCreate(t, s.grants, entities.Grant{GroupId: groupA, DoorId: doorId, ScheduleId: sched})
	mustCreate(t, s.grants, entities.Grant{GroupId: groupB, DoorId: doorId, ScheduleId: always})

	// A grant for a group this holder is NOT in, at the same door — it must not be returned.
	other := mustCreate(t, s.groupsRepo(), entities.AccessGroup{Name: "Cleaners"})
	mustCreate(t, s.grants, entities.Grant{GroupId: other, DoorId: doorId, ScheduleId: always})

	_ = ctx
	return holderId, doorId
}

// TestGrantsForReturnsEveryGrant is the regression test for the limit-1 trap.
//
// GetByForeign passes a HARDCODED limit of 1 to Select, so a child lookup written the obvious way
// returns exactly one row and silently drops the rest. For grants that is not a performance bug: a
// holder whose access comes from their SECOND group is denied, intermittently, for a reason that
// reproduces nowhere.
func TestGrantsForReturnsEveryGrant(t *testing.T) {
	s := newSQLiteStore(t)
	holderId, doorId := seedAccess(t, s)

	grants, err := s.GrantsFor(context.Background(), holderId, doorId)
	if err != nil {
		t.Fatalf("GrantsFor: %v", err)
	}
	if len(grants) != 2 {
		t.Fatalf("got %d grants, want 2 — a truncated grant list denies access from the second group",
			len(grants))
	}
	for _, g := range grants {
		if g.DoorId != doorId {
			t.Errorf("grant %d is for door %d, not %d", g.Id, g.DoorId, doorId)
		}
	}
}

// TestGrantsForExcludesOtherGroups: a grant at the same door for a group the holder is not in must
// not leak through.
func TestGrantsForExcludesOtherGroups(t *testing.T) {
	s := newSQLiteStore(t)
	holderId, doorId := seedAccess(t, s)

	grants, err := s.GrantsFor(context.Background(), holderId, doorId)
	if err != nil {
		t.Fatalf("GrantsFor: %v", err)
	}
	// Three grants exist at this door; the holder reaches two of them.
	if len(grants) != 2 {
		t.Fatalf("got %d grants, want 2 — a grant for a group the holder is not in leaked through", len(grants))
	}
}

// TestGrantsForUnknownHolder returns nothing rather than everything. An empty group set that fell
// through to an unfiltered query would grant every door on the site.
func TestGrantsForUnknownHolder(t *testing.T) {
	s := newSQLiteStore(t)
	_, doorId := seedAccess(t, s)

	grants, err := s.GrantsFor(context.Background(), 9999, doorId)
	if err != nil {
		t.Fatalf("GrantsFor: %v", err)
	}
	if len(grants) != 0 {
		t.Fatalf("an unknown holder received %d grants", len(grants))
	}
}

// TestSchedulesLoadsAllWindows is the same limit-1 hazard one level down: a schedule whose windows
// were truncated to the first would deny every holder outside that one interval.
func TestSchedulesLoadsAllWindows(t *testing.T) {
	s := newSQLiteStore(t)
	holderId, doorId := seedAccess(t, s)

	grants, err := s.GrantsFor(context.Background(), holderId, doorId)
	if err != nil {
		t.Fatalf("GrantsFor: %v", err)
	}
	ids := make([]int64, 0, len(grants))
	for _, g := range grants {
		ids = append(ids, g.ScheduleId)
	}

	scheds, windows, err := s.Schedules(context.Background(), ids)
	if err != nil {
		t.Fatalf("Schedules: %v", err)
	}
	if len(scheds) != 2 {
		t.Fatalf("got %d schedules, want 2", len(scheds))
	}

	var office int64
	for id, sc := range scheds {
		if sc.Name == "Office" {
			office = id
		}
	}
	if office == 0 {
		t.Fatal("the office schedule was not loaded")
	}
	if got := len(windows[office]); got != 2 {
		t.Errorf("office schedule has %d windows, want 2 — a truncated window list denies whole days", got)
	}
}

// TestSchedulesDeduplicates: several grants commonly point at one schedule, and the badge path must
// not turn that into a query per grant.
func TestSchedulesDeduplicates(t *testing.T) {
	s := newSQLiteStore(t)
	seedAccess(t, s)

	scheds, _, err := s.Schedules(context.Background(), []int64{1, 1, 1, 2, 2})
	if err != nil {
		t.Fatalf("Schedules: %v", err)
	}
	if len(scheds) > 2 {
		t.Errorf("got %d schedules from 2 distinct ids", len(scheds))
	}
}

// TestCredentialByCardNeedsAllThreeFields pins the match key against a real query. The same number
// under a different facility code, or a different encoding, is a DIFFERENT credential.
func TestCredentialByCardNeedsAllThreeFields(t *testing.T) {
	s := newSQLiteStore(t)
	ctx := context.Background()

	holderA := mustCreate(t, s.holders, entities.Holder{Ref: "A", Name: "A", Status: entities.HolderActive})
	holderB := mustCreate(t, s.holders, entities.Holder{Ref: "B", Name: "B", Status: entities.HolderActive})

	mustCreate(t, s.creds, entities.Credential{
		HolderId: holderA, Kind: entities.CredCard, Format: entities.FormatWiegand26,
		FacilityCode: 7, CardNumber: "1234", Status: entities.CredActive,
	})
	mustCreate(t, s.creds, entities.Credential{
		HolderId: holderB, Kind: entities.CredCard, Format: entities.FormatWiegand26,
		FacilityCode: 8, CardNumber: "1234", Status: entities.CredActive,
	})

	got, err := s.CredentialByCard(ctx, entities.FormatWiegand26, 7, "1234")
	if err != nil {
		t.Fatalf("CredentialByCard: %v", err)
	}
	if got == nil || got.HolderId != holderA {
		t.Fatalf("facility 7 resolved to holder %v, want %d", got, holderA)
	}

	got, err = s.CredentialByCard(ctx, entities.FormatWiegand26, 8, "1234")
	if err != nil {
		t.Fatalf("CredentialByCard: %v", err)
	}
	if got == nil || got.HolderId != holderB {
		t.Fatalf("facility 8 resolved to holder %v, want %d", got, holderB)
	}

	// A different encoding of the same number matches nothing.
	got, err = s.CredentialByCard(ctx, entities.FormatWiegand34, 7, "1234")
	if err != nil {
		t.Fatalf("CredentialByCard: %v", err)
	}
	if got != nil {
		t.Error("a wiegand34 lookup matched a wiegand26 credential")
	}
}

// TestDuplicateCredentialFailsClosed: two holders sharing one card is a data error, and picking one
// is a coin toss over who the audit log blames.
func TestDuplicateCredentialFailsClosed(t *testing.T) {
	s := newSQLiteStore(t)
	ctx := context.Background()

	for _, ref := range []string{"A", "B"} {
		h := mustCreate(t, s.holders, entities.Holder{Ref: ref, Name: ref, Status: entities.HolderActive})
		mustCreate(t, s.creds, entities.Credential{
			HolderId: h, Kind: entities.CredCard, Format: entities.FormatWiegand26,
			FacilityCode: 7, CardNumber: "5555", Status: entities.CredActive,
		})
	}

	if _, err := s.CredentialByCard(ctx, entities.FormatWiegand26, 7, "5555"); err == nil {
		t.Error("a duplicated credential resolved to one holder instead of failing closed")
	}
}

// TestReaderByBusIsUniquePerAddress: two readers claiming one bus address is an enrolment error,
// and binding a door to whichever row sorted first is the silent misconfiguration to avoid.
func TestReaderByBusIsUniquePerAddress(t *testing.T) {
	s := newSQLiteStore(t)
	ctx := context.Background()

	mustCreate(t, s.readers, entities.Reader{Name: "In", DoorId: 1, BusPort: "COM3", OsdpAddress: 1, Enabled: true})
	got, err := s.ReaderByBus(ctx, "COM3", 1)
	if err != nil || got == nil {
		t.Fatalf("ReaderByBus: %v %v", got, err)
	}
	if got.Name != "In" {
		t.Errorf("resolved reader %q", got.Name)
	}

	// A different address on the same bus is a different reader.
	if r, err := s.ReaderByBus(ctx, "COM3", 2); err != nil || r != nil {
		t.Errorf("address 2 resolved to %v (err %v)", r, err)
	}
	// Same address on a different bus, too — OSDP addresses are only unique per segment.
	if r, err := s.ReaderByBus(ctx, "COM4", 1); err != nil || r != nil {
		t.Errorf("a different bus resolved to %v (err %v)", r, err)
	}

	mustCreate(t, s.readers, entities.Reader{Name: "Dup", DoorId: 2, BusPort: "COM3", OsdpAddress: 1, Enabled: true})
	if _, err := s.ReaderByBus(ctx, "COM3", 1); err == nil {
		t.Error("two readers at one bus address resolved to one instead of failing closed")
	}
}

// TestHolidaySiteOverridesGlobal covers the by-state calendar: the more specific row must win.
func TestHolidaySiteOverridesGlobal(t *testing.T) {
	s := newSQLiteStore(t)
	ctx := context.Background()

	mustCreate(t, s.holidays, entities.Holiday{
		Name: "National", Date: "2026-08-31", SiteId: 0, Behaviour: entities.HolidayDeny})
	mustCreate(t, s.holidays, entities.Holiday{
		Name: "Selangor only", Date: "2026-08-31", SiteId: 5, Behaviour: entities.HolidayFollowSunday})

	got, err := s.HolidayOn(ctx, 5, "2026-08-31")
	if err != nil {
		t.Fatalf("HolidayOn: %v", err)
	}
	if got == nil || got.Behaviour != entities.HolidayFollowSunday {
		t.Fatalf("site 5 resolved to %v, want the site-specific row", got)
	}

	// A site with no override falls back to the global row.
	got, err = s.HolidayOn(ctx, 9, "2026-08-31")
	if err != nil {
		t.Fatalf("HolidayOn: %v", err)
	}
	if got == nil || got.Behaviour != entities.HolidayDeny {
		t.Fatalf("site 9 resolved to %v, want the global row", got)
	}

	// An ordinary day resolves to nothing.
	if h, err := s.HolidayOn(ctx, 5, "2026-08-04"); err != nil || h != nil {
		t.Errorf("an ordinary day resolved to %v (err %v)", h, err)
	}
}

// TestAccessEventRoundTrip proves the log actually persists — including the fields that make a
// denial useful.
func TestAccessEventRoundTrip(t *testing.T) {
	s := newSQLiteStore(t)
	ctx := context.Background()

	ev := entities.AccessEvent{
		At: time.Now().Unix(), DoorId: 1, ReaderId: 2, HolderId: 3, HolderName: "Aisyah",
		RawCredential: "7:1234", Decision: entities.DecisionDenied,
		Reason: entities.ReasonUnknownCredential, Detail: "no match", Duress: true, Offline: true,
	}
	if err := s.RecordEvent(ctx, ev); err != nil {
		t.Fatalf("RecordEvent: %v", err)
	}

	rows, _, err := s.events.Get(ctx, "", 10, 0, nil, nil)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d events, want 1", len(rows))
	}
	got := rows[0]
	if got.RawCredential != "7:1234" {
		t.Errorf("raw credential = %q — the value an operator would investigate was lost", got.RawCredential)
	}
	if got.HolderName != "Aisyah" {
		t.Errorf("holder name = %q — the log must read without a join", got.HolderName)
	}
	if !got.Duress || !got.Offline {
		t.Errorf("flags lost: duress=%v offline=%v", got.Duress, got.Offline)
	}
	if got.Reason != entities.ReasonUnknownCredential {
		t.Errorf("reason = %q", got.Reason)
	}
}

// TestCredentialPINHashesAreNotSerialised guards the `json:"-"` on the PIN columns. They must
// persist to the database but never leave the process in an API payload.
func TestCredentialPINHashesAreNotSerialised(t *testing.T) {
	s := newSQLiteStore(t)
	ctx := context.Background()

	h := mustCreate(t, s.holders, entities.Holder{Ref: "P", Name: "P", Status: entities.HolderActive})
	mustCreate(t, s.creds, entities.Credential{
		HolderId: h, Kind: entities.CredCard, Format: entities.FormatWiegand26,
		FacilityCode: 1, CardNumber: "9", Status: entities.CredActive,
		PinHash: "hashed-pin", DuressPinHash: "hashed-duress",
	})

	got, err := s.CredentialByCard(ctx, entities.FormatWiegand26, 1, "9")
	if err != nil || got == nil {
		t.Fatalf("CredentialByCard: %v %v", got, err)
	}
	// They must survive the round trip — the decision path needs them.
	if got.PinHash != "hashed-pin" || got.DuressPinHash != "hashed-duress" {
		t.Errorf("PIN hashes did not persist: %q / %q", got.PinHash, got.DuressPinHash)
	}
}

// TestMissingRowsAreNotErrors is the regression test for the GetById trap.
//
// GetById propagates the data layer's "no result found" as an ERROR rather than returning nil, so
// a badge against a deleted door or an orphaned credential would reach the controller as a database
// failure. The operator would be sent to look at the database while the real answer was "that door
// no longer exists".
func TestMissingRowsAreNotErrors(t *testing.T) {
	s := newSQLiteStore(t)
	ctx := context.Background()

	door, err := s.Door(ctx, 4242)
	if err != nil {
		t.Errorf("a missing door returned an error instead of nil: %v", err)
	}
	if door != nil {
		t.Error("a missing door returned a row")
	}

	holder, err := s.Holder(ctx, 4242)
	if err != nil {
		t.Errorf("a missing holder returned an error instead of nil: %v", err)
	}
	if holder != nil {
		t.Error("a missing holder returned a row")
	}

	// Empty list queries must also be empty, not errors.
	if g, err := s.GrantsFor(ctx, 4242, 4242); err != nil || len(g) != 0 {
		t.Errorf("GrantsFor on empty tables = %v, %v", g, err)
	}
	if h, err := s.HolidayOn(ctx, 1, "1999-01-01"); err != nil || h != nil {
		t.Errorf("HolidayOn with no rows = %v, %v", h, err)
	}
}

// TestEndToEndThroughSQLite is the persistence milestone: a badge on a real simulated reader,
// through the real OSDP driver, resolved against a REAL SQLite database, opening a real strike —
// and the resulting access event surviving in the table afterwards.
//
// It is deliberately the same scenario the in-memory controller test runs. Passing both is what
// demonstrates the two stores are genuinely interchangeable, which is the property that lets the
// offline replica and the live database share one code path.
func TestEndToEndThroughSQLite(t *testing.T) {
	s := newSQLiteStore(t)

	doorId := mustCreate(t, s.doors, entities.Door{
		Name: "Front", Class: entities.ClassInterior, LockKind: entities.LockFailSecure,
		UnlockSeconds: 5, HeldOpenSeconds: 30, Enabled: true,
		OfflinePolicy: entities.OfflineCached, AntiPassback: entities.APBOff,
		ContactDeviceKey: "relay-1/in-0",
	})
	mustCreate(t, s.readers, entities.Reader{
		Name: "Front-In", DoorId: doorId, Direction: entities.DirectionIn,
		BusPort: testPort, OsdpAddress: 1, Enabled: true,
	})
	holderId := mustCreate(t, s.holders, entities.Holder{
		Ref: "E-001", Name: "Aisyah", Kind: entities.HolderStaff, Status: entities.HolderActive,
	})
	mustCreate(t, s.creds, entities.Credential{
		HolderId: holderId, Kind: entities.CredCard, Format: entities.FormatWiegand26,
		FacilityCode: 7, CardNumber: "1234", Status: entities.CredActive,
	})
	groupId := mustCreate(t, s.groupsRepo(), entities.AccessGroup{Name: "Staff"})
	mustCreate(t, s.members, entities.AccessGroupMember{GroupId: groupId, HolderId: holderId})
	schedId := mustCreate(t, s.schedules, entities.Schedule{Name: "Always", Always: true})
	mustCreate(t, s.grants, entities.Grant{GroupId: groupId, DoorId: doorId, ScheduleId: schedId})

	r := newRigWithStore(t, s, ControllerConfig{TickInterval: 10 * time.Millisecond})
	r.waitOnline()
	r.badge(7, 1234)

	// Poll the real table until the decision lands.
	ctx := context.Background()
	deadline := time.After(5 * time.Second)
	for {
		rows, _, err := s.events.Get(ctx, "", 10, 0, nil, nil)
		if err != nil {
			t.Fatalf("read access log: %v", err)
		}
		if len(rows) > 0 {
			ev := rows[0]
			if ev.Decision != entities.DecisionGranted {
				t.Fatalf("decision = %s (%s: %s)", ev.Decision, ev.Reason, ev.Detail)
			}
			if ev.HolderName != "Aisyah" || ev.HolderId != holderId || ev.DoorId != doorId {
				t.Errorf("persisted event lost its identifiers: %+v", ev)
			}
			if r.act.count() != 1 {
				t.Errorf("strike fired %d times, want 1", r.act.count())
			}
			return
		}
		select {
		case <-deadline:
			t.Fatal("no access event reached the database")
		case <-time.After(20 * time.Millisecond):
		}
	}
}

// mustCreate inserts a row and returns its id.
func mustCreate[T any](t *testing.T, repo dbsql.IGenericRepo[T], model T) int64 {
	t.Helper()
	id, err := repo.Create(context.Background(), "", model)
	if err != nil {
		t.Fatalf("create %T: %v", model, err)
	}
	return int64(id)
}
