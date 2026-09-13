# Architecture & Project Structure

## Directory Layout

```
cmd/
  cli/          # CLI entrypoint (Cobra commands, flag parsing)
  server/       # Server entrypoint (HTTP listener setup)
internal/
  api/          # Wire types: BriefRequest, BriefResponse, HealthResponse
  client/       # HTTP client, spawn/detect/stop lifecycle orchestration
  config/       # Config structs, paths, load/save
  server/       # HTTP handlers, routing, server lifecycle (serving + bounded shutdown)
  state/        # Runtime state file (atomic publication, validation) + lifecycle lock
```

**Rule**: `cmd/` contains only entrypoints and command wiring. All logic lives in `internal/`.

---

## When to Create a New Package

### Create a new `internal/` package when:
1. **Clear single responsibility**: Package does one thing (e.g., `client` = talking to server, `config` = loading config)
2. **Imported by multiple places**: Avoids duplication
3. **Testable in isolation**: Can be tested without bringing in unrelated code

### Keep in existing package when:
- Logic is tightly coupled (e.g., spawn-readiness polling stays in `client/` since only spawn uses it)
- Fewer than 2-3 callers (not worth the package overhead)
- Still under ~600 lines (file is more natural boundary than package)

Note: the runtime state file is a deliberate exception — it lives in its own `state/` package because **both** `internal/client` (verify/remove) and `cmd/server` (publish) need it, and both `client` and `server` are otherwise forbidden from importing each other.

### Example: When to split
```
internal/client/
  client.go        // HTTP client: Brief(), Health(), Shutdown()
  ensure.go        // EnsureLocalServer(), locateServerBinary()
  stop.go          // StopLocalServer()
  process.go       // processAlive() + injectable process ops

internal/state/
  statefile.go     // StateFile struct, Valid(), ReadStateFile/CreateStateFile/RemoveStateFile
  lock.go          // lifecycle lock (flock) acquire/release
```

Each file in `client/` is `package client`. Don't create `internal/client/spawn/spawn.go`—that's over-engineering.

---

## Where Domain Logic Lives

### Domain Logic = `internal/`
- **Business rules**: How to spawn a server, when a runtime state file is stale, how to detect reuse
- **Data models**: Config structs, StateFile, BriefResponse
- **Interfaces**: consumer-defined, added only when a genuine second implementation or a test double needs one (see "When to Create an Interface" below)

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
    
    client, err := client.NewClient(baseURL, nil) // Wire from internal/
    if err != nil {
        log.Fatal(err)
    }
    resp, err := client.Brief(ctx, &api.BriefRequest{}) // Call domain logic
    if err != nil {
        log.Fatal(err)
    }
}

// Bad: Domain logic in main.go
func main() {
    // ... 100 lines of spawn/detect/state-file logic here ...
}
```

---

## When to Create a New Struct

### Create a struct when you need to:
1. **Group related data**: Config fields, Client state, Server state
2. **Attach methods**: Server needs `Handler()`, `Serve(ctx, ln)`
3. **Pass as function argument**: Cleaner than 5 separate parameters

```go
// Good: Related data grouped
type StateFile struct {
    PID       int
    Address   string
    StartedAt time.Time
    Token     string
}

type Client struct {
    baseURL string
    http    *http.Client
}

// Bad: Scattered data
var statePID int
var stateAddr string
var stateStart time.Time
```

### When NOT to create a struct:
- Single value (use primitive or custom type)
- Temporary helper (use local variables)
- No methods attached (use map or slice if appropriate)

---

## When to Create an Interface

**Rule**: Don't design with interfaces upfront. Discover them as needed. Go's proverb: "Accept interfaces, return structs" — start with concrete types; add an interface only when **two conditions** align:

1. **A genuine need appears**: Either a second real implementation exists, or a test needs to substitute a fake
2. **Benefit outweighs ceremony**: Coupling becomes a problem if left unfixed

**For Phase 1**: No interfaces needed yet for `Client`, `Server`, or `Config` — each has exactly one concrete implementation and one caller. Examples:

```go
// Good: Concrete struct, no interface wrapper (Phase 1)
type Server struct {
    config config.ServerConfig
}

// Also good: test doubles *discovered* when testing needs one
// (in _test.go, not main code). HTTP behavior is tested against
// httptest.Server against the real *http.Client — no mock needed.
type mockBriefingGenerator struct {
    generateFn func(ctx context.Context, req *api.BriefRequest) (*api.BriefResponse, error)
}

