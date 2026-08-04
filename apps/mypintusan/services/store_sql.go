package services

import (
	"context"
	"fmt"
	"strings"

	"github.com/mysayasan/kopiv2/apps/mypintusan/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// SQLStore is the database-backed Store: the production counterpart to the in-memory fake the
// controller tests run against. Because Store is an interface, the SAME decision path and the same
// controller run over either — which is what makes "works offline" a matter of swapping the store
// rather than maintaining a second code path that drifts from the first.

// maxRows bounds every list query. The generic repo takes an explicit limit and there is no
// "unbounded" value, so this is the deliberate ceiling rather than an accident: an access group
// with more members than this is a data-modelling problem, and silently returning a prefix of a
// holder's grants would deny access for reasons nobody could reproduce.
const maxRows = 5000

// SQLStore implements Store over the shared generic repository.
type SQLStore struct {
	readers   dbsql.IGenericRepo[entities.Reader]
	doors     dbsql.IGenericRepo[entities.Door]
	creds     dbsql.IGenericRepo[entities.Credential]
	holders   dbsql.IGenericRepo[entities.Holder]
	members   dbsql.IGenericRepo[entities.AccessGroupMember]
	grants    dbsql.IGenericRepo[entities.Grant]
	schedules dbsql.IGenericRepo[entities.Schedule]
	windows   dbsql.IGenericRepo[entities.ScheduleWindow]
	holidays  dbsql.IGenericRepo[entities.Holiday]
	events    dbsql.IGenericRepo[entities.AccessEvent]
	groups    dbsql.IGenericRepo[entities.AccessGroup]
}

// SQLStore must stay interchangeable with the in-memory fake the controller tests use, or the live
// database and the offline replica stop being the same code path.
var _ Store = (*SQLStore)(nil)

// groupsRepo exposes the access-group repository for administrative writes.
func (s *SQLStore) groupsRepo() dbsql.IGenericRepo[entities.AccessGroup] { return s.groups }

// NewSQLStore wires a store from one database handle.
func NewSQLStore(db dbsql.IDbCrud) *SQLStore {
	return &SQLStore{
		readers:   dbsql.NewGenericRepo[entities.Reader](db),
		doors:     dbsql.NewGenericRepo[entities.Door](db),
		creds:     dbsql.NewGenericRepo[entities.Credential](db),
		holders:   dbsql.NewGenericRepo[entities.Holder](db),
		members:   dbsql.NewGenericRepo[entities.AccessGroupMember](db),
		grants:    dbsql.NewGenericRepo[entities.Grant](db),
		schedules: dbsql.NewGenericRepo[entities.Schedule](db),
		windows:   dbsql.NewGenericRepo[entities.ScheduleWindow](db),
		holidays:  dbsql.NewGenericRepo[entities.Holiday](db),
		events:    dbsql.NewGenericRepo[entities.AccessEvent](db),
		groups:    dbsql.NewGenericRepo[entities.AccessGroup](db),
	}
}

// isNotFound reports whether an error is the data layer's "that row does not exist" signal.
//
// This exists because GetById does NOT treat a missing row as nil — despite a comment in the
// generic repo saying it does. Its nil check only covers the case where SelectById returns a nil
// map; on SQLite an empty result set comes back as an ERROR ("no result found") and is propagated
// straight through. Get, by contrast, swallows the same condition and returns an empty slice, so
// the two are inconsistent and only one of them matches its documentation.
//
// Left unhandled, a badge against a deleted door or an orphaned credential would surface as a
// DATABASE FAILURE rather than as a denial with a precise reason — the operator would be sent to
// look at the database while the actual answer was "that door no longer exists".
//
// The layer offers no typed sentinel and no exported helper, so matching the string is the only
// option available; it mirrors generic_repo.go's own unexported isNoResultErr, and is confined to
// this one function so there is a single place to fix if the data layer ever grows a real error type.
func isNotFound(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "no result found")
}

