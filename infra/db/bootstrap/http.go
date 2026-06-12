package bootstrap

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// StatusHandler returns the latest bootstrap status as JSON.
func StatusHandler(statusFn func() Status) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(statusFn()); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}

// SetupPageHandler serves a minimal bootstrap status page.
func SetupPageHandler(statusFn func() Status) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		status := statusFn()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprintf(w, `<!doctype html>
<html lang="en">
<head>
	<meta charset="utf-8" />
	<meta name="viewport" content="width=device-width, initial-scale=1" />
	<title>Setup Status</title>
	<style>
		body { font-family: system-ui, sans-serif; max-width: 720px; margin: 40px auto; padding: 0 16px; line-height: 1.5; }
		code, pre { background: #f5f5f5; padding: 2px 6px; border-radius: 4px; }
		.ok { color: #0a7; }
	</style>
</head>
<body>
	<h1>Bootstrap Status</h1>
	<p>App: <code>%s</code></p>
	<p>Database: <code>%s</code></p>
	<p>Status: <strong class="ok">%s</strong></p>
	<pre id="json"></pre>
	<script>
		document.getElementById('json').textContent = JSON.stringify(%s, null, 2);
	</script>
</body>
</html>`, status.AppName, status.DatabaseName, status.Message, mustJSON(status))
	}
}

func mustJSON(v any) string {
	data, _ := json.Marshal(v)
	return string(data)
}
