# Design Patterns & Best Practices

## Dependency Injection

**Rule**: Prefer concrete types by default. Go's proverb: "Don't design with interfaces, discover them." Only introduce an interface when you have proof — either a second real implementation or a genuine testing need.

### Constructor Injection (Standard Pattern, Concrete First)
```go
// Phase 1: Inject concrete types
package server

type Server struct {
    config config.ServerConfig
}

func NewServer(cfg config.ServerConfig) *Server {
    return &Server{config: cfg}
}

// If testing needs a mock HTTP client (discovered need):
// Define the interface IN the test, not the main code
package server_test

type mockHTTPClient struct {
    doFn func(*http.Request) (*http.Response, error)
}

func (m *mockHTTPClient) Do(req *http.Request) (*http.Response, error) {
    return m.doFn(req)
}

// Later (Phase 2+, with proof): If internal/llm needs multiple providers (Claude/OpenAI/Ollama)
package llm

type Provider interface {
    Generate(ctx context.Context, req *api.BriefRequest) (*api.BriefResponse, error)
}

package server

type Server struct {
    provider llm.Provider  // NOW justify an interface
}

func NewServer(provider llm.Provider, cfg config.ServerConfig) *Server {
    return &Server{provider: provider, config: cfg}
}
```

**Why**: Concrete-first keeps code simple until complexity is justified. Interfaces emerge from need, not speculation. When you do add one, it's clear to future readers that multiple implementations or testing trade-offs are involved.

---

## Interfaces: Design Principles

### Define Interfaces Where They're Used
```go
// Good: Consumer defines interface
package server
type Handler interface {
    ServeHTTP(w http.ResponseWriter, r *http.Request)
}

// Bad: Producer defines interface
package handlers
type Handler interface {
    ServeHTTP(w http.ResponseWriter, r *http.Request)
    DoSomethingElse()
}
package server
// Now server depends on handlers package; tight coupling
```

### Keep Interfaces Small (1-3 Methods)
```go
// Good: Focused
type Reader interface {
    Read(p []byte) (n int, err error)
}

type Closer interface {
    Close() error
}

// Bad: Monolithic
type AllOperations interface {
    Read(p []byte) (n int, err error)
    Write(p []byte) (n int, err error)
    Close() error
    Seek(offset int64, whence int) (int64, error)
    // ... 20 more methods
}
```

**Why**: Small interfaces compose; monolithic interfaces are hard to mock and implement.

### Satisfy Implicitly
Go interfaces are satisfied implicitly. No `implements` keyword needed.

```go
type Reader interface {
    Read(p []byte) (n int, err error)
}

type File struct { ... }
func (f *File) Read(p []byte) (n int, err error) { ... }

// File automatically satisfies Reader—no declaration needed
var r Reader = &File{}
```

---

## Error Handling

### Wrap Errors with Context
Add context as errors bubble up; allows tracing the error chain.

```go
// Layer 1: Low-level
func readFile(path string) ([]byte, error) {
    data, err := os.ReadFile(path)
    if err != nil {
        return nil, fmt.Errorf("readFile(%s): %w", path, err)
    }
    return data, nil
}

// Layer 2: Mid-level
func loadConfig(path string) (Config, error) {
    data, err := readFile(path)
    if err != nil {
        return nil, fmt.Errorf("loadConfig: %w", err)
    }
    return parseConfig(data), nil
}

// Layer 3: Caller sees full chain
cfg, err := loadConfig("config.yaml")
// Error: loadConfig: readFile(config.yaml): open config.yaml: no such file
```

### Use Sentinel Errors for Expected Failures
Define package-level errors for conditions callers need to check.

```go
var (
    ErrNotFound      = errors.New("not found")
    ErrUnauthorized  = errors.New("unauthorized")
    ErrAlreadyExists = errors.New("already exists")
)

func GetUser(id string) (*User, error) {
    user, ok := users[id]
    if !ok {
        return nil, ErrNotFound
    }
    return user, nil
}

// Caller can check
user, err := GetUser("123")
if errors.Is(err, ErrNotFound) {
    // User doesn't exist; handle gracefully
}
```

### Custom Error Types for Complex Context
For errors with extra fields, define a custom type.

