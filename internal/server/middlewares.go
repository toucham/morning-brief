package server

import (
	"net/http"
	"time"
)

// withLogging wraps h, logging method, path, status, and duration for
// every request via the server's structured logger.
func (s *Server) withLogging(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		h.ServeHTTP(rec, r)
		s.log.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"duration", time.Since(start),
		)
	})
}

// statusRecorder captures the status code written by the wrapped handler
// so withLogging can report it after the handler returns.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

// WriteHeader records status before delegating to the wrapped ResponseWriter.
func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}
