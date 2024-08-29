package middlewares

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"

	"github.com/golang-jwt/jwt/v5"
	enumauth "github.com/mysayasan/kopiv2/domain/enums/auth"
	"github.com/mysayasan/kopiv2/domain/models"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
)

// AuthMidware struct
type AuthMidware struct {
	secret string
}

// Create NewAuth
func NewAuth(secret string) *AuthMidware {
	return &AuthMidware{}
}

// Middleware function, which will be called for each request
func (m *AuthMidware) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokenString := r.Header.Get("Authorization")
		if tokenString == "" {
			w.WriteHeader(http.StatusUnauthorized)
			controllers.SendError(w, controllers.ErrPermission, "token not found")
			return
		}

		re := regexp.MustCompile(`(?i)bearer `)
		tokenString = re.ReplaceAllString(tokenString, "")

		token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method")
			}
			return []byte(m.secret), nil
		})

		if err != nil {
			controllers.SendError(w, controllers.ErrPermission, err.Error())
			return
		}

		if !token.Valid {
			controllers.SendError(w, controllers.ErrPermission, err.Error())
			return
		}

		jwtClaims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			controllers.SendError(w, controllers.ErrPermission, err.Error())
			return
		}

		tmp, _ := json.Marshal(jwtClaims)
		claims := &models.JwtCustomClaims{}
		_ = json.Unmarshal(tmp, claims)

		if claims == (&models.JwtCustomClaims{}) {
			controllers.SendError(w, controllers.ErrPermission, err.Error())
			return
		}

		ctx := context.WithValue(r.Context(), enumauth.Claims, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// Jwt Token
func (m *AuthMidware) JwtToken(claims models.JwtCustomClaims) (string, error) {
	// Create token
	jwtToken := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	// Generate encoded token and send it as response.
	t, err := jwtToken.SignedString([]byte(m.secret))
	if err != nil {
		return "", err
	}

	return t, nil
}
