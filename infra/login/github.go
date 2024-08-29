package login

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/mysayasan/kopiv2/domain/utils/middlewares"
)

// GithubLogin struct
type GithubLogin struct {
	conf OAuth2ConfigModel
	auth middlewares.AuthMidware
}

// Create GithubLogin
func NewGithubLogin(conf OAuth2ConfigModel, auth middlewares.AuthMidware) *GithubLogin {
	return &GithubLogin{
		conf: conf,
		auth: auth,
	}
}

func (m *GithubLogin) Login(w http.ResponseWriter, r *http.Request) {

	url := AppConfig.GitHubLoginConfig.AuthCodeURL("randomstate")
	http.Redirect(w, r, url, http.StatusSeeOther)
}

func (m *GithubLogin) Callback(state string, code string) error {

	if state != "randomstate" {
		return errors.New("states don't match")
	}
	githubcon := GithubConfig(m.conf)
	fmt.Println(code)

	token, err := githubcon.Exchange(context.Background(), code)
	if err != nil {
		return err
	}
	fmt.Println(token)

	resp, err := http.Get("https://api.github.com/user/repo?access_token=" + token.AccessToken)
	//resp, err := http.Get('Authorization: token my_access_token' https://api.github.com/user/repos)
	if err != nil {
		return err
	}
	fmt.Println(resp)

	// userData, err := io.ReadAll(resp.Body)
	// if err != nil {
	// 	return err
	// }

	return nil

}
