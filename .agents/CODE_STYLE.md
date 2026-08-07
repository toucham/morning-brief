# Code Style & Naming Conventions

## Naming Fundamentals

| Category | Rule | Example |
|----------|------|---------|
| **Packages** | lowercase, single word, no underscores | `client`, `config`, `server` (not `Client`, `my_client`) |
| **Exported** | CamelCase | `NewClient()`, `Brief()`, `BriefResponse` |
| **Unexported** | camelCase | `newHandler()`, `briefResponse`, `parseConfig()` |
| **Constants** | CapitalCase (exported) / camelCase (unexported) | `DefaultPort`, `errNotFound` (never `UPPER_SNAKE_CASE`) |
| **Booleans** | Prefix with `is`, `has`, `can` | `isAlive`, `hasError` (not `alive`, `error`) |
| **Receivers** | Short, consistent (1-2 chars) | `c`, `s`, `cl` (same across all methods on type) |
| **Interfaces** | Often end in `-er`: `Reader`, `Writer`, `Generator` | `BriefGenerator`, `Closer` |

---

## Package Documentation

**Rule**: Every package must have a doc comment immediately above `package` clause (no blank line).

```go
// Package client provides an HTTP client for communicating with the morning-brief server
// and utilities for spawning and detecting a local server instance.
package client

// Package config defines configuration structs and XDG path helpers.
package config
```

---

## Function & Method Signatures

```go
// Good: Clear parameter types, context first, error last
func (c *Client) Brief(ctx context.Context, req *api.BriefRequest) (*api.BriefResponse, error)

func (s *Server) ListenAndServe() error

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

// Use SIGTERM to allow graceful shutdown; the server's signal handler will trigger
// http.Server.Shutdown() to clean up connections.
proc.Signal(syscall.SIGTERM)

// Bad: Restates code; unnecessary noise
// Check if process is alive
err := proc.Signal(syscall.Signal(0))

// Send SIGTERM signal
proc.Signal(syscall.SIGTERM)

// Also bad: Too verbose
// This function ensures that a local server is running. It first checks the lockfile
// to see if a server is already running. If one is, it returns the address. Otherwise...
func EnsureLocalServer(lockPath, logPath string) (string, error) { ... }
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

### One Concept Per File (Usually)
```
client.go          # Client struct, constructor, Brief()
lockfile.go        # Lockfile struct, Read/Write/IsAlive()
spawn.go           # EnsureLocalServer(), locateServerBinary(), waitForReady()
stop.go            # StopLocalServer()
```

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
func (c *Client) Stop() error { ... }

type Lockfile struct { ... }
func (lf *Lockfile) IsAlive() bool { ... }

// Bad: Inconsistent
func (client *Client) Brief(...) error { ... }
func (c *Client) Stop() error { ... }

func (l *Lockfile) IsAlive() bool { ... }
func (lockf *Lockfile) Remove() error { ... }
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
serverAddress := lockfile.Address
isServerAlive := lockfile.IsAlive()
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

---

## Tooling: Non-negotiable

- **`gofmt -l .`**: Before considering work done, run `gofmt` (or enable in your editor). Go formatting is not a style choice; it's enforced by the language itself.
- **`go vet ./...`**: Catch static errors before tests run.
- **`golangci-lint run`** (if configured): Additional lints; check if the project has a `.golangci.yml`.

These are required checks, not optional nice-to-haves.

---

## Summary

- **Packages**: lowercase, no underscores; exports CamelCase, unexports camelCase
- **Doc comments**: On every exported declaration and package
- **Receivers**: Short, consistent, 1-2 characters
- **Context**: Always first param (after receiver)
- **Errors**: Always last return value, wrapped with `fmt.Errorf("%w", ...)`
- **Guard clauses**: Early returns, avoid deep nesting
- **Comments**: Explain *why*, not *what*
- **Type assertions**: Always use comma-ok
- **Formatting**: `gofmt` is mandatory, not optional
