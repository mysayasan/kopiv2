package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	mymatasanapp "github.com/mysayasan/kopiv2/apps/mymatasan/app"
	"github.com/mysayasan/kopiv2/infra/apphost"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

func main() {
	if err := apphost.Run(mymatasanapp.New()); err != nil {
		panic(err)
	}
}

// HealthCheckHandler is retained for app integration tests.
func HealthCheckHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, `{"alive": true}`)
}

// ReadinessCheckHandler is retained for app integration tests.
func ReadinessCheckHandler(db dbsql.IDbCrud) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		if err := db.Ping(ctx); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]any{"ok": false, "db": "down"})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{"ok": true, "db": "up"})
	}
}
