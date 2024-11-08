package login

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/mysayasan/kopiv2/domain/utils/middlewares"
)

// GoogleLogin struct
type GoogleLogin struct {
	conf OAuth2ConfigModel
	auth middlewares.AuthMidware
}

// Create GoogleLogin
func NewGoogleLogin(conf OAuth2ConfigModel, auth middlewares.AuthMidware) *GoogleLogin {
	return &GoogleLogin{
		conf: conf,
		auth: auth,
	}
}

func (m *GoogleLogin) Login(w http.ResponseWriter, r *http.Request) {

	url := AppConfig.GoogleLoginConfig.AuthCodeURL("randomstate")

	// w.WriteHeader(http.StatusSeeOther)
	http.Redirect(w, r, url, http.StatusSeeOther)
	// c.Status(fiber.StatusSeeOther)
	// c.Redirect(url)
	// return c.JSON(url)
}

func (m *GoogleLogin) Callback(state string, code string) (*GoogleUserInfoModel, error) {
	if state != "randomstate" {
		return nil, errors.New("states don't match")
	}

	googlecon := GoogleConfig(m.conf)

	token, err := googlecon.Exchange(context.Background(), code)
	if err != nil {
		return nil, errors.New("Code-Token Exchange Failed")
	}

	resp, err := http.Get("https://www.googleapis.com/oauth2/v2/userinfo?access_token=" + token.AccessToken)
	if err != nil {
		return nil, errors.New("user data fetch failed")
	}

	userData, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.New("json parsing failed")
	}

	var userJson GoogleUserInfoModel
	json.Unmarshal(userData, &userJson)

	return &userJson, nil

}
