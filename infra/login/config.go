package login

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/github"
	"golang.org/x/oauth2/google"
)

func GoogleConfig(conf OAuth2ConfigModel) oauth2.Config {
	return oauth2.Config{
		RedirectURL:  conf.RedirectUrl,
		ClientID:     conf.ClientId,
		ClientSecret: conf.ClientSecret,
		Scopes:       conf.Scopes,
		Endpoint:     google.Endpoint,
	}
}

func GithubConfig(conf OAuth2ConfigModel) oauth2.Config {
	return oauth2.Config{
		RedirectURL:  conf.RedirectUrl,
		ClientID:     conf.ClientId,
		ClientSecret: conf.ClientSecret,
		Scopes:       conf.Scopes,
		Endpoint:     github.Endpoint,
	}
}

func FacebookConfig(conf OAuth2ConfigModel) oauth2.Config {
	return oauth2.Config{
		RedirectURL:  conf.RedirectUrl,
		ClientID:     conf.ClientId,
		ClientSecret: conf.ClientSecret,
		Scopes:       conf.Scopes,
		Endpoint:     google.Endpoint,
	}
}

func NewOAuthState(provider string, secure bool) (*http.Cookie, string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return nil, "", err
	}

	state := base64.RawURLEncoding.EncodeToString(raw)
	return &http.Cookie{
		Name:     oauthStateCookieName(provider),
		Value:    state,
		Path:     "/api/callback/" + strings.ToLower(provider),
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int((5 * time.Minute).Seconds()),
	}, state, nil
}

func ValidateOAuthState(r *http.Request, provider string, state string) error {
	state = strings.TrimSpace(state)
	if state == "" {
		return errors.New("oauth state is required")
	}

	cookie, err := r.Cookie(oauthStateCookieName(provider))
	if err != nil {
		return errors.New("oauth state cookie is missing")
	}
	if cookie.Value != state {
		return errors.New("oauth state does not match")
	}
	return nil
}

func oauthStateCookieName(provider string) string {
	return "oauth_state_" + strings.ToLower(strings.TrimSpace(provider))
}