// eq is shorthand for a single equality filter.
func eq(field string, value any) sqldataenums.Filter {
	return sqldataenums.Filter{FieldName: field, Compare: sqldataenums.Equal, Value: value}
}

// first returns the single row a filtered lookup is expected to produce, or nil.
//
// It deliberately uses Get with an explicit filter rather than GetByForeign. GetByForeign passes a
// HARDCODED limit of 1 down to Select (see infra/db/sql/sqlite/db_crud_sel.go), so it silently
// returns at most ONE child row — fine for a lookup that is genuinely unique, catastrophic for a
// list, and indistinguishable between the two at the call site. Every child lookup in this file
// goes through Get so the row count is the query's business and not a hidden default's.
func first[T any](rows []*T) *T {
	if len(rows) == 0 {
		return nil
	}
	return rows[0]
}

func (s *SQLStore) ReaderByBus(ctx context.Context, busPort string, address int) (*entities.Reader, error) {
	rows, _, err := s.readers.Get(ctx, "", 2, 0, []sqldataenums.Filter{
		eq("BusPort", busPort),
		eq("OsdpAddress", address),
	}, nil)
	if err != nil {
		return nil, fmt.Errorf("reader at %s/%d: %w", busPort, address, err)
	}
	// Two readers claiming the same bus address is an enrolment error, and binding a door to
	// whichever row sorted first is exactly the silent misconfiguration this app cannot afford.
	if len(rows) > 1 {
		return nil, fmt.Errorf("%d readers are enrolled at %s address %d; the binding is ambiguous",
			len(rows), busPort, address)
	}
	return first(rows), nil
}

func (s *SQLStore) Door(ctx context.Context, id int64) (*entities.Door, error) {
	if id <= 0 {
		return nil, nil
	}
	// GetById, never GetByUnique(..., "id", ...). A unique-key lookup whose key group matches no
	// declared ukey falls through to an unfiltered select and returns the FIRST ROW IN THE TABLE —
	// the bug that once made everyone a superadmin elsewhere in this suite. Here it would open the
	// wrong door.
	door, err := s.doors.GetById(ctx, "", uint64(id))
	if isNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("door %d: %w", id, err)
	}
	return door, nil
}

func (s *SQLStore) CredentialByCard(ctx context.Context, format string, facility int, number string) (*entities.Credential, error) {
	// All three fields are part of the match key. Card 1234 exists in every facility code ever
	// issued, so a site with two card batches would otherwise open a door for the wrong person —
	// and the same number in two encodings is two different credentials.
	rows, _, err := s.creds.Get(ctx, "", 2, 0, []sqldataenums.Filter{
		eq("Format", format),
		eq("FacilityCode", facility),
		eq("CardNumber", number),
	}, nil)
	if err != nil {
		return nil, fmt.Errorf("credential %s/%d/%s: %w", format, facility, number, err)
	}
	if len(rows) > 1 {
		// Fail closed. A duplicate credential means two holders could be identified by one card,
		// and picking one is a coin toss over who the audit log blames.
		return nil, fmt.Errorf("%d credentials share format %s facility %d number %s",
			len(rows), format, facility, number)
	}
	return first(rows), nil
}

func (s *SQLStore) Holder(ctx context.Context, id int64) (*entities.Holder, error) {
	if id <= 0 {
		return nil, nil
	}
	holder, err := s.holders.GetById(ctx, "", uint64(id))
	if isNotFound(err) {
		// An orphaned credential — the holder was deleted but the card was not. The decision path
		// denies with `unknown-credential`, which is the honest answer.
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("holder %d: %w", id, err)
	}
	return holder, nil
}

