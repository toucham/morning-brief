package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/toucham/morning-brief/internal/api"
	"github.com/toucham/morning-brief/internal/config"
)

const testToken = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func newManagedServer(t *testing.T) *Server {
	t.Helper()
	return New(config.ServerConfig{}, testToken, nil)
}

func newManualServer(t *testing.T) *Server {
	t.Helper()
	return New(config.ServerConfig{}, "", nil)
}

func doGet(t *testing.T, url string, token string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func doPost(t *testing.T, url string, token string, body io.Reader) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, body)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func decodeError(t *testing.T, resp *http.Response) api.ErrorResponse {
	t.Helper()
	defer resp.Body.Close()
	var er api.ErrorResponse
	if err := json.NewDecoder(resp.Body).Decode(&er); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if er.Error == "" {
		t.Fatal("empty error message")
	}
	return er
}

func TestBriefEndpoint(t *testing.T) {
	srv := httptest.NewServer(newManagedServer(t).Handler())
	defer srv.Close()

	t.Run("POST returns stub brief", func(t *testing.T) {
		resp := doPost(t, srv.URL+"/brief", testToken, nil)
		defer resp.Body.Close()
		if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q", ct)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d", resp.StatusCode)
		}
		var br api.BriefResponse
		if err := json.NewDecoder(resp.Body).Decode(&br); err != nil {
			t.Fatal(err)
		}
		if br.Message != "stub brief: no real content yet" {
			t.Errorf("Message = %q", br.Message)
		}
		if br.GeneratedAt.IsZero() {
			t.Error("GeneratedAt is zero")
		}
	})

	t.Run("POST with empty JSON object is accepted", func(t *testing.T) {
		resp := doPost(t, srv.URL+"/brief", testToken, strings.NewReader("{}"))
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d", resp.StatusCode)
		}
	})

	t.Run("POST with malformed JSON is 400", func(t *testing.T) {
		resp := doPost(t, srv.URL+"/brief", testToken, strings.NewReader("{nope"))
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("status = %d", resp.StatusCode)
		}
		decodeError(t, resp)
	})

	t.Run("oversized body is 413", func(t *testing.T) {
		resp := doPost(t, srv.URL+"/brief", testToken, bytes.NewReader(make([]byte, maxBodyBytes+1)))
		if resp.StatusCode != http.StatusRequestEntityTooLarge {
			t.Fatalf("status = %d", resp.StatusCode)
		}
		decodeError(t, resp)
	})

	t.Run("GET is 405 JSON", func(t *testing.T) {
		resp := doGet(t, srv.URL+"/brief", testToken)
		if resp.StatusCode != http.StatusMethodNotAllowed {
			t.Fatalf("status = %d", resp.StatusCode)
		}
		er := decodeError(t, resp)
		if er.Error == "" {
			t.Error("empty error")
		}
	})
}

func TestHealthzManaged(t *testing.T) {
	srv := httptest.NewServer(newManagedServer(t).Handler())
	defer srv.Close()

	t.Run("no token is 401", func(t *testing.T) {
		resp := doGet(t, srv.URL+"/healthz", "")
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("status = %d", resp.StatusCode)
		}
		decodeError(t, resp)
	})

	t.Run("wrong token is 401", func(t *testing.T) {
		resp := doGet(t, srv.URL+"/healthz", "1"+testToken[1:])
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("status = %d", resp.StatusCode)
		}
		decodeError(t, resp)
	})

	t.Run("correct token echoes token and protocol version", func(t *testing.T) {
		resp := doGet(t, srv.URL+"/healthz", testToken)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d", resp.StatusCode)
		}
		var hr api.HealthResponse
		if err := json.NewDecoder(resp.Body).Decode(&hr); err != nil {
			t.Fatal(err)
		}
		if hr.Token != testToken {
			t.Errorf("Token = %q, want echo", hr.Token)
		}
		if hr.ProtocolVersion != api.ProtocolVersion {
			t.Errorf("ProtocolVersion = %d", hr.ProtocolVersion)
		}
	})

	t.Run("POST is 405", func(t *testing.T) {
		resp := doPost(t, srv.URL+"/healthz", testToken, nil)
		if resp.StatusCode != http.StatusMethodNotAllowed {
			t.Fatalf("status = %d", resp.StatusCode)
		}
		decodeError(t, resp)
	})
}

