# Testing Best Practices

## Testing Fundamentals

### Test File Organization
- **Location**: Same package as implementation; `client_test.go` tests `client.go`
- **Naming**: `func TestFunctionName(t *testing.T)` or `TestTypeName_MethodName`
- **Coverage**: Aim for >80% for core logic; focus on behavior, not lines

```go
// client_test.go
package client

import "testing"

func TestNewClient(t *testing.T) {
    c, err := NewClient("http://localhost:8787", nil)
    if err != nil {
        t.Fatalf("NewClient failed: %v", err)
    }
    if c == nil {
        t.Fatal("expected non-nil client")
    }
}

func TestClient_Brief(t *testing.T) {
    // Test the Brief method
}
```

---

## Table-Driven Tests

Use for functions with multiple input/output cases. Reduces boilerplate and improves clarity.

```go
// processAlive is a liveness hint only — ownership is additionally proven by
// the /healthz instance token (see ../docs/IMPLEMENTATION.md, Spawn / Detect / Stop).
func TestProcessAlive(t *testing.T) {
    tests := []struct {
        name string
        pid  int
        want bool
    }{
        {"own pid", os.Getpid(), true},
        {"negative pid", -1, false},
        {"zero pid", 0, false},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            if got := processAlive(tt.pid); got != tt.want {
                t.Errorf("processAlive(%d) = %v, want %v", tt.pid, got, tt.want)
            }
        })
    }
}
```

**Why**: Easy to add test cases; clear what's being tested; test names are descriptive.

---

## Testing with Interfaces & Mocks

### Lightweight Test Doubles
Define test doubles directly in test files (not a separate mock package). For HTTP clients that hold a concrete `*http.Client`, prefer `httptest.Server` over a mock — it exercises the real request/response path (URL joining, headers, decoding):

```go
// In client_test.go
func TestBrief(t *testing.T) {
    ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodPost || r.URL.Path != "/brief" {
            t.Errorf("got %s %s, want POST /brief", r.Method, r.URL.Path)
        }
        w.Header().Set("Content-Type", "application/json")
        fmt.Fprint(w, `{"generated_at":"2026-01-01T00:00:00Z","message":"test"}`)
    }))
    defer ts.Close()

    c, err := NewClient(ts.URL, nil)
    if err != nil {
        t.Fatalf("NewClient failed: %v", err)
    }
    resp, err := c.Brief(context.Background(), &api.BriefRequest{})
    if err != nil {
        t.Fatalf("Brief failed: %v", err)
    }
    if resp.Message != "test" {
        t.Errorf("got %q, want %q", resp.Message, "test")
    }
}
```

Use hand-rolled mock types (in `_test.go`) only for non-HTTP collaborators where a fake is genuinely needed (e.g. injected process/filesystem ops in the lifecycle code).

### Verify Mocks Were Called
For behavior verification (e.g., "was X called with Y?"):

```go
type mockBriefGenerator struct {
    calls []api.BriefRequest
    resp  *api.BriefResponse
}

func (m *mockBriefGenerator) Generate(ctx context.Context, req *api.BriefRequest) (*api.BriefResponse, error) {
    m.calls = append(m.calls, *req)
    return m.resp, nil
}

func TestServer_CallsGenerator(t *testing.T) {
    mock := &mockBriefGenerator{
        resp: &api.BriefResponse{Message: "test"},
    }
    
    srv := server.New(mock, config.ServerConfig{})
    resp, err := srv.handleBrief(&api.BriefRequest{})
    
    if len(mock.calls) != 1 {
        t.Errorf("expected 1 call, got %d", len(mock.calls))
    }
}
```

---

## Testing Errors

### Test Both Success and Failure Paths

Config contract (per ../docs/IMPLEMENTATION.md): **missing file is not an error** — load returns the zero value. Only malformed content is an error.

```go
func TestLoadConfig_Success(t *testing.T) {
    tmp := t.TempDir()
    path := filepath.Join(tmp, "client.yaml")
    os.WriteFile(path, []byte("server:\n  url: http://localhost:8787"), 0600)

    cfg, err := LoadClientConfigFrom(path)
    if err != nil {
        t.Fatalf("LoadClientConfigFrom failed: %v", err)
    }
    if cfg.Server.URL != "http://localhost:8787" {
        t.Errorf("got %q, want %q", cfg.Server.URL, "http://localhost:8787")
    }
}

func TestLoadConfig_MissingFileIsNotAnError(t *testing.T) {
    cfg, err := LoadClientConfigFrom(filepath.Join(t.TempDir(), "absent.yaml"))
    if err != nil {
        t.Fatalf("missing file must load as zero value, got error: %v", err)
    }
    if cfg != (ClientConfig{}) {
        t.Errorf("expected zero value, got %+v", cfg)
    }
}

func TestLoadConfig_Malformed(t *testing.T) {
    tmp := t.TempDir()
    path := filepath.Join(tmp, "client.yaml")
    os.WriteFile(path, []byte("server: [unclosed"), 0600)

    _, err := LoadClientConfigFrom(path)
    if err == nil {
        t.Fatal("expected error for malformed YAML, got nil")
    }
}
```

### Check Error Messages
```go
func TestProcessAlive_ValidAndInvalidPID(t *testing.T) {
    tests := []struct {
        name string
        pid  int
        want bool
    }{
        {"own pid", os.Getpid(), true},
        {"negative pid", -1, false},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            if got := processAlive(tt.pid); got != tt.want {
                t.Errorf("processAlive(%d) = %v, want %v", tt.pid, got, tt.want)
            }
        })
    }
}
```

