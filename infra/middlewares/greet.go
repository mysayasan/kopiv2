package middlewares

import (
	"net/http"
)

// GreetMiddleware struct
type GreetMiddleware struct {
}

// Init
func NewGreet() *GreetMiddleware {
	return &GreetMiddleware{}
}

// Greet
func (m *GreetMiddleware) Greet(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Server", "r450k")
		next.ServeHTTP(w, r)
	})
}
