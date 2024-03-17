package middlewares

import (
	jwtware "github.com/gofiber/contrib/jwt"
	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
)

// AuthMiddleware struct
type AuthMiddleware struct {
	secret string
}

// Create NewAuth
func NewAuth(secret string) *AuthMiddleware {
	return &AuthMiddleware{}
}

// Jwt Handler
func (m *AuthMiddleware) JwtHandler() fiber.Handler {
	return jwtware.New(jwtware.Config{
		SigningKey: jwtware.SigningKey{Key: []byte(m.secret)},
	})
}

func (m *AuthMiddleware) JwtToken(claims JwtCustomClaimsModel) (string, error) {
	// Create token
	jwtToken := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	// Generate encoded token and send it as response.
	t, err := jwtToken.SignedString([]byte(m.secret))
	if err != nil {
		return "", err
	}

	return t, nil
}
