package login

import (
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/github"
	"golang.org/x/oauth2/google"
)

type Config struct {
	GoogleLoginConfig oauth2.Config
	GitHubLoginConfig oauth2.Config
	FacebookConfig    oauth2.Config
}

var AppConfig Config

func GoogleConfig(conf OAuth2ConfigModel) oauth2.Config {
	AppConfig.GoogleLoginConfig = oauth2.Config{
		RedirectURL:  conf.RedirectUrl,
		ClientID:     conf.ClientId,
		ClientSecret: conf.ClientSecret,
		Scopes:       conf.Scopes,
		Endpoint:     google.Endpoint,
	}

	return AppConfig.GoogleLoginConfig
}

func GithubConfig(conf OAuth2ConfigModel) oauth2.Config {
	AppConfig.GitHubLoginConfig = oauth2.Config{
		RedirectURL:  conf.RedirectUrl,
		ClientID:     conf.ClientId,
		ClientSecret: conf.ClientSecret,
		Scopes:       conf.Scopes,
		Endpoint:     github.Endpoint,
	}

	return AppConfig.GitHubLoginConfig
}

func FacebookConfig(conf OAuth2ConfigModel) oauth2.Config {
	AppConfig.FacebookConfig = oauth2.Config{
		RedirectURL:  conf.RedirectUrl,
		ClientID:     conf.ClientId,
		ClientSecret: conf.ClientSecret,
		Scopes:       conf.Scopes,
		Endpoint:     google.Endpoint,
	}

	return AppConfig.FacebookConfig
}
