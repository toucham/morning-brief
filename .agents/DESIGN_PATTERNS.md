# Design Patterns & Best Practices

## Dependency Injection

**Rule**: High-level modules depend on abstractions (interfaces), not concrete implementations.

### Constructor Injection (Standard Pattern)
```go
// Consumer package defines what it needs
package server

type BriefGenerator interface {
    Generate(ctx context.Context, req *api.BriefRequest) (*api.BriefResponse, error)
}

type Server struct {
    gen    BriefGenerator
    config config.ServerConfig
}

// Constructor accepts dependencies as interfaces
func NewServer(gen BriefGenerator, cfg config.ServerConfig) *Server {
    return &Server{gen: gen, config: cfg}
}

// Producer can be in any package
package impl

type GeneratorImpl struct { ... }
func (g *GeneratorImpl) Generate(...) { ... }

// Wiring in cmd/
gen := &impl.GeneratorImpl{...}
srv := server.NewServer(gen, cfg)
```

**Why**: Decouples caller from implementation. Easy to test with mocks. Swap implementations later without changing consumer code.

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

## Concurrency Patterns

### Use Goroutines with Channels for Coordination
```go
// Bad: Uncoordinated goroutines
go func() {
    data := fetchData()  // No way to know when it's done
    // ...
}()

// Good: Channel for coordination
done := make(chan error)
go func() {
    data, err := fetchData()
    done <- err
}()

// Wait for completion
if err := <-done; err != nil {
    log.Fatal(err)
}

// Even better: Use context for cancellation
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

done := make(chan error, 1)
go func() {
    data, err := fetchData(ctx)
    done <- err
}()

select {
case err := <-done:
    if err != nil { log.Fatal(err) }
case <-ctx.Done():
    log.Fatal("timeout")
}
```

### Protect Shared State with Mutex
```go
type Server struct {
    mu    sync.Mutex
    state map[string]interface{}
}

func (s *Server) Set(key string, value interface{}) {
    s.mu.Lock()
    defer s.mu.Unlock()
    s.state[key] = value
}

func (s *Server) Get(key string) interface{} {
    s.mu.Lock()
    defer s.mu.Unlock()
    return s.state[key]
}
```

### Use `defer` for Cleanup
```go
// Good: Guaranteed cleanup
func (s *Server) ListenAndServe() error {
    listener, err := net.Listen("tcp", s.addr)
    if err != nil {
        return err
    }
    defer listener.Close()
    
    // listener is guaranteed to close, even on early return
    return http.Serve(listener, s.Handler())
}
```

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

- **Dependency Injection**: Constructor-based, interfaces in consumer packages
- **Interfaces**: Small (1-3 methods), satisfied implicitly
- **Errors**: Wrapped with context using `fmt.Errorf("%w", ...)`, sentinel errors for expected failures
- **Composition**: Over inheritance; embed for shared behavior
- **Concurrency**: Channels + goroutines, mutexes for shared state, `defer` for cleanup
- **Testing**: Table-driven tests, mock interfaces
- **Avoid**: Global state, unbounded goroutines, silent error swallowing
