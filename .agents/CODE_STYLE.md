# Code Style & Naming Conventions

## Naming Fundamentals

| Category | Rule | Example |
|----------|------|---------|
| **Packages** | lowercase, single word, no underscores | `client`, `config`, `server` (not `Client`, `my_client`) |
| **Exported** | CamelCase | `NewClient()`, `Brief()`, `BriefResponse` |
| **Unexported** | camelCase | `newHandler()`, `briefResponse`, `parseConfig()` |
| **Constants** | CapitalCase (exported) / camelCase (unexported) | `DefaultPort`, `errNotFound` (never `UPPER_SNAKE_CASE`) |
| **Booleans** | Prefer clear adjectives or predicates; `is`/`has`/`can` prefixes are fine when they make the predicate readable | `found`, `ready`, `isAlive`, `hasExplicitAddress` (never `hasError`/`isError` — return an `error` value instead) |
| **Receivers** | Short, consistent (1-2 chars) | `c`, `s`, `cl` (same across all methods on type) |
| **Interfaces** | Often end in `-er`: `Reader`, `Writer`, `Generator` | `BriefGenerator`, `Closer` |

---

## Initialisms

**Rule**: Initialisms (acronyms) must have consistent all-caps or all-lowercase casing throughout names. Never mix cases.

Common initialisms: `API`, `ASCII`, `CPU`, `ID`, `JSON`, `URL`, `URI`, `HTTP`, `HTTPS`, `TCP`, `UDP`, `IP`, `PID`, `SHA`, `TLS`.

```go
// Good: Consistent casing
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) // HTTP stays all-caps
var baseURL string         // URL all-caps
url := req.URL.String()    // url lowercase when it's the whole word
userID := "u123"           // ID all-caps
pid := os.Getpid()         // PID -> pid when the whole name is the word

// Bad: Mixed or inconsistent casing
func (s *Server) ServeHttp(w http.ResponseWriter, r *http.Request) // Http — should be HTTP
var base_url string        // no underscores in identifiers
var userId = 0             // should be userID
var userid = 0             // should be userID
var apiUrl = ""            // should be apiURL
```

**Rationale**: Mixed casing (`Url` vs `URL`) makes search and reading harder. Go's `gofmt` does not enforce this — the linter (and reviewers) do. The casing must be the same everywhere the initialism appears, including in the middle of a compound name (`apiURL`, not `apiUrl`).

---

## Package Documentation

**Rule**: Every package must have a doc comment immediately above `package` clause (no blank line).

```go
// Package client provides an HTTP client for communicating with the morning-brief server
// and utilities for spawning and detecting a local server instance.
package client

// Package config defines configuration structs and platform path helpers.
package config
```

---

## Function & Method Signatures

```go
// Good: Clear parameter types, context first, error last
func (c *Client) Brief(ctx context.Context, req *api.BriefRequest) (*api.BriefResponse, error)

func (s *Server) Serve(ctx context.Context, ln net.Listener) error

func LoadClientConfig() (config.ClientConfig, error)

// Bad: Unclear, no context, errors not last
func (c *Client) GetBrief(request BriefRequest) (resp BriefResponse, err error)

func LoadConfig() (Config, string)  // error as string, not type error
```

**Rule**: `context.Context` is always first parameter (after receiver). Errors always last.

---

## Comments: Explain Why, Not What

```go
// Good: Explains non-obvious decision
// Use Signal(0) to check if the process is alive without actually signaling it.
err := proc.Signal(syscall.Signal(0))

// Drain in-flight requests cleanly before exiting on cancellation.
if err := srv.Shutdown(shutdownCtx); err != nil {
    return err
}

// Bad: Restates code; unnecessary noise
// Check if process is alive
err := proc.Signal(syscall.Signal(0))

// Shutdown the server
if err := srv.Shutdown(shutdownCtx); err != nil {
    return err
}

// Also bad: Too verbose
// This function ensures that a local server is running. It first checks the state file
// to see if a server is already running. If one is, it returns the address. Otherwise...
func EnsureLocalServer(ctx context.Context, opts EnsureOpts) (EnsureResult, error) { ... }
```

**Less is more**: Document the package and exported functions. Skip comments on obvious code.

---

## Exported Declarations: Always Document

Every exported package, type, function, constant, and variable must have a doc comment.

```go
// Good
// Client handles HTTP communication with the morning-brief server.
type Client struct {
    baseURL string
    http    *http.Client
}

// NewClient creates a new Client for the given base URL.
func NewClient(baseURL string) *Client { ... }

// Brief fetches a briefing from the server.
func (c *Client) Brief(ctx context.Context, req *api.BriefRequest) (*api.BriefResponse, error) { ... }

// ErrNotRunning indicates the server is not currently running.
var ErrNotRunning = errors.New("server not running")

// DefaultAddress is the loopback address used by the local server.
const DefaultAddress = "127.0.0.1"

// Bad: Missing doc comments
type Client struct { ... }
func NewClient(baseURL string) *Client { ... }
```

