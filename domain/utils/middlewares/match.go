package middlewares

import "strings"

// hostMatches reports whether requestHost is covered by allowedHost ("*" matches
// any). Shared by the rate-limit middleware (previously also by the retired RBAC
// middleware).
func hostMatches(allowedHost string, requestHost string) bool {
	allowedHost = normalizeHost(allowedHost)
	requestHost = normalizeHost(requestHost)
	return allowedHost == requestHost || allowedHost == "*"
}

// pathMatches reports whether requestPath is covered by allowedPath (exact or a
// prefix segment). An empty allowedPath matches nothing.
func pathMatches(allowedPath string, requestPath string) bool {
	allowedPath = strings.TrimRight(strings.TrimSpace(allowedPath), "/")
	requestPath = strings.TrimRight(strings.TrimSpace(requestPath), "/")
	if allowedPath == "" {
		return false
	}
	if requestPath == allowedPath {
		return true
	}
	return strings.HasPrefix(requestPath, allowedPath+"/")
}
