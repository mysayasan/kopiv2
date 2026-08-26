package apis

import (
	"net"
	"net/http"
	"time"

	"github.com/mysayasan/kopiv2/apps/myidsan/services"
	sharedentities "github.com/mysayasan/kopiv2/domain/entities"
	"github.com/mysayasan/kopiv2/domain/models"
)

// Indexing an issued session, for every path that issues one.
//
// A session that is not indexed cannot be listed and cannot be revoked. That is not a
// cosmetic gap: `/api/session-admin` is the surface an administrator uses when a laptop is
// stolen or somebody leaves, and against an unindexed session it answers
// `{"ok":true,"revoked":0}` — success, having done nothing.
//
// Three call sites issue a session on this server, and until a live bench went looking only
// ONE of them indexed what it issued:
//
//   - `loginApi.issueSessionCookies` — the JSON local login, LDAP, Kerberos, and the MFA /
//     WebAuthn completions. Indexed.
//   - `federatedAuthApi.issueProviderSession` — the SERVER-RENDERED login page at
//     `/api/auth/login`, which is where every relying app's SSO redirect lands. Not indexed.
//     This is the primary way anybody signs in to the suite, so in practice session
//     administration was blind to almost every real session: a bench signed a user in
//     through the full authorization-code flow, and both the user's own session list and the
//     administrator's listing for that account came back EMPTY while the user was demonstrably
//     signed in at the relying app.
//   - `loginApi.setOAuthSession` — the Google/GitHub callback. Also not indexed.
//
// Hence one helper rather than a third copy of the same six lines. The rule it encodes —
// pre-mint the id, then index what you issued — is the part that was missing, not the
// formatting.

// mintSessionId pre-generates the session id for a session about to be issued.
//
// IssueAuthCookies mints one itself when the claim is empty and never reports it back, so a
// caller that leaves it empty cannot know which session it just created. Pre-minting is what
// makes the session indexable at all.
func mintSessionId(claims *models.JwtCustomClaims, sessions services.ISessionService) error {
	if sessions == nil || claims == nil || claims.SessionId != "" {
		return nil
	}
	sessionId, err := newFederatedOpaqueToken()
	if err != nil {
		return err
	}
	claims.SessionId = sessionId
	return nil
}

// indexIssuedSession records a session that has just been issued.
//
// Best-effort by design, and the ordering matters: the session is already live by the time
// this runs, so a failure here must not undo it. The service swallows and logs its own write
// errors for that reason. The alternative — refusing a sign-in because the index is
// unavailable — is worse than an unlisted session.
func indexIssuedSession(
	r *http.Request,
	sessions services.ISessionService,
	userId int64,
	sessionId string,
	expiresAt time.Time,
	trustedProxies []*net.IPNet,
) {
	if sessions == nil || userId == 0 || sessionId == "" {
		return
	}
	ip, ua := auditContext(r, trustedProxies)
	sessions.Record(r.Context(), sharedentities.UserSession{
		SessionId:   sessionId,
		UserLoginId: userId,
		ExpiresAt:   expiresAt.Unix(),
		IpAddress:   ip,
		UserAgent:   ua,
		CreatedBy:   userId,
	})
}
