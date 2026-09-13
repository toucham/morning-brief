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

// defaultShutdownTimeout bounds how long Serve/ListenAndServe wait for
// in-flight requests to drain after ctx is cancelled before forcing the
// listener closed.
const defaultShutdownTimeout = 5 * time.Second

// Config contains the server's tunable behavior.
type Config struct {
	// Addr is the TCP address ListenAndServe binds, e.g. "127.0.0.1:8080".
	// Ignored by Serve, which uses the caller-supplied net.Listener instead.
	Addr string

	// ShutdownTimeout bounds how long Serve/ListenAndServe wait for
	// in-flight requests to drain after ctx is cancelled. Zero uses
	// defaultShutdownTimeout.
	ShutdownTimeout time.Duration
}

// Server hosts the morning-brief HTTP API: it owns the configured
// http.Server (routes + middleware) and the shutdown timeout applied when
// Serve/ListenAndServe's context is cancelled.
type Server struct {
	log             *slog.Logger
	httpSrv         *http.Server
	shutdownTimeout time.Duration
}

// New creates a Server ready to Serve or ListenAndServe. If cfg is nil, a
// zero Config is used. If log is nil, slog.Default() is used.
func New(cfg *Config, log *slog.Logger) *Server {
	if cfg == nil {
		cfg = &Config{}
	}
	if log == nil {
		log = slog.Default()
	}

	shutdownTimeout := defaultShutdownTimeout
	if cfg.ShutdownTimeout > 0 {
		shutdownTimeout = cfg.ShutdownTimeout
	}

	s := &Server{
		log:             log,
		shutdownTimeout: shutdownTimeout,
	}

	s.httpSrv = &http.Server{
		Addr:              cfg.Addr,
		Handler:           s.handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	return s
}

// handler builds the server's HTTP handler: routes registered on a fresh
// ServeMux, wrapped with request-logging middleware.
func (s *Server) handler() http.Handler {
	mux := http.NewServeMux()
	s.registerRoutes(mux)
	return s.withLogging(mux)
}

// Serve accepts connections on ln, serving until ctx is cancelled, then
// drains in-flight requests before returning. Use this when the caller
// needs control over listener construction (e.g. binding an ephemeral
// port in tests). A listener/serve error (other than a clean shutdown) is
// returned immediately without waiting for ctx.
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	if s == nil {
		return errors.New("server is nil")
	}
	if ln == nil {
		return errors.New("listener is nil")
	}
	return s.serve(ctx, ln.Addr().String(), func() error { return s.httpSrv.Serve(ln) })
}

// ListenAndServe listens on the server's configured Addr (see Config) and
// serves until ctx is cancelled, then drains in-flight requests. It
// mirrors http.Server.ListenAndServe, which reads Addr off the receiver
// rather than taking it as an argument.
func (s *Server) ListenAndServe(ctx context.Context) error {
	if s == nil {
		return errors.New("server is nil")
	}
	if s.httpSrv.Addr == "" {
		return errors.New("listen address is empty")
	}
	return s.serve(ctx, s.httpSrv.Addr, s.httpSrv.ListenAndServe)
}

// serve runs listen in a goroutine, logs the bind address, and drains
// in-flight requests via Shutdown once ctx is cancelled or listen fails.
func (s *Server) serve(ctx context.Context, addr string, listen func() error) error {
	errCh := make(chan error, 1)
	go func() {
		defer close(errCh)
		if err := listen(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	s.log.Info("server listening", "addr", addr)

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
