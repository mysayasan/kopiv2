package middlewares

import "github.com/golang-jwt/jwt/v5"

type JwtCustomClaimsModel struct {
	Id            int64  `json:"id" form:"id" query:"id" validate:"required"`
	Email         string `json:"email" form:"email" query:"email" validate:"required"`
	VerifiedEmail bool   `json:"verified_email" form:"verified_email" query:"verified_email"`
	Name          string `json:"name" form:"name" query:"name" validate:"required"`
	GivenName     string `json:"given_name" form:"given_name" query:"given_name"`
	FamilyName    string `json:"family_name" form:"family_name" query:"family_name"`
	Picture       string `json:"picture" form:"picture" query:"picture"`
	jwt.RegisteredClaims
}
