package apis

import (
	"net/http"
	"strings"

	"github.com/mysayasan/kopiv2/domain/utils/controllers"
)

// Telling a relying app that a session it is serving has been revoked here.
//
// THE PROBLEM THIS SOLVES. myidsan and a relying app are separate processes with separate
// caches. The relying app is handed myidsan's session id at the end of the authorization-code
// flow and then mints its OWN token, signed with its OWN key, and caches its own session entry
// under its own TTL — three days by default (`sso.sessionTtlSeconds`). Revoking at myidsan
// deletes myidsan's cache entry and nothing else, so a live bench watched an administrator
// revoke an account's sessions, saw the session go 401 at myidsan, and then watched the same
// browser cookie keep working at the fleet console. "Terminate this person's access" did not.
//
// WHY NOT `/api/sso/introspect`, which already exists for relying apps. Two reasons, and the
// first is fatal:
//
//  1. introspect takes a TOKEN and validates its signature with myidsan's key. The relying
//     app does not hold a myidsan token — it discarded the access token after the exchange and
//     issued its own. Asked about the relying app's own cookie, introspect answers
//     `{"active":false,"reason":"token signature is invalid"}`, which is correct and reads
//     exactly like a revoked session. A bench believed that answer for one run.
//  2. introspect is gated on `sso.internalToken`, a shared secret that a relying app is not
//     required to hold and which the SSO settings bundle does not carry. Requiring it would
//     make revocation propagation depend on an extra manual deployment step, which is the kind
//     of thing that silently does not get done.
//
// So this endpoint is keyed on the SESSION ID — which both sides already share, because the
// relying app reuses myidsan's verbatim — and authenticated with the client_id/client_secret
// pair the app already used to redeem its authorization code. No new configuration on either
// side, and nothing sensitive to store.
//
// WHAT IT DELIBERATELY DOES NOT DO. It answers only "is this session live", never who it
// belongs to. A relying app asking about a session id it was never given learns one bit, and
// an unknown id is reported exactly like a revoked one, so it is not an enumeration oracle
// for which sessions exist.

type sessionStatusRequest struct {
	SessionID    string `json:"sessionId"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
}

type sessionStatusResponse struct {
	Active bool `json:"active"`
}

func (m *federatedAuthApi) sessionStatus(w http.ResponseWriter, r *http.Request) {
	body := new(sessionStatusRequest)
	if err := decodeJSON(w, r, body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}

	client, _, err := m.loadClient(r.Context(), body.ClientID)
	if err != nil {
		controllers.SendError(w, controllers.ErrLimitedAccess, "client is not registered")
		return
	}
	if ok, _ := secretMatches(client.ClientSecretHash, body.ClientSecret); !ok {
		controllers.SendError(w, controllers.ErrLimitedAccess, "client secret not valid")
		return
	}

	sessionID := strings.TrimSpace(body.SessionID)
	if sessionID == "" {
		controllers.SendError(w, controllers.ErrBadRequest, "sessionId is required")
		return
	}

	// The CACHE is the authority, exactly as it is for myidsan's own requests. Asking the
	// session table instead would answer from the index, and the index is the half that was
	// already telling operators the right thing while the session kept working.
	active := false
	if m.store != nil {
		var entry sessionStatusCacheProbe
		found, err := m.store.Get(r.Context(), "sso:session:"+sessionID, &entry)
		if err != nil {
			// A cache this server cannot read is not evidence that a session is over, and
			// answering "revoked" here would sign the whole estate out during a Redis blip.
			// The relying app treats an error as "no answer" and keeps its own verdict.
			controllers.SendError(w, controllers.ErrInternalServerError, "session store unavailable")
			return
		}
		active = found && !entry.Revoked
	}
	controllers.SendResult(w, sessionStatusResponse{Active: active})
}

// sessionStatusCacheProbe mirrors the fields of middlewares.SessionCacheEntry this endpoint
// needs. Declared locally so reading a session's liveness does not pull the whole auth
// middleware's entry shape into a decision that only cares about two things.
type sessionStatusCacheProbe struct {
	Revoked bool `json:"revoked"`
}
