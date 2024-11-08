package login

// Google User Info represents user profile
type GoogleUserInfoModel struct {
	Id            string `json:"id" form:"id" query:"id" validate:"required"`
	Email         string `json:"email" form:"email" query:"email" validate:"required"`
	VerifiedEmail bool   `json:"verified_email" form:"verified_email" query:"verified_email"`
	Name          string `json:"name" form:"name" query:"name" validate:"required"`
	GivenName     string `json:"given_name" form:"given_name" query:"given_name"`
	FamilyName    string `json:"family_name" form:"family_name" query:"family_name"`
	Picture       string `json:"picture" form:"picture" query:"picture"`
}

type OAuth2ConfigModel struct {
	ClientId     string   `json:"client_id" validate:"required"`
	ClientSecret string   `json:"client_secret" validate:"required"`
	RedirectUrl  string   `json:"redirect_url" validate:"required"`
	Scopes       []string `json:"scopes" validate:"required"`
}
