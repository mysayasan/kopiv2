package middlewares

import "net/http"

// GreetMidware struct
type GreetMidware struct {
}

// Init
func NewGreet() *GreetMidware {
	return &GreetMidware{}
}

// Greet
func (m *GreetMidware) GreetHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Server", "r450k")
		next.ServeHTTP(w, r)
	})
}
