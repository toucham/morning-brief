// Package server implements the morning-brief HTTP server: route
// registration, middleware, and the listen/serve/shutdown lifecycle.
package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"
)

// defaultShutdownTimeout bounds how long Serve waits for in-flight requests
// to drain after ctx is cancelled before forcing the listener closed.
const defaultShutdownTimeout = 5 * time.Second

// Config contains the fields for the server configuration.
type Config struct {
	Addr string
	Port int
}

// Server hosts the morning-brief HTTP API: it owns the configured
// http.Server (routes + middleware) and the shutdown timeout applied when
// Serve's context is cancelled.
type Server struct {
	cfg             Config
	log             *slog.Logger
	httpSrv         *http.Server
	shutdownTimeout time.Duration
}

// New creates a Server ready to Serve. If cfg is nil, a zero Config is
// used. If log is nil, slog.Default() is used.
func New(cfg *Config, log *slog.Logger) *Server {
	if cfg == nil {
		cfg = &Config{}
	}
	if log == nil {
		log = slog.Default()
	}

	s := &Server{
		cfg:             *cfg,
		log:             log,
		shutdownTimeout: defaultShutdownTimeout,
	}

	mux := http.NewServeMux()
	s.registerRoutes(mux)
	s.httpSrv = &http.Server{
		Handler:           s.withLogging(mux),
		ReadHeaderTimeout: 5 * time.Second,
	}
	return s
}

// Serve accepts connections on ln, serving until ctx is cancelled, then
// drains in-flight requests before returning. A listener/serve error
// (other than a clean shutdown) is returned immediately without waiting
// for ctx.
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	if s == nil {
		return errors.New("server is nil")
	}
	if ln == nil {
		return errors.New("listener is nil")
	}

	errCh := make(chan error, 1)
	go func() {
		defer close(errCh)
		if err := s.httpSrv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	s.log.Info("server listening", "addr", ln.Addr().String())

	select {
	case err := <-errCh:
		return fmt.Errorf("serve: %w", err)
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), s.shutdownTimeout)
	defer cancel()
	if err := s.httpSrv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	<-errCh
	s.log.Info("server stopped")
	return nil
}