// Future (Phase 2+): Internal/llm will have multiple providers → interface is justified
type Provider interface {
    Generate(ctx context.Context, req *api.BriefRequest) (*api.BriefResponse, error)
}
// (Claude, OpenAI, Ollama implementations — the spec already names them)
```

### Rule of thumb:
**Define interfaces in the consumer package when one is genuinely needed; implement in any package.** Go satisfies implicitly. But wait to define the interface until you have proof (multiple implementations or a testing need).

---

## Package Boundaries for morning-brief

| Package | Responsibility | Imports | Exported |
|---------|---|---|---|
| `api` | Wire types (JSON-marshaled) + protocol constants | stdlib only | BriefRequest, BriefResponse, HealthResponse, ErrorResponse, ProtocolVersion |
| `config` | Load client YAML + narrow transactional updates; load server YAML; path helpers | stdlib, yaml.v3, x/sys (config lock) | ClientConfig, ServerConfig, ServerProfile, SetServerConnection(To), ClearServerURL(To), LoadClientConfig(From), LoadServerConfig(From), paths |
| `state` | Runtime state file (atomic publication, validation) + lifecycle lock + LifecyclePaths | stdlib, x/sys (flock) — no other internal packages | StateFile, CreateStateFile/ReadStateFile/RemoveStateFile, LifecyclePaths, lifecycle lock |
| `client` | HTTP client, spawn/detect/stop lifecycle orchestration | stdlib, api, config, state | Client, EnsureLocalServer, StopLocalServer, EnsureOpts, EnsureResult, sentinels |
| `server` | HTTP handlers, routing, Serve + bounded shutdown | stdlib, api, config | Server, New, Handler, Serve |
| `cmd/cli` | Cobra commands, CLI entrypoint | cobra, api, client, config | main() |
| `cmd/server` | Server entrypoint, signal handling, state publication wiring | stdlib, server, config, state | main() |

**Import rules**:
- `cmd/` imports `internal/` only
- `internal/server` imports `api`, `config`, stdlib (not `client`, not `state`)
- `internal/client` imports `api`, `config`, `state`, stdlib (not `server`)
- `internal/state` imports stdlib + x/sys only (no other internal packages)
- `internal/api` imports stdlib only
- `internal/config` imports stdlib, yaml.v3, x/sys only

**No circular imports**: If A imports B, B cannot import A. Enforce this.

---

## Growing the Project (Future Phases)

When adding features (news, stocks, LLM), keep this pattern:

```
internal/
  api/              # Add NewsRequest, NewsResponse, StockResponse wire types
  client/           # (no change for local client)
  server/           # Add /news, /stocks, /news/summary handlers
  state/            # (no change)
  config/           # Add news.* and stocks.* config fields
  news/             # NEW: fetch & filter news (internal only)
  stocks/           # NEW: fetch stocks & compute moves (internal only)
  briefing/         # NEW: orchestrate news + stocks into briefing
  llm/              # NEW: provider abstraction + tool interface/registry (per ../docs/PRODUCT.md)
  render/           # NEW: CLI-side terminal UI (Bubble Tea) and markdown page writer
```

**Dependency direction (normative — see ../docs/PRODUCT.md, Architecture)**: `internal/llm` defines the tool interface/registry and never imports `news`/`stocks`; `news`/`stocks` import `llm`; `briefing` composes fetchers as tools. No import cycles.

Domain logic (`news/`, `stocks/`, `briefing/`) lives in `internal/`. The server wires it:

```go
// cmd/server/main.go (illustrative; exact constructor signature evolves per phase)
func main() {
    // ... load config, wire server, start listener, signal context, publish state ...
    newsService := news.NewService(config)
    stocksService := stocks.NewService(config)
    briefingService := briefing.NewService(newsService, stocksService)
    // briefingService also registers news/stocks fetchers as LLM tools

    // Domain services are injected into the server: internal/server owns the
    // consumer-side interfaces (e.g. BriefingGenerator) and never imports
    // news/stocks/briefing directly. Phase 1's server.New(cfg, token, log)
    // takes no services — /brief is a stub.
    srv := server.New(config, token, logger, briefingService)
    srv.Serve(ctx, ln)
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
  backoff.go     // spawn readiness polling uses backoff; keep it here
  
internal/config/
  format.go      // path formatting helpers; keep in config package
```

---

## Summary

**Package = cohesive responsibility.** Don't create a package for reuse until needed by 2+ callers. **Struct = grouped data + methods.** **Interface = explicit dependency.** Define in consumer, implement anywhere.