// GrantsFor resolves the grants reaching a holder at a door, through their group memberships.
//
// Two queries rather than a join: the group set is small, the join spec on the generic repo is
// aimed at list projections rather than this shape, and an explicit second query is far easier to
// reason about when the question is "why did this door open?".
func (s *SQLStore) GrantsFor(ctx context.Context, holderId, doorId int64) ([]entities.Grant, error) {
	if holderId <= 0 || doorId <= 0 {
		return nil, nil
	}

	members, _, err := s.members.Get(ctx, "", maxRows, 0,
		[]sqldataenums.Filter{eq("HolderId", holderId)}, nil)
	if err != nil {
		return nil, fmt.Errorf("group membership for holder %d: %w", holderId, err)
	}
	if len(members) == 0 {
		return nil, nil
	}
	groupIds := make([]any, 0, len(members))
	for _, m := range members {
		groupIds = append(groupIds, m.GroupId)
	}

	rows, _, err := s.grants.Get(ctx, "", maxRows, 0, []sqldataenums.Filter{
		eq("DoorId", doorId),
		{FieldName: "GroupId", Compare: sqldataenums.In, Value: groupIds},
	}, nil)
	if err != nil {
		return nil, fmt.Errorf("grants for holder %d at door %d: %w", holderId, doorId, err)
	}

	out := make([]entities.Grant, 0, len(rows))
	for _, g := range rows {
		out = append(out, *g)
	}
	return out, nil
}

// Schedules loads the schedules and their windows for a set of ids.
func (s *SQLStore) Schedules(ctx context.Context, ids []int64) (map[int64]entities.Schedule, map[int64][]entities.ScheduleWindow, error) {
	scheds := map[int64]entities.Schedule{}
	windows := map[int64][]entities.ScheduleWindow{}
	if len(ids) == 0 {
		return scheds, windows, nil
	}

	// Deduplicate: several grants commonly point at the same schedule, and asking for it once per
	// grant turns a two-query lookup into an N-query one on the badge path.
	seen := map[int64]bool{}
	vals := make([]any, 0, len(ids))
	for _, id := range ids {
		if id > 0 && !seen[id] {
			seen[id] = true
			vals = append(vals, id)
		}
	}
	if len(vals) == 0 {
		return scheds, windows, nil
	}

	srows, _, err := s.schedules.Get(ctx, "", maxRows, 0,
		[]sqldataenums.Filter{{FieldName: "Id", Compare: sqldataenums.In, Value: vals}}, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("schedules: %w", err)
	}
	for _, sc := range srows {
		scheds[sc.Id] = *sc
	}

	wrows, _, err := s.windows.Get(ctx, "", maxRows, 0,
		[]sqldataenums.Filter{{FieldName: "ScheduleId", Compare: sqldataenums.In, Value: vals}}, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("schedule windows: %w", err)
	}
	for _, w := range wrows {
		windows[w.ScheduleId] = append(windows[w.ScheduleId], *w)
	}

	return scheds, windows, nil
}

// HolidayOn resolves the site's holiday for a local calendar date.
//
// A site-specific row wins over a global one (SiteId 0). Malaysian public holidays vary BY STATE,
// so a customer with offices in two states genuinely needs both calendars, and the more specific
// entry has to be the one that applies.
func (s *SQLStore) HolidayOn(ctx context.Context, siteId int64, date string) (*entities.Holiday, error) {
	if date == "" {
		return nil, nil
	}
	rows, _, err := s.holidays.Get(ctx, "", maxRows, 0,
		[]sqldataenums.Filter{eq("Date", date)}, nil)
	if err != nil {
		return nil, fmt.Errorf("holiday on %s: %w", date, err)
	}

	var global *entities.Holiday
	for _, h := range rows {
		if h.SiteId == siteId && siteId != 0 {
			return h, nil
		}
		if h.SiteId == 0 && global == nil {
			global = h
		}
	}
	return global, nil
}

// RecordEvent appends to the access log.
//
// The log is append-only and there is no update or delete path anywhere in this store, by design:
// it is the product. A denied unknown credential at 03:00 on a perimeter door is the single most
// valuable row this table will ever hold, and a system that can quietly rewrite it is not evidence
// of anything.
func (s *SQLStore) RecordEvent(ctx context.Context, ev entities.AccessEvent) error {
	if _, err := s.events.Create(ctx, "", ev); err != nil {
		return fmt.Errorf("record access event: %w", err)
	}
	return nil
}
