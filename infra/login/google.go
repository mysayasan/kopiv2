package login

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/mysayasan/kopiv2/domain/utils/middlewares"
	"golang.org/x/oauth2"
)

// GoogleLogin struct
type GoogleLogin struct {
	conf oauth2.Config
	auth middlewares.AuthMidware
}

// Create GoogleLogin
func NewGoogleLogin(conf OAuth2ConfigModel, auth middlewares.AuthMidware) *GoogleLogin {
	return &GoogleLogin{
		conf: GoogleConfig(conf),
		auth: auth,
	}
}

func (m *GoogleLogin) Login(w http.ResponseWriter, r *http.Request) {
	cookie, state, err := NewOAuthState("google", r.TLS != nil)
	if err != nil {
		http.Error(w, "oauth state generation failed", http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, cookie)
	http.Redirect(w, r, m.conf.AuthCodeURL(state), http.StatusSeeOther)
}

func (m *GoogleLogin) Callback(r *http.Request) (*GoogleUserInfoModel, error) {
	if err := ValidateOAuthState(r, "google", r.URL.Query().Get("state")); err != nil {
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

	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, "https://www.googleapis.com/oauth2/v2/userinfo", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("user data fetch failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("user data fetch failed with status %d", resp.StatusCode)
	}

	var userJson GoogleUserInfoModel
	if err := json.NewDecoder(resp.Body).Decode(&userJson); err != nil {
		return nil, fmt.Errorf("json parsing failed: %w", err)
	}

	return &userJson, nil
}
