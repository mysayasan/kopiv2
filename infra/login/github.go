package login

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/mysayasan/kopiv2/domain/utils/middlewares"
	"golang.org/x/oauth2"
)

// GithubLogin struct
type GithubLogin struct {
	conf oauth2.Config
	auth middlewares.AuthMidware
}

// Create GithubLogin
func NewGithubLogin(conf OAuth2ConfigModel, auth middlewares.AuthMidware) *GithubLogin {
	return &GithubLogin{
		conf: GithubConfig(conf),
		auth: auth,
	}
}

func (m *GithubLogin) Login(w http.ResponseWriter, r *http.Request) {
	cookie, state, err := NewOAuthState("github", r.TLS != nil)
	if err != nil {
		http.Error(w, "oauth state generation failed", http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, cookie)
	http.Redirect(w, r, m.conf.AuthCodeURL(state), http.StatusSeeOther)
}

func (m *GithubLogin) Callback(r *http.Request) (*GitHubUserInfoModel, error) {
	if err := ValidateOAuthState(r, "github", r.URL.Query().Get("state")); err != nil {
		return nil, err
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		return nil, errors.New("oauth code is required")
	}
	token, err := m.conf.Exchange(r.Context(), code)
	if err != nil {
		return nil, fmt.Errorf("code-token exchange failed: %w", err)
	}

	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, "https://api.github.com/user", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("user data fetch failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("user data fetch failed with status %d", resp.StatusCode)
	}

	var userJson GitHubUserInfoModel
	if err := json.NewDecoder(resp.Body).Decode(&userJson); err != nil {
		return nil, fmt.Errorf("json parsing failed: %w", err)
	}

	return &userJson, nil
}