```go
type ConfigError struct {
    Path string
    Err  error
}

func (e *ConfigError) Error() string {
    return fmt.Sprintf("config error at %s: %v", e.Path, e.Err)
}

func LoadServerConfig(path string) (ServerConfig, error) {
    data, err := os.ReadFile(path)
    if err != nil {
        return ServerConfig{}, &ConfigError{Path: path, Err: err}
    }
    // ...
}

// Caller can unwrap
cfg, err := LoadServerConfig("server.yaml")
if cfgErr, ok := err.(*ConfigError); ok {
    log.Printf("config problem at %s: %v", cfgErr.Path, cfgErr.Err)
}
```

### Don't Wrap Multiple Times at Same Layer
Avoid redundant wrapping.

```go
// Bad: Both functions wrap
func fetch(url string) ([]byte, error) {
    resp, err := http.Get(url)
    if err != nil {
        return nil, fmt.Errorf("fetch: %w", err)  // Wrap
    }
    return ioutil.ReadAll(resp.Body)
}

func process(url string) (Result, error) {
    data, err := fetch(url)
    if err != nil {
        return nil, fmt.Errorf("process: %w", err)  // Wrap again
    }
    return parseData(data), nil
}
// Error: process: fetch: ... (redundant)

// Good: Only wrap once, at the entry point
func process(url string) (Result, error) {
    data, err := fetch(url)
    if err != nil {
        return nil, err  // Don't wrap; fetch already has context
    }
    return parseData(data), nil
}
```

---

## Composition Over Inheritance

Go has no inheritance. Use composition and interfaces instead.

```go
// Good: Composition
type Server struct {
    handler http.Handler  // Compose behavior
    config  Config
}

// Good: Embedding for shared fields
type BaseLogger struct {
    level slog.Level
}

type FileLogger struct {
    BaseLogger  // Embed to inherit fields
    file       *os.File
}

func (fl *FileLogger) Log(msg string) {
    if fl.level <= slog.LevelInfo {
        fmt.Fprintf(fl.file, "%s\n", msg)
    }
}

// Bad: Attempting inheritance
type BaseService struct {
    config Config
}

type ChildService struct {
    Base *BaseService  // Doesn't inherit methods; just composition
}

func (cs *ChildService) Init() {
    // Can't call cs.Init() from BaseService; must call cs.Base.Init()
}
```

---

## Graceful Shutdown (Idiomatic Go 1.21+)

This pattern applies to `cmd/server` to enable `morning server stop` via SIGTERM. Use `signal.NotifyContext` to cancel operations cleanly.

```go
// cmd/server/main.go
package main

import (
    "context"
    "net/http"
    "os"
    "os/signal"
    "syscall"
    "time"
)

func main() {
    // Context that cancels on SIGINT or SIGTERM
    ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
    defer stop()

    // Start server in a goroutine
    httpSrv := &http.Server{Addr: ":8787", Handler: http.NewServeMux()}
    go func() {
        if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
            fmt.Fprintf(os.Stderr, "server error: %v\n", err)
        }
    }()

    // Wait for shutdown signal
    <-ctx.Done()

    // Graceful shutdown with timeout
    shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    
    if err := httpSrv.Shutdown(shutdownCtx); err != nil {
        fmt.Fprintf(os.Stderr, "shutdown error: %v\n", err)
    }
}
```

**Key points**:
- `signal.NotifyContext` converts SIGTERM/SIGINT into a context cancellation — no manual signal handling needed.
- `http.Server.Shutdown(ctx)` gracefully stops accepting new connections and waits for active handlers to finish.
- The timeout prevents the server from hanging forever — if shutdown takes >5s, it hard-stops anyway.
- This is what `morning server stop` (sending SIGTERM to the spawned process) triggers.

---

---

## Logging

**Rule**: Use `log/slog` (stdlib, Go 1.21+) for anything beyond a one-off CLI print. Avoid global loggers — pass context explicitly or attach a logger to types that need it.

```go
// Good: Construct once, pass explicitly
func main() {
    logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
    server := server.NewServer(cfg, logger)
    // ...
}

type Server struct {
    log *slog.Logger
    // ...
}

func (s *Server) Handle(w http.ResponseWriter, r *http.Request) {
    s.log.Info("request", "method", r.Method, "path", r.URL.Path)
}

// Good: One-off CLI output is OK with fmt
fmt.Println("server stopped")

// Bad: Don't mix fmt.Println and logging in the same flow
log.Printf("fetching...")  // inconsistent with slog
```