---

## Testing Concurrency

### Race Detector
Run tests with `-race` flag to detect data races:
```bash
go test -race ./...
```

### Testing Goroutines
```go
func TestConcurrentAccess(t *testing.T) {
    srv := New(config.ServerConfig{}, "", slog.Default())
    
    // Spawn multiple goroutines
    done := make(chan error, 10)
    for i := 0; i < 10; i++ {
        go func(id int) {
            _, err := srv.Brief(context.Background(), &api.BriefRequest{})
            done <- err
        }(i)
    }
    
    // Collect results
    for i := 0; i < 10; i++ {
        if err := <-done; err != nil {
            t.Errorf("goroutine %d failed: %v", i, err)
        }
    }
}
```

---

## Testing File I/O & Temp Files

### Use `t.TempDir()` for Temporary Files
```go
func TestStateFile_RoundTrip(t *testing.T) {
    tmp := t.TempDir()  // Cleaned up automatically
    path := filepath.Join(tmp, "server.lock")

    sf := &StateFile{PID: 1234, Address: "127.0.0.1:8787", Token: "abc123"}
    if err := CreateStateFile(path, sf); err != nil {
        t.Fatalf("CreateStateFile failed: %v", err)
    }

    // no-replace publication: a second creation must conflict (EEXIST)
    if err := CreateStateFile(path, sf); err == nil {
        t.Fatal("expected no-replace conflict, got nil")
    }

    // Verify file was written
    data, err := os.ReadFile(path)
    if err != nil {
        t.Fatalf("ReadFile failed: %v", err)
    }

    var read StateFile
    if err := json.Unmarshal(data, &read); err != nil {
        t.Fatalf("Unmarshal failed: %v", err)
    }

    if read.PID != sf.PID || read.Token != sf.Token {
        t.Errorf("got %+v, want %+v", read, *sf)
    }
}
```

---

## Testing with Context

### Timeout Testing
```go
func TestBrief_Timeout(t *testing.T) {
    ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
    defer cancel()
    
    // Simulate slow operation
    time.Sleep(200 * time.Millisecond)
    
    _, err := c.Brief(ctx, &api.BriefRequest{})
    if err == nil || !errors.Is(err, context.DeadlineExceeded) {
        t.Errorf("expected context.DeadlineExceeded, got %v", err)
    }
}
```

### Cancellation Testing
```go
func TestBrief_Cancelled(t *testing.T) {
    ctx, cancel := context.WithCancel(context.Background())
    cancel()  // Cancel immediately
    
    _, err := c.Brief(ctx, &api.BriefRequest{})
    if err == nil || !errors.Is(err, context.Canceled) {
        t.Errorf("expected context.Canceled, got %v", err)
    }
}
```

---

## Subtests for Organization

Use `t.Run()` to organize related tests.

```go
func TestClient(t *testing.T) {
    t.Run("NewClient", func(t *testing.T) {
        c := NewClient("http://localhost:8787")
        if c == nil {
            t.Fatal("expected non-nil client")
        }
    })
    
    t.Run("Brief", func(t *testing.T) {
        // Sub-tests for Brief
        t.Run("success", func(t *testing.T) {
            // ...
        })
        t.Run("error", func(t *testing.T) {
            // ...
        })
    })
}
```

**Output**:
```
ok    client      0.005s
  === RUN TestClient
  === RUN TestClient/NewClient
  === RUN TestClient/Brief
  === RUN TestClient/Brief/success
  === RUN TestClient/Brief/error
```

---

## Benchmarking

Mark performance-critical paths with benchmarks.

```go
func BenchmarkProcessAlive(b *testing.B) {
    pid := os.Getpid()

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        processAlive(pid)
    }
}
```

**Run**: `go test -bench=. ./...`

---

## Test Helpers

Keep test code DRY with helper functions.

```go
func mustCreateClient(t *testing.T, baseURL string) *Client {
    c, err := NewClient(baseURL, nil)
    if err != nil {
        t.Fatalf("failed to create client: %v", err)
    }
    return c
}

func mustCreateStateFile(t *testing.T, path string, sf *StateFile) {
    if err := CreateStateFile(path, sf); err != nil {
        t.Fatalf("CreateStateFile failed: %v", err)
    }
}

// Use in tests
func TestSomething(t *testing.T) {
    c := mustCreateClient(t, "http://localhost:8787")
    sf := &StateFile{PID: os.Getpid(), Token: "test-token"}
    mustCreateStateFile(t, filepath.Join(t.TempDir(), "server.lock"), sf)
}
```

---

## Coverage

Check coverage:
```bash
go test -cover ./...
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

**Target**: >80% for core logic. Don't obsess over 100%—focus on meaningful tests, not line coverage.

---

## Before You're Done

Before marking work complete, run these checks alongside `go test ./...`:

```bash
go vet ./...
gofmt -l .
golangci-lint run  # if configured in the project
```

These are not optional. Tests passing is necessary but not sufficient — code must also be formatted and pass static checks.

---

## Summary

- **File organization**: Same package, `_test.go` suffix
- **Table-driven tests**: For multiple input/output cases
- **Mocks**: Lightweight, defined in test files
- **Error testing**: Both success and failure paths
- **Race detector**: `go test -race ./...`
- **Context**: Timeout and cancellation testing
- **Subtests**: `t.Run()` for organization
- **Helpers**: DRY test code with `must*` functions
- **Coverage**: Aim for >80%, focus on behavior
