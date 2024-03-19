package sql

// DbConfig
type DbConfigModel struct {
	Host     string `json:"host" validate:"required"`
	Port     int    `json:"port" validate:"required"`
	User     string `json:"user" validate:"required"`
	Password string `json:"password" validate:"required"`
	DbName   string `json:"db_name" validate:"required"`
	SslMode  string `json:"ssl_mode" validate:"required"`
}