---

## File & Type Organization

### File Structure
1. Package clause + doc comment
2. Imports
3. Constants
4. Variables
5. Main types (structs, interfaces)
6. Constructors
7. Methods (exported first, then unexported)
8. Helper functions

### File Size
Keep files ~400–600 lines. Split if a file grows beyond that.

---

## Nil Checks & Guard Clauses

**Rule**: Check and return early. Avoid deep nesting.

```go
// Good: Guard clauses
func (c *Client) Brief(ctx context.Context, req *api.BriefRequest) (*api.BriefResponse, error) {
    if c == nil {
        return nil, errors.New("client is nil")
    }
    if req == nil {
        return nil, errors.New("request is nil")
    }
    // ... real logic
    return resp, nil
}

// Bad: Deep nesting
func (c *Client) Brief(ctx context.Context, req *api.BriefRequest) (*api.BriefResponse, error) {
    if c != nil {
        if req != nil {
            // ... 10 levels deeper
        } else {
            return nil, errors.New("request is nil")
        }
    } else {
        return nil, errors.New("client is nil")
    }
}
```

---

## Receivers

**Rule**: Consistent, short abbreviation. Use pointer receiver for mutating types; value for immutable.

```go
// Good: Consistent receiver
type Client struct { ... }
func (c *Client) Brief(...) error { ... }
func (c *Client) Health(...) error { ... }

type Client struct { ... }
func (c *Client) Brief(ctx context.Context, req *api.BriefRequest) (*api.BriefResponse, error) { ... }
func (c *Client) Health(ctx context.Context, token string) error { ... }

// Bad: Inconsistent
func (client *Client) Brief(...) error { ... }
func (c *Client) Stop() error { ... }

func (cl *Client) Health(...) error { ... }
```

---

## Variable Names

### Narrow Scope (1-5 lines): Short names OK
```go
for i := 0; i < len(items); i++ { ... }
if err != nil { return err }
ctx, cancel := context.WithTimeout(parent, 5*time.Second)
```

### Broader Scope: Descriptive names
```go
configPath := config.ClientConfigPath()
serverAddress := stateFile.Address
isProcessAlive := processAlive(stateFile.PID)
```

---

## Blank Identifier

Use `_` intentionally for unused values. Don't ignore errors silently.

```go
// Good: Intentional ignore (rare)
_ = os.Remove(tempFile)  // cleanup, error ok

// Better: Check error
if err := os.Remove(tempFile); err != nil {
    log.Printf("failed to remove temp file: %v", err)
}

// Bad: Silent error swallowing
proc, _ := os.FindProcess(pid)  // What if this fails?
```

---

## String Formatting

```go
// Good: fmt.Errorf with %w for error wrapping
return fmt.Errorf("loadConfig(%s): %w", path, err)

// Good: Minimal, descriptive
fmt.Printf("starting server on %s\n", addr)

// Bad: Concatenation for errors
return errors.New("error: " + err.Error())

// Bad: Over-verbose logging
log.Printf("THE SERVER HAS STARTED ON ADDRESS: %v AT TIME: %v", addr, time.Now())
```

---

## Error Strings

**Rule**: Error strings should be lowercase and should not end with punctuation, since they are usually printed following other context. Use `fmt.Errorf("something bad")`, not `fmt.Errorf("Something bad.")`.

```go
// Good: lowercase, no trailing punctuation — reads well when embedded
return errors.New("state file is nil")
return fmt.Errorf("read state file %s: %w", path, err)

// Good: proper nouns / acronyms may start capitalized (e.g. an env var name)
return fmt.Errorf("MORNING_SERVER_BIN %s is not a regular file", bin)

// Bad: capitalized and/or trailing punctuation
return errors.New("State file is nil.")
return fmt.Errorf("Failed to read state file: %v", err)
```

**Why**: `log.Printf("loading %s: %v", name, err)` should not produce `loading config: Failed to read...` with a spurious capital mid-message. This does **not** apply to log messages, which are line-oriented and may be capitalized.

