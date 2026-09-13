package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestNew(t *testing.T) {
	tests := []struct {
		name string
		cfg  *Config
		log  *slog.Logger
	}{
		{"nil cfg and nil log use defaults", nil, nil},
		{"explicit cfg and explicit log", &Config{Addr: "127.0.0.1", Port: 8080}, slog.New(slog.NewTextHandler(io.Discard, nil))},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New(tt.cfg, tt.log)
			if s == nil {
				t.Fatal("expected non-nil Server")
			}
			if s.httpSrv == nil {
				t.Fatal("expected non-nil httpSrv")
			}
			if s.log == nil {
				t.Fatal("expected non-nil logger")
			}
		})
	}
}

func TestServer_Serve_NilInputs(t *testing.T) {
	ctx := context.Background()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer ln.Close()

	srv := New(nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

	t.Run("nil server receiver", func(t *testing.T) {
		var nilSrv *Server
		if err := nilSrv.Serve(ctx, ln); err == nil {
			t.Fatal("expected error on nil receiver, got nil")
		}
	})

	t.Run("nil listener", func(t *testing.T) {
		if err := srv.Serve(ctx, nil); err == nil {
			t.Fatal("expected error on nil listener, got nil")
		}
	})
}

func TestServer_Serve_HealthzThenShutdown(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen failed: %v", err)
	}

	srv := New(&Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Serve(ctx, ln)
	}()

	client := &http.Client{Timeout: 2 * time.Second}
	healthzURL := "http://" + ln.Addr().String() + "/healthz"

	resp, err := client.Get(healthzURL)
	if err != nil {
		t.Fatalf("GET /healthz failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var body healthzResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response body failed: %v", err)
	}
	if body.Status != "ok" {
		t.Errorf("got status %q, want %q", body.Status, "ok")
	}

	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Serve returned unexpected error on shutdown: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Serve did not exit within 3 seconds of context cancellation")
	}
}

func TestServer_Serve_ListenerError(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen failed: %v", err)
	}
	// Close immediately so Serve's initial listener call fails.
	if err := ln.Close(); err != nil {
		t.Fatalf("close failed: %v", err)
	}

	srv := New(&Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err = srv.Serve(ctx, ln)
	if err == nil {
		t.Fatal("expected error on closed listener, got nil")
	}
}
