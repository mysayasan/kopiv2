package services

import (
	"context"
	"strings"
	"time"

	sharedentities "github.com/mysayasan/kopiv2/domain/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	"github.com/mysayasan/kopiv2/infra/cache"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
	"github.com/mysayasan/kopiv2/infra/telemetry"
)

// Session administration.
//
// The cache entry at sso:session:<sid> remains the authority: the auth middleware checks
// it on every request, and deleting it is what actually ends a session. This service keeps
// a database row alongside each one purely as an INDEX, because the cache is keyed by
// session id and therefore cannot answer "which sessions does this user have?" — the
// question an administrator revoking access, and a user reviewing their own devices, both
// need answered.
//
// Consequences of that split, which the code below depends on:
//
//   - Revoking deletes the cache entry FIRST and marks the row second. If the row update
//     fails the session is still dead, which is the safe direction to fail in. Doing it the
//     other way round would leave a row claiming "revoked" while the session still works.
//   - A row is never trusted on its own. Rows outlive their cache entries (a session that
//     simply expired leaves a row behind), so listings reconcile against the cache and
//     report anything missing as ended.

const sessionCachePrefixKey = "sso:session:"

// SessionView is one session as presented to an operator or to the owning user.
type SessionView struct {
	Id         int64  `json:"id"`
	SessionId  string `json:"sessionId"`
	UserId     int64  `json:"userLoginId"`
	IpAddress  string `json:"ipAddress"`
	UserAgent  string `json:"userAgent"`
	CreatedAt  int64  `json:"createdAt"`
	LastSeenAt int64  `json:"lastSeenAt"`
	ExpiresAt  int64  `json:"expiresAt"`
	// Active is the reconciled answer: the row is unrevoked, unexpired, AND its cache
	// entry still exists. A row alone never proves a live session.
	Active bool `json:"active"`
	// Current marks the session making the request, so the UI can label it and warn
	// before someone signs themselves out.
	Current bool `json:"current"`
}

// ISessionService records, lists and revokes SSO sessions.
type ISessionService interface {
	// Record persists a newly issued session. Best-effort: a failure to index a session
	// must never fail the sign-in that just succeeded.
	Record(ctx context.Context, s sharedentities.UserSession)
	// Touch refreshes LastSeenAt, so an idle session is distinguishable from a live one.
	Touch(ctx context.Context, sessionId string)
	// ListForUser returns a user's sessions, newest first, reconciled against the cache.
	ListForUser(ctx context.Context, userId int64, currentSessionId string) ([]SessionView, error)
	// Revoke ends one session. Returns whether a matching row was found.
	Revoke(ctx context.Context, sessionId string) (bool, error)
	// RevokeAllForUser ends every session belonging to a user except optionally one
	// (used by "sign out everywhere else"). Returns how many were ended.
	RevokeAllForUser(ctx context.Context, userId int64, exceptSessionId string) (int, error)
	// CountActive returns how many sessions the index currently marks live. Backs the
	// myidsan_sessions_active gauge; see PublishActiveSessions.
	CountActive(ctx context.Context) (uint64, error)
}

type sessionService struct {
	repo  dbsql.IGenericRepo[sharedentities.UserSession]
	store cache.Store
	logf  func(format string, args ...any)
}

func NewSessionService(
	repo dbsql.IGenericRepo[sharedentities.UserSession],
	store cache.Store,
	logf func(format string, args ...any),
) ISessionService {
	return &sessionService{repo: repo, store: store, logf: logf}
}

func (s *sessionService) Record(ctx context.Context, row sharedentities.UserSession) {
	if strings.TrimSpace(row.SessionId) == "" {
		return
	}
	now := time.Now().Unix()
	if row.CreatedAt == 0 {
		row.CreatedAt = now
	}
	row.LastSeenAt = now
	row.IsActive = true
	if _, err := s.repo.Create(ctx, "", row); err != nil && s.logf != nil {
		s.logf("session index write failed for user=%d: %v", row.UserLoginId, err)
	}
}

func (s *sessionService) Touch(ctx context.Context, sessionId string) {
	row, err := s.bySessionId(ctx, sessionId)
	if err != nil || row == nil {
		return
	}
	row.LastSeenAt = time.Now().Unix()
	if _, err := s.repo.UpdateById(ctx, "", *row); err != nil && s.logf != nil {
		s.logf("session touch failed for %s: %v", sessionId, err)
	}
}

