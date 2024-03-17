package config

import (
	"github.com/mysayasan/kopiv2/infra/db"
	"github.com/mysayasan/kopiv2/infra/login"
)

// App config
type AppConfigModel struct {
	Login struct {
		Google login.OAuth2ConfigModel `json:"google"`
	} `json:"login"`
	Jwt struct {
		Secret string `json:"secret" validate:"required"`
	} `json:"jwt"`
	Tls struct {
		CertPath string `json:"cert_path" validate:"required"`
		KeyPath  string `json:"key_path" validate:"required"`
	} `json:"tls"`
	Db db.DbConfigModel `json:"db"`
}