func TestHealthzManual(t *testing.T) {
	srv := httptest.NewServer(newManualServer(t).Handler())
	defer srv.Close()

	resp := doGet(t, srv.URL+"/healthz", "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var hr api.HealthResponse
	if err := json.NewDecoder(resp.Body).Decode(&hr); err != nil {
		t.Fatal(err)
	}
	if hr.Token != "" {
		t.Errorf("manual server token = %q, want empty", hr.Token)
	}
}

func TestShutdownManaged(t *testing.T) {
	s := newManagedServer(t)
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	t.Run("wrong token is 401 and does not shut down", func(t *testing.T) {
		resp := doPost(t, srv.URL+"/shutdown", "1"+testToken[1:], nil)
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("status = %d", resp.StatusCode)
		}
		decodeError(t, resp)
		select {
		case <-s.shutdown:
			t.Fatal("shutdown closed on 401")
		default:
		}
	})

	t.Run("correct token responds then closes shutdown", func(t *testing.T) {
		resp := doPost(t, srv.URL+"/shutdown", testToken, nil)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d", resp.StatusCode)
		}
		var m map[string]string
		if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
			t.Fatal(err)
		}
		if m["message"] != "shutting down" {
			t.Errorf("message = %q", m["message"])
		}
		select {
		case <-s.shutdown:
		case <-time.After(time.Second):
			t.Fatal("shutdown channel not closed")
		}
	})

	t.Run("repeated shutdown does not double-close", func(t *testing.T) {
		resp := doPost(t, srv.URL+"/shutdown", testToken, nil)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d", resp.StatusCode)
		}
	})

	t.Run("GET is 405", func(t *testing.T) {
		resp := doGet(t, srv.URL+"/shutdown", testToken)
		if resp.StatusCode != http.StatusMethodNotAllowed {
			t.Fatalf("status = %d", resp.StatusCode)
		}
		decodeError(t, resp)
	})
}

func TestShutdownManualForbidden(t *testing.T) {
	s := newManualServer(t)
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	resp := doPost(t, srv.URL+"/shutdown", "", nil)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	decodeError(t, resp)
	select {
	case <-s.shutdown:
		t.Fatal("manual server shutdown closed")
	default:
	}
}

func TestNotFoundIsJSON(t *testing.T) {
	srv := httptest.NewServer(newManagedServer(t).Handler())
	defer srv.Close()

	resp := doGet(t, srv.URL+"/nope", "")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q", ct)
	}
	decodeError(t, resp)
}

func TestServeReturnsOnShutdownRequest(t *testing.T) {
	s := newManagedServer(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	errCh := make(chan error, 1)
	go func() { errCh <- s.Serve(ctx, ln) }()

	// Wait for the listener to accept connections (any HTTP response, even a
	// 401, proves the server is up).
	deadline := time.Now().Add(2 * time.Second)
	for {
		resp, err := http.Get("http://" + ln.Addr().String() + "/healthz")
		if err == nil {
			resp.Body.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("server did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}

	resp := doPost(t, "http://"+ln.Addr().String()+"/shutdown", testToken, nil)
	resp.Body.Close()
	// Release pooled keep-alive sockets (from the /healthz poll and the
	// /shutdown call) so the server's Shutdown has no idle sockets to wait on.
	http.DefaultClient.CloseIdleConnections()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Serve() = %v, want nil", err)
		}
	case <-time.After(7 * time.Second):
		t.Fatal("Serve did not return after shutdown")
	}
}

func TestServeReturnsOnContextCancel(t *testing.T) {
	s := newManagedServer(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- s.Serve(ctx, ln) }()

	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Serve() = %v, want nil", err)
		}
	case <-time.After(7 * time.Second):
		t.Fatal("Serve did not return after ctx cancel")
	}
}

// readFailReader makes every Read fail, simulating a broken client connection
// mid-body.
type readFailReader struct{ err error }

func (r *readFailReader) Read(p []byte) (int, error) { return 0, r.err }

func TestBriefBodyReadErrorIs400(t *testing.T) {
	s := newManagedServer(t)
	req := httptest.NewRequest(http.MethodPost, "/brief", &readFailReader{err: errors.New("connection dropped")})
	req.Header.Set("Authorization", "Bearer "+testToken)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (a body read failure is not an oversize)", rec.Code)
	}
	var er api.ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &er); err != nil || er.Error == "" {
		t.Errorf("body = %q, want JSON error", rec.Body.String())
	}
}

func TestBriefOversizedBodyStays413(t *testing.T) {
	s := newManagedServer(t)
	req := httptest.NewRequest(http.MethodPost, "/brief", bytes.NewReader(make([]byte, maxBodyBytes+1)))
	req.Header.Set("Authorization", "Bearer "+testToken)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", rec.Code)
	}
}

func TestNilServerReceivers(t *testing.T) {
	var s *Server
	if err := s.Serve(context.Background(), nil); err == nil {
		t.Fatal("Serve on nil Server = nil, want error")
	}
	if err := newManagedServer(t).Serve(context.Background(), nil); err == nil {
		t.Fatal("Serve with nil listener = nil, want error")
	}

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("nil Handler() status = %d, want 503", rec.Code)
	}
}

func TestConcurrentShutdownRequests(t *testing.T) {
	s := newManagedServer(t)
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	const n = 16
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		go func() {
			req, err := http.NewRequest(http.MethodPost, srv.URL+"/shutdown", nil)
			if err != nil {
				errs <- err
				return
			}
			req.Header.Set("Authorization", "Bearer "+testToken)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				errs <- err
				return
			}
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				errs <- fmt.Errorf("unexpected status %d", resp.StatusCode)
				return
			}
			errs <- nil
		}()
	}
	for i := 0; i < n; i++ {
		if err := <-errs; err != nil {
			t.Fatalf("concurrent shutdown: %v", err)
		}
	}
	select {
	case <-s.shutdown:
	case <-time.After(time.Second):
		t.Fatal("shutdown channel not closed")
	}
}
