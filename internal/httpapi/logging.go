package httpapi

import (
	"log/slog"
	"net/http"
	"time"
)

type responseLog struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (w *responseLog) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *responseLog) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	if status >= 200 || status == http.StatusSwitchingProtocols {
		w.status = status
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *responseLog) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	n, err := w.ResponseWriter.Write(p)
	w.bytes += n
	return n, err
}

func (s *Server) logRequest(r *http.Request, w *responseLog, started time.Time) {
	status := w.status
	if status == 0 {
		status = http.StatusOK
	}
	method := r.Method
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions:
	default:
		method = "OTHER"
	}
	route := r.Pattern
	if route == "" {
		route = "unmatched"
	}
	level := slog.LevelInfo
	if status >= 500 {
		level = slog.LevelError
	} else if status >= 400 {
		level = slog.LevelWarn
	}
	// Route patterns contain placeholders, never account IDs, query parameters,
	// cookies, request bodies, client addresses, or upstream connection details.
	s.config.Logger.Log(r.Context(), level, "http_request", "component", "http",
		"method", method, "route", route, "status", status, "bytes", w.bytes,
		"duration_ms", time.Since(started).Milliseconds())
}