**Why**: Structured logging (`slog`) makes debugging easier (machine-readable fields, log levels) and is the modern Go standard. Avoid `fmt.Println` for operational events; reserve it for user-facing CLI output only.

---

## CLI Entrypoints

**Rule**: Keep `main()` thin and untestable; put real logic in a testable `run(ctx) error` function. This applies to both `cmd/cli` and `cmd/server`.

```go
// Good: cmd/cli/main.go
package main

import (
    "context"
    "os"
)

func main() {
    os.Exit(run(context.Background(), os.Args))
}

func run(ctx context.Context, args []string) int {
    cmd := cmd.NewRootCmd()  // Cobra setup here, testable
    if err := cmd.ExecuteContext(ctx); err != nil {
        return 1
    }
    return 0
}

// Good: cmd/server/main.go
package main

import (
    "context"
    "os"
)

func main() {
    os.Exit(run(context.Background()))
}

func run(ctx context.Context) int {
    cfg, err := config.Load()
    if err != nil {
        fmt.Fprintf(os.Stderr, "config error: %v\n", err)
        return 1
    }
    
    srv := server.New(cfg)
    if err := srv.ListenAndServe(ctx); err != nil {
        fmt.Fprintf(os.Stderr, "server error: %v\n", err)
        return 1
    }
    return 0
}

// Bad: Don't put logic in main()
func main() {
    // parsing, server setup, error handling all here
    // hard to test
}
```

**Why**: `main()` can't be tested in isolation. The `run` pattern keeps the entry point trivial and lets unit tests call `run` with a test `context.Context` or test config.

---

## Testing Patterns

### Table-Driven Tests
```go
func TestIsAlive(t *testing.T) {
    tests := []struct {
        name    string
        pid     int
        signal  os.Signal
        want    bool
    }{
        {"valid pid", 1234, syscall.Signal(0), true},
        {"invalid pid", -1, syscall.Signal(0), false},
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got := IsAlive(tt.pid, tt.signal)
            if got != tt.want {
                t.Errorf("got %v, want %v", got, tt.want)
            }
        })
    }
}
```

### Mock Interfaces
```go
// Test double in _test.go
type MockBriefGenerator struct {
    GenerateFn func(ctx context.Context, req *api.BriefRequest) (*api.BriefResponse, error)
}

func (m *MockBriefGenerator) Generate(ctx context.Context, req *api.BriefRequest) (*api.BriefResponse, error) {
    return m.GenerateFn(ctx, req)
}

// Use in test
func TestServer(t *testing.T) {
    mock := &MockBriefGenerator{
        GenerateFn: func(ctx context.Context, req *api.BriefRequest) (*api.BriefResponse, error) {
            return &api.BriefResponse{Message: "test"}, nil
        },
    }
    srv := server.NewServer(mock)
    // Test srv...
}
```

---

## Anti-Patterns to Avoid

| Anti-Pattern | Why | Fix |
|---|---|---|
| **Global state** | Hard to test, hidden dependencies | Use DI; pass state as parameters |
| **`init()` with side effects** | Order-dependent, untestable | Move logic to constructors |
| **Panic for expected errors** | Crashes the program | Return errors |
| **Catching all errors with `_`** | Silent failures | Log or handle deliberately |
| **Unbounded goroutines** | Resource leaks, unpredictable | Use channels/`sync.WaitGroup` |
| **Concurrent map access** | Race conditions, panics | Use `sync.Mutex` or `sync.Map` |
| **Creating `util/` packages** | Vague, not cohesive | Keep helpers in the package that uses them |

---

## Summary

- **Dependency Injection**: Concrete types first, interfaces only when discovered (multiple implementations or genuine test need)
- **Interfaces**: Define in consumer package, small (1-3 methods), satisfied implicitly — but wait to define until needed
- **Errors**: Wrapped with context using `fmt.Errorf("%w", ...)`, sentinel errors for expected failures
- **Composition**: Over inheritance; embed for shared behavior
- **Graceful Shutdown**: `signal.NotifyContext` + `http.Server.Shutdown(ctx)` with timeout — standard for any long-running server
- **Logging**: `log/slog` for structured logging, pass logger explicitly (not global), use `fmt` only for CLI output
- **CLI Entrypoints**: Thin `main()` that calls `run(ctx) error` — keeps entry point trivial and logic testable
- **Testing**: Table-driven tests, mock interfaces (defined in test files, not main code)
- **Avoid**: Global state, interface-first design, unbounded goroutines, silent error swallowing
