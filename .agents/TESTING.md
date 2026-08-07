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
    c := NewClient("http://localhost:8787")
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
func TestIsAlive(t *testing.T) {
    tests := []struct {
        name    string
        pid     int
        want    bool
    }{
        {"valid pid", 1234, true},
        {"negative pid", -1, false},
        {"zero pid", 0, false},
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got := IsAlive(tt.pid)
            if got != tt.want {
                t.Errorf("IsAlive(%d) = %v, want %v", tt.pid, got, tt.want)
            }
        })
    }
}
```

**Why**: Easy to add test cases; clear what's being tested; test names are descriptive.

---

## Testing with Interfaces & Mocks

### Lightweight Mock Implementation
Define test doubles directly in test files (not separate mock package).

```go
// In client_test.go
type mockHTTPClient struct {
    doFn func(req *http.Request) (*http.Response, error)
}

func (m *mockHTTPClient) Do(req *http.Request) (*http.Response, error) {
    return m.doFn(req)
}

// Use in tests
func TestBrief(t *testing.T) {
    mock := &mockHTTPClient{
        doFn: func(req *http.Request) (*http.Response, error) {
            return &http.Response{
                StatusCode: 200,
                Body:       ioutil.NopCloser(bytes.NewReader([]byte(`{"message":"test"}`))),
            }, nil
        },
    }
    
    c := &Client{http: mock}
    resp, err := c.Brief(context.Background(), &api.BriefRequest{})
    if err != nil {
        t.Fatalf("Brief failed: %v", err)
    }
    if resp.Message != "test" {
        t.Errorf("got %q, want %q", resp.Message, "test")
    }
}
```

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
    
    srv := server.NewServer(mock)
    resp, err := srv.handleBrief(&api.BriefRequest{})
    
    if len(mock.calls) != 1 {
        t.Errorf("expected 1 call, got %d", len(mock.calls))
    }
}
```

---

## Testing Errors

### Test Both Success and Failure Paths
```go
func TestLoadConfig_Success(t *testing.T) {
    // Write a temp config file
    tmp := t.TempDir()
    path := filepath.Join(tmp, "config.yaml")
    ioutil.WriteFile(path, []byte("server:\n  url: http://localhost:8787"), 0644)
    
    cfg, err := LoadClientConfig(path)
    if err != nil {
        t.Fatalf("LoadClientConfig failed: %v", err)
    }
    if cfg.Server.URL != "http://localhost:8787" {
        t.Errorf("got %q, want %q", cfg.Server.URL, "http://localhost:8787")
    }
}

func TestLoadConfig_NotFound(t *testing.T) {
    cfg, err := LoadClientConfig("/nonexistent/path")
    if err == nil {
        t.Fatal("expected error, got nil")
    }
    // Optionally check error type
    if !errors.Is(err, os.ErrNotExist) {
        t.Errorf("expected os.ErrNotExist, got %v", err)
    }
}
```

### Check Error Messages
```go
func TestIsAlive_InvalidPID(t *testing.T) {
    lf := &Lockfile{PID: -1}
    
    // If IsAlive returns an error
    if err := lf.IsAlive(); err != nil {
        if !strings.Contains(err.Error(), "invalid pid") {
            t.Errorf("unexpected error message: %v", err)
        }
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
    srv := NewServer()
    
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
func TestWriteLockfile(t *testing.T) {
    tmp := t.TempDir()  // Cleaned up automatically
    path := filepath.Join(tmp, "server.lock")
    
    lf := &Lockfile{PID: 1234, Address: "127.0.0.1:8787"}
    if err := WriteLockfile(path, lf); err != nil {
        t.Fatalf("WriteLockfile failed: %v", err)
    }
    
    // Verify file was written
    data, err := ioutil.ReadFile(path)
    if err != nil {
        t.Fatalf("ReadFile failed: %v", err)
    }
    
    var read Lockfile
    if err := json.Unmarshal(data, &read); err != nil {
        t.Fatalf("Unmarshal failed: %v", err)
    }
    
    if read.PID != lf.PID {
        t.Errorf("PID mismatch: got %d, want %d", read.PID, lf.PID)
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
func BenchmarkIsAlive(b *testing.B) {
    lf := &Lockfile{PID: os.Getpid()}
    
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        lf.IsAlive()
    }
}
```

**Run**: `go test -bench=. ./...`

---

## Test Helpers

Keep test code DRY with helper functions.

```go
func mustCreateClient(t *testing.T, baseURL string) *Client {
    c := NewClient(baseURL)
    if c == nil {
        t.Fatal("failed to create client")
    }
    return c
}

func mustWriteLockfile(t *testing.T, path string, lf *Lockfile) {
    if err := WriteLockfile(path, lf); err != nil {
        t.Fatalf("WriteLockfile failed: %v", err)
    }
}

// Use in tests
func TestSomething(t *testing.T) {
    c := mustCreateClient(t, "http://localhost:8787")
    lf := &Lockfile{...}
    mustWriteLockfile(t, "/tmp/test.lock", lf)
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