func (s *sessionService) ListForUser(ctx context.Context, userId int64, currentSessionId string) ([]SessionView, error) {
	// Filter on the FK with an explicit Equal rather than GetByForeign: that helper is
	// hardcoded to return a single child row, which would silently show one session per
	// user and make "sign out everywhere" look like it had nothing to do.
	filters := []sqldataenums.Filter{
		{FieldName: "UserLoginId", Compare: sqldataenums.Equal, Value: userId},
	}
	sorters := []sqldataenums.Sorter{{FieldName: "Id", Sort: sqldataenums.DESC}}
	rows, _, err := s.repo.Get(ctx, "", 500, 0, filters, sorters)
	if err != nil {
		if isNotFoundErr(err) {
			return []SessionView{}, nil
		}
		return nil, err
	}

	now := time.Now().Unix()
	out := make([]SessionView, 0, len(rows))
	for _, row := range rows {
		if row == nil {
			continue
		}
		out = append(out, SessionView{
			Id:         row.Id,
			SessionId:  row.SessionId,
			UserId:     row.UserLoginId,
			IpAddress:  row.IpAddress,
			UserAgent:  row.UserAgent,
			CreatedAt:  row.CreatedAt,
			LastSeenAt: row.LastSeenAt,
			ExpiresAt:  row.ExpiresAt,
			Active:     s.isLive(ctx, row, now),
			Current:    row.SessionId == currentSessionId && currentSessionId != "",
		})
	}
	return out, nil
}

// isLive reconciles a row against the cache. A row is only reported active when it has not
// been revoked, has not expired, and its cache entry still exists — otherwise a session
// that simply timed out would keep appearing in a device list as though it were usable.
func (s *sessionService) isLive(ctx context.Context, row *sharedentities.UserSession, now int64) bool {
	if row.RevokedAt > 0 || !row.IsActive {
		return false
	}
	if row.ExpiresAt > 0 && row.ExpiresAt <= now {
		return false
	}
	if s.store == nil {
		return true
	}
	var entry map[string]any
	found, err := s.store.Get(ctx, sessionCachePrefixKey+row.SessionId, &entry)
	if err != nil {
		// Cache trouble is not proof the session is dead; fall back to the row's own
		// view rather than telling a user their live session has ended.
		return true
	}
	return found
}

// CountActive counts rows the index still marks active.
//
// The INDEX, not the cache: the cache remains the authority on whether any individual
// session is valid, but it cannot be enumerated, so a gauge has to come from the table.
// The two can differ briefly — a cache entry that expired on its own leaves a row still
// marked active until something reconciles it — which makes this a capacity and
// anomaly signal rather than an exact count, and it is documented that way.
func (s *sessionService) CountActive(ctx context.Context) (uint64, error) {
	_, total, err := s.repo.Get(ctx, "", 1, 0, []sqldataenums.Filter{
		{FieldName: "IsActive", Compare: sqldataenums.Equal, Value: true},
	}, nil)
	if err != nil {
		if isNotFoundErr(err) {
			return 0, nil
		}
		return 0, err
	}
	return total, nil
}

// PublishActiveSessions sets the gauge. Separate from CountActive so the scheduler task
// stays a one-liner and the count is testable without a metrics recorder.
func PublishActiveSessions(ctx context.Context, sessions ISessionService, m telemetry.Metrics) error {
	if sessions == nil || m == nil {
		return nil
	}
	total, err := sessions.CountActive(ctx)
	if err != nil {
		return err
	}
	m.Set(MetricSessionsActive, nil, float64(total))
	return nil
}

func (s *sessionService) Revoke(ctx context.Context, sessionId string) (bool, error) {
	sessionId = strings.TrimSpace(sessionId)
	if sessionId == "" {
		return false, nil
	}
	// Kill the session FIRST. If the bookkeeping below fails the session is already
	// unusable, which is the safe way round to fail.
	if s.store != nil {
		if err := s.store.Delete(ctx, sessionCachePrefixKey+sessionId); err != nil && s.logf != nil {
			s.logf("session cache delete failed for %s: %v", sessionId, err)
		}
	}

	row, err := s.bySessionId(ctx, sessionId)
	if err != nil || row == nil {
		return false, err
	}
	row.RevokedAt = time.Now().Unix()
	row.IsActive = false
	if _, err := s.repo.UpdateById(ctx, "", *row); err != nil {
		return true, err
	}
	return true, nil
}

func (s *sessionService) RevokeAllForUser(ctx context.Context, userId int64, exceptSessionId string) (int, error) {
	views, err := s.ListForUser(ctx, userId, exceptSessionId)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, v := range views {
		if v.SessionId == exceptSessionId && exceptSessionId != "" {
			continue
		}
		if !v.Active {
			continue
		}
		if ok, err := s.Revoke(ctx, v.SessionId); err != nil {
			return count, err
		} else if ok {
			count++
		}
	}
	return count, nil
}

func (s *sessionService) bySessionId(ctx context.Context, sessionId string) (*sharedentities.UserSession, error) {
	filters := []sqldataenums.Filter{
		{FieldName: "SessionId", Compare: sqldataenums.Equal, Value: strings.TrimSpace(sessionId)},
	}
	rows, _, err := s.repo.Get(ctx, "", 1, 0, filters, nil)
	if err != nil {
		if isNotFoundErr(err) {
			return nil, nil
		}
		return nil, err
	}
	if len(rows) == 0 || rows[0] == nil {
		return nil, nil
	}
	return rows[0], nil
}