**Related**: Wrap with `fmt.Errorf("...: %w", err)` to preserve the chain (see [DESIGN_PATTERNS.md](DESIGN_PATTERNS.md#error-handling)).

---

## Type Assertions

Always use the comma-ok idiom to avoid panics.

```go
// Good: Safe
if closer, ok := reader.(io.Closer); ok {
    defer closer.Close()
}

// Good: Type switch for multiple types
switch v := value.(type) {
case int:
    fmt.Println("int:", v)
case string:
    fmt.Println("string:", v)
default:
    fmt.Println("unknown type")
}

// Bad: Panics if not io.Closer
closer := reader.(io.Closer)
defer closer.Close()
```

---

## Slice & Map Zero Values

**Rule**: Prefer a nil slice (`var s []T`) over a non-nil, zero-length literal (`s := []T{}`). When you know the capacity ahead of time, preallocate.

```go
// Good: nil slice for "no items"
var items []string

// Good: preallocate when capacity is known — avoids regrowth
items := make([]string, 0, len(names))
m := make(map[string]int, len(pairs))

// Bad: empty literal where nil is fine
items := []string{}

// Exception: JSON encoding — a nil slice marshals to null, an empty slice to []
// Use []string{} only when the API contract requires [] instead of null.
```

**Why**: `nil` and empty slices are functionally equivalent for `len`/`cap`/ranging, but nil is the idiomatic "nothing" value. Preallocating with a known capacity avoids repeated reallocation. Do not design APIs that distinguish nil from empty slices — it causes subtle bugs.

---

## Interface Compliance

**Rule**: Verify interface compliance at compile time where a type is expected to satisfy an interface as part of its contract.

```go
// Compile-time assertion: fails to build if *Server stops satisfying http.Handler.
var _ http.Handler = (*Server)(nil)

// For value-receiver types, use the zero value of the asserted type.
var _ fmt.Stringer = StateFile{}
```

**When**: Exported types that must implement a stdlib/third-party interface (e.g. `http.Handler`, `fmt.Stringer`, `io.Closer`), or any type that is part of a family of implementations of the same interface. **Skip** for internal one-off types where the interface is obvious.

---

## Goroutines & Concurrency

**Rule**: Make goroutine lifetimes obvious. Never spawn fire-and-forget goroutines without a clear exit path.

- Every goroutine must have a documented, reachable exit: `context.Context` cancellation, a closed channel, or an explicit `sync.WaitGroup`.
- Spawned goroutines must respect the passed `ctx` — check `ctx.Done()` or use context-aware operations.
- Synchronize exit where the parent must not return before the goroutine finishes (`sync.WaitGroup`, buffered channel, or `errgroup`).
- Prefer `sync.WaitGroup` or channels over global counters; guard shared state with `sync.Mutex` (or use `sync.Map` for high-churn read-heavy maps).
- Close channels only from the sender, and only once; receiving from a closed channel is fine, sending panics.

```go
// Good: goroutine lifetime is bounded and synchronized
func runWorker(ctx context.Context) error {
    done := make(chan struct{})
    go func() {
        defer close(done)
        for {
            select {
            case <-ctx.Done():
                return
            case task := <-tasks:
                process(task)
            }
        }
    }()
    // ...
    cancel()
    <-done // wait for the goroutine to actually exit
    return nil
}

// Bad: fire-and-forget — leaks, races, no exit guarantee
go process(task) // who stops this? when does main wait for it?
```

See [DESIGN_PATTERNS.md](DESIGN_PATTERNS.md#anti-patterns-to-avoid) for the full anti-pattern list (unbounded goroutines, concurrent map access).

---

## Tooling: Non-negotiable

- **`gofmt -l .`**: Before considering work done, run `gofmt` (or enable in your editor). Go formatting is not a style choice; it's enforced by the language itself.
- **`go vet ./...`**: Catch static errors before tests run.
- **`golangci-lint run`** (if configured): Additional lints; check if the project has a `.golangci.yml`.

These are required checks, not optional nice-to-haves.

---

## Summary

- **Packages**: lowercase, no underscores; exports CamelCase, unexports camelCase
- **Initialisms**: Consistent all-caps (`apiURL`, `ServeHTTP`, `userID`)
- **Booleans**: Clear adjectives/predicates; no boolean error flags (return `error`)
- **Doc comments**: On every exported declaration and package
- **Receivers**: Short, consistent, 1-2 characters
- **Context**: Always first param (after receiver)
- **Errors**: Always last return value; lowercase strings, no trailing punctuation; wrapped with `fmt.Errorf("%w", ...)`
- **Guard clauses**: Early returns, avoid deep nesting
- **Comments**: Explain *why*, not *what*
- **Slices/maps**: Prefer nil slice; preallocate known capacities
- **Interfaces**: Verify compliance at compile time (`var _ Iface = (*T)(nil)`)
- **Goroutines**: Obvious lifetimes; context-cancellable; synchronized exit
- **Type assertions**: Always use comma-ok
- **Formatting**: `gofmt` is mandatory, not optional
