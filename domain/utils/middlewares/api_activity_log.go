package middlewares

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/domain/entities"
	"github.com/mysayasan/kopiv2/infra/telemetry"
)

type apiActivityLogService interface {
	Create(ctx context.Context, model entities.ApiLog) (uint64, error)
}

type activityLogger interface {
	Warnf(source string, format string, args ...any)
}

// ApiActivityLogMidware persists one ApiLog row for each API request.
type ApiActivityLogMidware struct {
	serv      apiActivityLogService
	auth      *AuthMidware
	logger    activityLogger
	appName   string
	telemetry telemetry.APIRecorder
}

func NewApiActivityLog(serv apiActivityLogService, auth *AuthMidware, logger activityLogger, options ...ApiActivityLogOption) *ApiActivityLogMidware {
	m := &ApiActivityLogMidware{
		serv:      serv,
		auth:      auth,
		logger:    logger,
		appName:   "unknown",
		telemetry: telemetry.NewNoopRecorder(),
	}
	for _, option := range options {
		if option != nil {
			option(m)
		}
	}
	if m.telemetry == nil {
		m.telemetry = telemetry.NewNoopRecorder()
	}
	return m
}

// ApiActivityLogOption customizes API activity logging behavior.
type ApiActivityLogOption func(*ApiActivityLogMidware)

// WithApiActivityAppName sets the app label used by telemetry.
func WithApiActivityAppName(appName string) ApiActivityLogOption {
	return func(m *ApiActivityLogMidware) {
		appName = strings.TrimSpace(appName)
		if appName != "" {
			m.appName = appName
		}
	}
}

// WithApiActivityTelemetry sets the telemetry recorder used for request observations.
func WithApiActivityTelemetry(recorder telemetry.APIRecorder) ApiActivityLogOption {
	return func(m *ApiActivityLogMidware) {
		if recorder != nil {
			m.telemetry = recorder
		}
	}
}

func (m *ApiActivityLogMidware) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK, start: start}

		next.ServeHTTP(sw, r)

		durationMs := time.Since(start).Milliseconds()
		m.telemetry.ObserveAPIRequest(telemetry.APIRequestMetric{
			AppName:    m.appName,
			Method:     r.Method,
			Path:       apiMetricPath(r),
			StatusCode: sw.status,
			DurationMs: durationMs,
		})

		if m.serv == nil {
			return
		}

		model := entities.ApiLog{
			StatsCode:  sw.status,
			DurationMs: durationMs,
			LogMsg:     fmt.Sprintf("api-activity method=%s path=%s dur_ms=%d user_agent=%q", r.Method, r.URL.Path, durationMs, r.UserAgent()),
			RequestUrl: r.URL.RequestURI(),
			CreatedBy:  m.createdBy(r),
			CreatedAt:  time.Now().UTC().Unix(),
		}

		clientIP := clientIPFromRequest(r)
		if strings.Contains(clientIP, ":") {
			model.ClientIpAddrV6 = clientIP
		} else {
			model.ClientIpAddrV4 = clientIP
		}

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if _, err := m.serv.Create(ctx, model); err != nil {
			if m.logger != nil {
				m.logger.Warnf("api-activity-log", "persist failed method=%s path=%s err=%v", r.Method, r.URL.Path, err)
				return
			}
			log.Printf("api-activity-log persist failed method=%s path=%s err=%v", r.Method, r.URL.Path, err)
		}
	})
}

func apiMetricPath(r *http.Request) string {
	if route := mux.CurrentRoute(r); route != nil {
		if path, err := route.GetPathTemplate(); err == nil && strings.TrimSpace(path) != "" {
			return path
		}
	}
	if r != nil && r.URL != nil {
		return r.URL.Path
	}
	return ""
}

func (m *ApiActivityLogMidware) createdBy(r *http.Request) int64 {
	if m.auth == nil {
		return 0
	}

	claims, err := m.auth.ClaimsFromRequest(r)
	if err != nil || claims == nil {
		return 0
	}
	return claims.Id
}

func clientIPFromRequest(r *http.Request) string {
	clientIP := strings.TrimSpace(r.Header.Get("X-Forwarded-For"))
	if i := strings.Index(clientIP, ","); i >= 0 {
		clientIP = strings.TrimSpace(clientIP[:i])
	}
	if clientIP != "" {
		return clientIP
	}

	clientIP = strings.TrimSpace(r.Header.Get("X-Real-IP"))
	if clientIP != "" {
		return clientIP
	}

	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err == nil {
		return host
	}
	return strings.TrimSpace(r.RemoteAddr)
}
