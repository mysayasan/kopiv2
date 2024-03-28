package login

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/gofiber/fiber/v2"
	"github.com/mysayasan/kopiv2/domain/utils/middlewares"
)

// GithubLogin struct
type GithubLogin struct {
	conf OAuth2ConfigModel
	auth middlewares.AuthMiddleware
}

// Create GithubLogin
func NewGithubLogin(conf OAuth2ConfigModel, auth middlewares.AuthMiddleware) *GithubLogin {
	return &GithubLogin{
		conf: conf,
		auth: auth,
	}
}

func (m *GithubLogin) Login(c *fiber.Ctx) error {

	url := AppConfig.GitHubLoginConfig.AuthCodeURL("randomstate")

	c.Status(fiber.StatusSeeOther)
	c.Redirect(url)
	return c.JSON(url)
}

func (m *GithubLogin) Callback(c *fiber.Ctx) error {

	state := c.Query("state")
	if state != "randomstate" {
		return c.SendString("States don't Match!!")
	}

	code := c.Query("code")

	githubcon := GithubConfig(m.conf)
	fmt.Println(code)

	token, err := githubcon.Exchange(context.Background(), code)
	if err != nil {
		return c.SendString("Code-Token Exchange Failed")
	}
	fmt.Println(token)

	resp, err := http.Get("https://api.github.com/user/repo?access_token=" + token.AccessToken)
	//resp, err := http.Get('Authorization: token my_access_token' https://api.github.com/user/repos)
	if err != nil {
		return c.SendString("User Data Fetch Failed")
	}
	fmt.Println(resp)

	userData, err := io.ReadAll(resp.Body)
	if err != nil {
		return c.SendString("JSON Parsing Failed")
	}

	return c.SendString(string(userData))

}
