package middlewares

import "net/http"

// CorsMidware struct
type CorsMidware struct {
}

// Init
func NewCors() *CorsMidware {
	return &CorsMidware{}
}

// Cors
func (m *CorsMidware) CorsHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		next.ServeHTTP(w, r)
	})
}
