# Architecture & Project Structure

## Directory Layout

```
cmd/
  cli/          # CLI entrypoint (Cobra commands, flag parsing)
  server/       # Server entrypoint (HTTP listener setup)
internal/
  api/          # Wire types: BriefRequest, BriefResponse
  client/       # HTTP client, spawn/detect, lockfile, server management
  config/       # Config structs, XDG paths, load/save
  server/       # HTTP handlers, routing, server lifecycle
```

**Rule**: `cmd/` contains only entrypoints and command wiring. All logic lives in `internal/`.

---

## When to Create a New Package

### Create a new `internal/` package when:
1. **Clear single responsibility**: Package does one thing (e.g., `client` = talking to server, `config` = loading config)
2. **Imported by multiple places**: Avoids duplication
3. **Testable in isolation**: Can be tested without bringing in unrelated code

### Keep in existing package when:
- Logic is tightly coupled (e.g., lockfile handling stays in `client/` since spawn/detect need it)
- Fewer than 2-3 callers (not worth the package overhead)
- Still under ~600 lines (file is more natural boundary than package)

### Example: When to split
```
internal/client/
  client.go        # HTTP client, Brief()
  spawn.go         # EnsureLocalServer(), locateServerBinary()
  lockfile.go      # Lockfile struct, Read/Write/IsAlive()
  stop.go          # StopLocalServer()
```

Each file in `client/` is `package client`. Don't create `internal/client/spawn/spawn.go`—that's over-engineering.

---

## Where Domain Logic Lives

### Domain Logic = `internal/`
- **Business rules**: How to spawn a server, when a lockfile is stale, how to detect reuse
- **Data models**: Config structs, Lockfile, BriefResponse
- **Interfaces**: Define what the server needs from the client, what the client needs from config

### Entrypoint = `cmd/`
- **Parse flags**: CLI arguments
- **Load config**: Call into `internal/config`
- **Wire dependencies**: Construct structs and interfaces
- **Call domain logic**: Pass control to `internal/`

```go
// Good: cmd/cli/main.go (entrypoint only)
func main() {
    cfg, err := config.LoadClientConfig()  // Load from internal/
    if err != nil {
        log.Fatal(err)
    }
    
    client := client.NewClient(baseURL)    // Wire from internal/
    resp, err := client.Brief(ctx, req)    // Call domain logic
    if err != nil {
        log.Fatal(err)
    }
}

// Bad: Domain logic in main.go
func main() {
    // ... 100 lines of spawn/detect/lockfile logic here ...
}
```

---

## When to Create a New Struct

### Create a struct when you need to:
1. **Group related data**: Config fields, Client state, Server state
2. **Attach methods**: Server needs `Handler()`, `ListenAndServe()`
3. **Pass as function argument**: Cleaner than 5 separate parameters

```go
// Good: Related data grouped
type Lockfile struct {
    PID       int
    Address   string
    StartedAt time.Time
}

type Client struct {
    baseURL string
    http    *http.Client
}

// Bad: Scattered data
var lockfilePID int
var lockfileAddr string
var lockfileStart time.Time
```

### When NOT to create a struct:
- Single value (use primitive or custom type)
- Temporary helper (use local variables)
- No methods attached (use map or slice if appropriate)

---

## When to Create an Interface

### Create an interface when:
1. **Multiple implementations possible**: Mock for testing, real implementation for production
2. **Consumed by another package**: Decouples consumer from concrete type
3. **Dependency to inject**: Makes dependencies explicit in constructors

```go
// Good: Consumer defines interface it needs
package server

type BriefGenerator interface {
    Generate(ctx context.Context, req *api.BriefRequest) (*api.BriefResponse, error)
}

type Server struct {
    gen BriefGenerator  // Inject as interface
}

// Bad: Exporting concrete type, consumer imports impl package
package api
type BriefGeneratorImpl struct { ... }

package server
s := server.NewServer(&api.BriefGeneratorImpl{})  // Tight coupling
```

### Rule of thumb:
**Define interfaces in the consumer package, implement in any package.** Go satisfies implicitly.

---

## Package Boundaries for morning-brief

| Package | Responsibility | Imports | Exported |
|---------|---|---|---|
| `api` | Wire types (JSON-marshaled) | stdlib only | BriefRequest, BriefResponse |
| `config` | Load/save YAML config, path helpers | stdlib, yaml.v3 | ClientConfig, ServerConfig, paths |
| `client` | HTTP client, spawn/detect, lockfile | stdlib, api, config | Client, EnsureLocalServer, StopLocalServer |
| `server` | HTTP handlers, routing, Server lifecycle | stdlib, api, config | Server, New, Handler |
| `cmd/cli` | Cobra commands, CLI entrypoint | cobra, client, config | main() |
| `cmd/server` | Server entrypoint, signal handling | stdlib, server, config | main() |

**Import rules**:
- `cmd/` imports `internal/` only
- `internal/server` imports `api`, `config`, stdlib (not `client`)
- `internal/client` imports `api`, `config`, stdlib (not `server`)
- `internal/api` imports stdlib only
- `internal/config` imports stdlib, yaml.v3 only

**No circular imports**: If A imports B, B cannot import A. Enforce this.

---

## Growing the Project (Future Phases)

When adding features (news, stocks, LLM), keep this pattern:

```
internal/
  api/              # Add NewsRequest, NewsResponse, StockResponse
  client/           # (no change for local client)
  server/           # Add /news, /stocks handlers
  config/           # Add news.* and stocks.* config fields
  news/             # NEW: fetch & filter news (internal only)
  stocks/           # NEW: fetch stocks & compute moves (internal only)
  briefing/         # NEW: orchestrate news + stocks into briefing
```

Domain logic (`news/`, `stocks/`, `briefing/`) lives in `internal/`. The server wires it:

```go
// cmd/server/main.go
func main() {
    // ... load config, wire server ...
    newsService := news.NewService(config)
    stocksService := stocks.NewService(config)
    briefingService := briefing.NewService(newsService, stocksService)
    
    srv := server.NewServer(briefingService)
    srv.ListenAndServe()
}
```

---

## Anti-Pattern: The `util/` Package

**Don't create `internal/util/` or `internal/helper/`** unless it's truly generic.

```go
// Bad: Vague utility package
internal/util/
  strings.go     // PadString, TrimString, ...
  misc.go        // RandomID, RetryWithBackoff, ...

// Good: Specific, named packages or helper functions in the package that uses them
internal/client/
  backoff.go     # waitForReady uses backoff; keep it here
  
internal/config/
  format.go      # path formatting helpers; keep in config package
```

---

## Summary

**Package = cohesive responsibility.** Don't create a package for reuse until needed by 2+ callers. **Struct = grouped data + methods.** **Interface = explicit dependency.** Define in consumer, implement anywhere.
