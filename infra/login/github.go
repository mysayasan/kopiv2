package login

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"golang.org/x/oauth2"
)

// GithubLogin implements RedirectProvider via GitHub's OAuth2 user endpoint.
type GithubLogin struct {
	conf oauth2.Config
}

// Create GithubLogin
func NewGithubLogin(conf OAuth2ConfigModel) *GithubLogin {
	return &GithubLogin{
		conf: GithubConfig(conf),
	}
}

func (m *GithubLogin) Key() string {
	return "github"
}

func (m *GithubLogin) DisplayName() string {
	return "GitHub"
}

func (m *GithubLogin) Login(w http.ResponseWriter, r *http.Request) {
	cookie, state, err := NewOAuthState(m.Key(), r.TLS != nil)
	if err != nil {
		http.Error(w, "oauth state generation failed", http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, cookie)
	http.Redirect(w, r, m.conf.AuthCodeURL(state), http.StatusSeeOther)
}

func (m *GithubLogin) Callback(r *http.Request) (*Identity, error) {
	if err := ValidateOAuthState(r, m.Key(), r.URL.Query().Get("state")); err != nil {
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

	if userJson.Id == 0 {
		return nil, errors.New("github user response did not include an id")
	}

	name := strings.TrimSpace(userJson.Name)
	if name == "" {
		name = userJson.Login
	}

	return &Identity{
		Provider: m.Key(),
		Subject:  strconv.FormatInt(userJson.Id, 10),
		Email:    userJson.Email,
		// GitHub only exposes the account's verified public email on this endpoint.
		EmailVerified: true,
		Name:          name,
		GivenName:     name,
		Picture:       userJson.AvatarURL,
	}, nil
}
