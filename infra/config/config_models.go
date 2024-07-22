package config

import (
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
	"github.com/mysayasan/kopiv2/infra/login"
)

// App config
type AppConfigModel struct {
	Login *struct {
		Google *login.OAuth2ConfigModel `json:"google"`
	} `json:"login"`
	Jwt struct {
		Secret string `json:"secret" validate:"required"`
	} `json:"jwt"`
	FileStorage struct {
		Path string `json:"path" validate:"required"`
	} `json:"fileStorage" validate:"required"`
	AllowOrigin string `json:"allowOrigins" validate:"required"`
	Tls         struct {
		CertPath string `json:"certPath" validate:"required"`
		KeyPath  string `json:"keyPath" validate:"required"`
	} `json:"tls"`
	Db dbsql.DbConfigModel `json:"db"`
}
