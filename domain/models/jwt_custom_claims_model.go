package models

import "github.com/golang-jwt/jwt/v5"

type JwtCustomClaims struct {
	Id            int64  `json:"id" form:"id" query:"id" validate:"required"`
	Email         string `json:"email" form:"email" query:"email" validate:"required"`
	VerifiedEmail bool   `json:"verifiedEmail" form:"verifiedEmail" query:"verifiedEmail"`
	Name          string `json:"name" form:"name" query:"name" validate:"required"`
	GivenName     string `json:"givenName" form:"givenName" query:"givenName"`
	FamilyName    string `json:"familyName" form:"familyName" query:"familyName"`
	Picture       string `json:"picture" form:"picture" query:"picture"`
	RoleId        int64  `json:"roleId" form:"roleId" query:"roleId"`
	jwt.RegisteredClaims
}
