package apis

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/mysayasan/kopiv2/domain/utils/middlewares"
	"github.com/mysayasan/kopiv2/infra/config"
)

// Asking the identity server whether a session it issued is still live.
//
// WHY THIS APP NEEDS IT. myseliasan is a relying party: at the end of the authorization-code
// flow it is handed myidsan's session id and then mints its OWN token, signed with its OWN
// key, cached under its OWN TTL — `sso.sessionTtlSeconds`, three days by default. Nothing in
// that path can notice a revocation that happens at myidsan. A live two-process bench watched
// an administrator revoke every session for an account, saw the session go 401 at myidsan, and
// then watched the same browser cookie keep working here with full access to the fleet.
//
// The check is rate-limited to once per `sso.policyCacheTtlSeconds` per session (30s by
// default) — a config knob that already existed and, until this, drove nothing whatsoever.
// See domain/utils/middlewares/revocation.go for the fail-open-on-unreachable reasoning.

// NewSessionRevocationChecker builds the checker for this app's SSO configuration, or nil when
// this install is not a relying party (no `sso.providerBaseUrl`) or is missing the client
// credentials the endpoint authenticates with. Nil means "behave exactly as before".
//
// client must already trust the identity server's certificate — pass the same one the token
// exchange uses (providerHTTPClient), or a self-signed intranet IdP is simply unreachable and
// every check fails open forever while looking configured.
func NewSessionRevocationChecker(cfg *config.AppConfigModel) *middlewares.RevocationChecker {
	if cfg == nil {
		return nil
	}
	// The SAME client the token exchange uses, so a self-signed intranet identity server is
	// trusted here exactly as it is there. Building a plain http.Client instead would make
	// every check fail (and therefore fail open) on precisely the deployments this suite
	// targets, while looking perfectly configured.
	client, err := providerHTTPClient(cfg)
	if err != nil || client == nil {
		return nil
	}
	base := strings.TrimRight(strings.TrimSpace(cfg.SSO.ProviderBaseURL), "/")
	clientID := strings.TrimSpace(cfg.SSO.ClientID)
	clientSecret := strings.TrimSpace(cfg.SSO.ClientSecret)
	if base == "" || clientID == "" || clientSecret == "" {
		return nil
	}

	interval := time.Duration(cfg.SSO.PolicyCacheTTLSeconds) * time.Second
	if interval <= 0 {
		interval = 30 * time.Second
	}

	ask := func(ctx context.Context, sessionId string) (bool, bool) {
		payload, err := json.Marshal(map[string]string{
			"sessionId": sessionId, "client_id": clientID, "client_secret": clientSecret,
		})
		if err != nil {
			return false, false
		}
		// A short deadline of its own: this runs inside a request the user is waiting on, and
		// a slow identity server must not become a slow fleet console. Its own context rather
		// than the request's, so a client that gives up does not leave the answer uncached and
		// force the next request to ask again.
		reqCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 4*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(reqCtx, http.MethodPost,
			base+"/api/auth/session-status", bytes.NewReader(payload))
		if err != nil {
			return false, false
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			return false, false
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			// A 4xx here means this app's own client credentials are wrong or the endpoint is
			// missing (an older myidsan). Both are misconfiguration, not evidence that a
			// user's session ended, so they are reported as "no answer" and fail open.
			return false, false
		}
		var body struct {
			Result struct {
				Active bool `json:"active"`
			} `json:"result"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			return false, false
		}
		return body.Result.Active, true
	}

	return middlewares.NewRevocationChecker(ask, interval)
}
