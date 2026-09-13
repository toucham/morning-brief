# Morning Brief — System Implementation Guide (Phases 1–7)

This document captures the implementation mechanics for `morning-brief` across every roadmap phase, written to let a future session or agent resume implementation without re-deriving decisions. **Product behavior is defined in [PRODUCT.md](PRODUCT.md)** — this file covers implementation mechanics only, and must never contradict the product spec. When they disagree, the product spec wins for behavior and this file wins for mechanics.

## Overview

Phase 1 goal: scaffold two Go binaries (CLI client and server) in one module, with a stub HTTP endpoint, a **safe** standalone auto-spawn/detect lifecycle (lifecycle lock, token-verified identity, authenticated shutdown), `--server`/`--standalone` flag persistence to config, and `morning server stop`. No LLM, news, stocks, or TUI work in Phase 1 — just enough to prove the architecture end-to-end.

Beyond the Phase 1 scaffold (everything in this file up to the CLI Command Logic section), this guide documents the mechanics for each later phase so the decisions are not re-derived later:

| Phase | Roadmap slice | Section in this file |
|---|---|---|
| 1 | Client/server split (scaffold) | [Directory Layout](#directory-layout) through [CLI Command Logic](#cli-command-logic-in-cmdcli) (below) |
| 2 | News list | [News Digest Engine](#news-digest-engine-phase-2-internalnews) |
| 3 | Lazy summaries | [LLM Engine & Agentic Tool Use](#llm-engine--agentic-tool-use-phase-3-internalllm) |
| 4 | Stocks | [Stock Tracking](#stock-tracking-phase-4-internalstocks) |
| 5 | Unified output | [Unified Briefing & TUI Rendering](#unified-briefing--tui-rendering-phase-5-internalbriefing-internalrender) |
| 6 | Settings | [Settings Overlay & Live Sync](#settings-overlay--live-sync-phase-6-internalrendersettings) |
| 7 | Polish | [Production Polish & Operational Runbooks](#production-polish--operational-runbooks-phase-7) |

Sections are ordered by phase; a later phase's section may cross-reference an earlier one.

**Module**: `github.com/toucham/morning-brief` (go 1.26.2)

**Platforms**: **macOS and Linux only.** The lifecycle uses Unix primitives: `flock(2)` (lifecycle + config locking), `Setsid` (detached child), `Signal(0)` (liveness probe), and `SIGTERM`/`SIGKILL` — the latter **only** against the direct child that this CLI invocation itself spawned, during failed-spawn cleanup, and only while its `Wait` is still pending (see Lifecycle, safety property 3). Stopping a persistent server is an authenticated HTTP request to the verified identity, never a signal to a PID read from a state file. Windows is not supported in this project and must not be implied by documentation or CI.

**Binaries**:
- `cmd/cli/main.go` → `morning` (or `bin/morning`) — CLI entrypoint
- `cmd/server/main.go` → `morning-server` (or `bin/morning-server`) — server entrypoint

**User-facing commands**:
- `morning brief` — fetch a stub briefing from the server, print it as one line of JSON to stdout. Flags: `--server <url>` (use and persist remote), `--standalone` (use local), neither (use saved config, or local if nothing saved).
- `morning server stop` — stop the managed local server (authenticated `POST /shutdown` to the verified instance) and clean up its state file.

---

## Directory Layout

Phase 1 ships only the packages marked **(P1)**; later phases add the rest. The final target layout is:

```
cmd/
  cli/
    main.go        # CLI entrypoint: thin main() + newRootCmd() Cobra tree (brief, server stop)
  server/
    main.go        # Server entrypoint: flags, listener, signal context, state publication, shutdown

internal/
  client/          # (P1) HTTP client + local server spawn/detect/stop lifecycle orchestration (standalone mode)
  server/          # (P1) HTTP server: Server struct, Handler(), POST /brief + GET /healthz + POST /shutdown
  config/          # (P1) Config structs, path helpers, load/save (atomic, unknown-field-preserving)
  state/           # (P1) Runtime state file + lifecycle lock (atomic publication, validation, flock)
  api/             # (P1) Shared wire types: BriefRequest, BriefResponse, HealthResponse
                   #   (P2+) NewsRequest/NewsItem/NewsResponse, NewsSummaryRequest/Response
                   #   (P4)  StockRequest/Quote/StocksResponse
  news/            # (P2) News digest engine: source fetchers (RSS, YouTube, scrape fallback), item identity
  llm/             # (P3) LLM provider abstraction (LangChainGo), Tool interface + registry, lazy summarizer
  stocks/          # (P4) Stock tracker: multi-provider fetch (Yahoo primary, Stooq fallback), TTL cache, explainer
  briefing/        # (P5) Server-side orchestrator: concurrent news+stocks fan-out, BriefResponse assembly, tool wiring
  render/          # (P5) Charm stack TUI (Bubble Tea loop, Lip Gloss 66/33 layout, markdown save)
    settings/      # (P6) Full-screen tabbed settings overlay model + live config dispatch
```

`cmd/` contains only entrypoints and command wiring (Cobra definitions live in `cmd/cli` as `package main` — the sanctioned exception to "no logic in cmd", per `../.agents/ARCHITECTURE.md`). All other logic lives in `internal/`.

**Ownership invariants** (normative):
- `internal/state` owns the state file's format, atomic publication, validation, and the lifecycle lock. It is the only package that *implements* state-file creation/removal; its callers are `internal/client` (removal) and `cmd/server` (publication).
- `internal/client` orchestrates the lifecycle (lock → verify → spawn/reuse/stop → cleanup) and owns the HTTP client. It never writes the state file itself.
- `internal/server` owns HTTP serving and the bounded-shutdown state machine. It **never** handles OS signals, touches state files, or manages processes.
- `cmd/server` wires flags/config/listener and, in managed mode, calls `internal/state` to publish state after a successful bind. It performs no state recovery and no state self-cleanup.
- **Dependency acyclicity (P2+, normative):** `briefing → {news, stocks, llm}`, `news → llm`, `stocks → llm`, `llm → (nothing internal)`, `render → api + client only` (the TUI talks to the server exclusively over HTTP via `internal/client`, never directly to `news`/`stocks`/`llm`). `internal/llm` must never import `news` or `stocks` — it exposes a `Tool` interface + registry, and concrete domain tools are constructed in `internal/briefing` (server-side) by wrapping the fetchers. `news` and `stocks` never import each other.

---

## Dependencies

Pinned at implementation time (recorded in `go.mod`/`go.sum`). Do **not** use bare `@latest` — bump versions deliberately.

| Dependency | Purpose | Version |
|---|---|---|
| `github.com/spf13/cobra` | CLI framework (subcommand tree, mutually exclusive flags) | `v1.10.2` |
| `gopkg.in/yaml.v3` | YAML config (including `yaml.Node` round-trips for unknown-field preservation) | `v3.0.1` |
| `golang.org/x/sys` | `unix.Flock` (`flock(2)`) for the lifecycle lock and config lock | `v0.47.0` |

**Toolchain note**: built and verified with Go `1.27.0` (`go.mod` declares `go 1.26.2`). On Go 1.27+ the standalone `http.Client.Proxy` field is **removed** — disable the environment proxy via `Transport: &http.Transport{Proxy: nil}`. Also, `http.ErrBodyTooLarge` no longer exists; `http.MaxBytesReader` over-cap reads surface as a typed `*http.MaxBytesError`, detected with `errors.As`.

**Rationale for golang.org/x/sys**: `flock(2)` gives an advisory lock the kernel releases automatically when the holder dies (crash-safe, no stale-lock files to recover), and `unix.Flock` is the portable API across both supported platforms (macOS + Linux). Stdlib has no flock wrapper.

**Rationale for Cobra**: Phase 1 needs a two-level subcommand tree (`morning brief`, `morning server stop`) plus mutually exclusive flags (`--server` vs `--standalone`) with good `--help`. Stdlib `flag` doesn't handle this cleanly. Cobra also pairs naturally with Charm's Bubble Tea (the TUI framework for later phases).

**Rationale for yaml.v3**: Direct struct marshal/unmarshal plus `yaml.Node` document round-trips. Avoid `viper` (env/flag-merging magic, remote config) — it would obscure the explicit `--server` → `client.yaml` persistence behavior the spec cares about.

**Stdlib coverage**: `net/http` (Go 1.22+ `ServeMux` with method+pattern routes, `MaxBytesReader`), `os/exec` + `syscall.SysProcAttr` (process spawning; `Wait` for reaping), `os.FindProcess`/`Signal(0)` (liveness probe), `crypto/rand` (instance tokens), `crypto/subtle` (constant-time token comparison).

### Future-phase dependencies (P2–P7)

Pinned at each phase's implementation time; record the chosen versions here and in `go.mod`/`go.sum`. Same no-bare-`@latest` rule applies.

| Dependency | Purpose | First used |
|---|---|---|
| `github.com/tmc/langchaingo` | LLM provider abstraction (Claude, OpenAI, Ollama) + chat tool-calling API | Phase 3 (`internal/llm`) |
| `github.com/mmcdole/gofeed` | RSS 2.0 / Atom feed parsing | Phase 2 (`internal/news`) |
| `github.com/PuerkitoBio/goquery` | CSS-style HTML queries for the scrape fallback | Phase 2 (`internal/news`) |
| `github.com/charmbracelet/bubbletea` | TUI event loop | Phase 5 (`internal/render`) |
| `github.com/charmbracelet/bubbles` | Composable TUI widgets (viewport, spinner, list) | Phase 5 (`internal/render`) |
| `github.com/charmbracelet/lipgloss` | Layout + styling (66/33 split, borders, themes) | Phase 5 (`internal/render`) |
| `github.com/charmbracelet/glamour` | Terminal Markdown rendering (summary panel, saved-preview) | Phase 5 (`internal/render`) |
| `github.com/dop251/goja` *(candidate)* | JS evaluation if a YouTube transcript fallback route requires it — decide at Phase 2 implementation time; do not add speculatively | Phase 2 (only if needed) |
| `golang.org/x/sync` | `errgroup` for bounded concurrent fetch fan-out | Phase 2/5 |

Charm stack note: all four Charm packages move version together and are API-compatible only as a set — bump them in lockstep.

---

## Configuration

### Paths

Path helpers live in `internal/config`. Config and runtime state are separated: config survives reinstallation, **state is durable** (never in a cache directory, per product decision 7) — it is the only management record for the persistent local server.

```go
func ClientConfigPath() (string, error)     // {os.UserConfigDir()}/morning-cli/client.yaml
func ServerConfigPath() (string, error)     // {os.UserConfigDir()}/morning-server/server.yaml
func ClientConfigLockPath() (string, error) // {config dir}/client.yaml.lock — sidecar lock, never deleted
func ClientStateDir() (string, error)       // see table below
func ClientStateFilePath() (string, error)  // {ClientStateDir()}/server.lock
func ClientLogPath() (string, error)        // {ClientStateDir()}/server.log
func ClientLifecycleLockPath() (string, error) // {ClientStateDir()}/server.lifecycle.lock — persistent, never deleted
```

| Platform | Config | State dir (state file + log + lifecycle lock) |
|---|---|---|
| Linux | `~/.config/morning-cli/client.yaml` | `$XDG_STATE_HOME/morning-cli` (default `~/.local/state/morning-cli`) |
| macOS | `~/Library/Application Support/morning-cli/client.yaml` | `~/Library/Application Support/morning-cli` |

Implementation notes:
- `ClientStateDir` on Linux honors `$XDG_STATE_HOME` when set (non-empty); otherwise `$HOME/.local/state`. Both `os.UserConfigDir()` and the Linux state dir can fail (no `$HOME`); **propagate the error** — no silent fallbacks.
- On macOS the state dir coincides with the client config dir; that is intentional and matches the product spec.
- All path helpers must be overridable in tests (e.g. via `t.Setenv` for `HOME`/`XDG_STATE_HOME`, or an unexported resolver function that tests can swap). Environment-dependent path tests must not run in parallel.
- **Directory mode `0700`** for both config and state dirs (create with `MkdirAll`; do not chmod pre-existing dirs). **File mode `0600`** for config, state, log, and lock files.
- **No symlinked state**: before use, `Lstat` the state dir; if it is a symlink, return a wrapped error (a symlink swap could redirect state writes to an attacker-chosen location).
- **Lock file semantics**: both lock files are created on first use and **never deleted** — deleting a flock file races with a concurrent acquirer. The kernel releases the lock when the holder's fd closes (normal exit or crash), so no stale-lock recovery protocol is needed.

### Client Config

YAML file. Phase 1 shape:

```go
type ClientConfig struct {
    Server ServerConn `yaml:"server,omitempty"`
}

type ServerConn struct {
    URL      string          `yaml:"url,omitempty"`      // "" => standalone mode
    Profiles []ServerProfile `yaml:"profiles,omitempty"` // named entries for Settings → Server profile
}

type ServerProfile struct {
    Name string `yaml:"name"`
    URL  string `yaml:"url"`
}
```

**Narrow transactional operations — path-accepting helpers are primary; no-arg wrappers use the standard paths**. There is deliberately **no general "save whole config" API in Phase 1**: a writer that replaces the document would be a footgun for fields added by later phases. Each operation is a transaction: lock → read the latest document → mutate only its own fields → atomic write.

```go
func LoadClientConfig() (ClientConfig, error)            // wrapper over LoadClientConfigFrom(ClientConfigPath())
func LoadClientConfigFrom(path string) (ClientConfig, error)

// SetServerConnection persists server.url = url AND upserts the profile named
// "default" with that URL (product spec, Settings → Server profiles).
func SetServerConnection(url string) error               // wrapper over SetServerConnectionTo(url, ClientConfigPath())
func SetServerConnectionTo(url string, path string) error

// ClearServerURL sets server.url to "" (standalone mode). Profiles are kept.
func ClearServerURL() error                              // wrapper over ClearServerURLTo(ClientConfigPath())
func ClearServerURLTo(path string) error
```

Semantics (normative):
- **Missing file is not an error**: load returns the zero value. (This supersedes the older `os.ErrNotExist` expectation in `../.agents/TESTING.md`, which is updated to match.)
- **Malformed YAML is an error** (wrapped with the path).
- **A non-empty document whose root is not a mapping is an error**: a top-level scalar or sequence cannot be merged into, and synthesizing an empty mapping would silently replace the file's content on the next write. The file is left untouched.
- **Unknown fields are semantically preserved**: each transaction round-trips the existing document through `yaml.Node`, mutates **only `server.url` and `server.profiles`**, and re-marshals the whole document. All other `server.*` mappings and every sibling key survive with their values and key order, and comments are preserved where `yaml.v3` retains them. Formatting (quoting, indentation) is re-encoded — **byte-for-byte identity is NOT promised** and must not be asserted in tests; what is promised is that no field added by a later phase is discarded.
- **Atomic writes**: write to a temp file in the same directory, `fsync`, close, rename over the target, then `fsync` the containing directory (the directory entry is what durability is about). `mkdir -p` the parent with mode `0700`. File mode `0600`.
- **Serialized read-modify-write, lock derived from the target path**: the transaction re-reads the existing document under an exclusive `flock` on the lock sidecar `path + ".lock"` and applies its mutation to the *latest* document, so concurrent `morning brief --server` invocations cannot truncate or clobber each other. `ClientConfigLockPath()` is just `ClientConfigPath() + ".lock"`; a custom path gets its own sidecar, so tests never touch the real config. The sidecar's parent directory is created (`0700`) before locking, so a clean install with no config directory yet does not fail after retrying until timeout. Lock files are persistent and never deleted.
- **Validate before persist**: `SetServerConnection`'s `url` must be strict — `http://` or `https://` with a non-empty host and **no** path (other than `/`), query, fragment, or userinfo (Phase 1 remote endpoints have no path prefix). Invalid URLs are rejected and nothing is written.
- **`ClearServerURLTo` is a no-op** when there is no `server` key, or no `url` key under it, or the url is already empty (the file is not rewritten); a non-mapping `server` key is an error.

**Design for future growth**: later phases add sibling keys to `ClientConfig` (`news:`, `stocks:`, `output:`) and further nested `server.*` fields — the Node round-trip keeps older binaries safe.

### Server Config

YAML file. Phase 1 shape:

```go
type ServerConfig struct {
    Address string `yaml:"address,omitempty"` // bind address; used when the server is run manually (see precedence below)
}
```

Load-only in Phase 1 — **no save API exists until a writer does** (editing `server.yaml` is a manual, user-driven act per the product spec; adding a save API now would be speculative surface area):

```go
func LoadServerConfig() (ServerConfig, error)
func LoadServerConfigFrom(path string) (ServerConfig, error)
```

Load semantics are the same as the client config (missing file → zero value; malformed YAML → wrapped error).

**Address precedence** (normative, resolves the earlier "spawn overrides config" contradiction):
1. `--address` flag — used **as provided when the flag was explicitly set** (detected via `flag.Visit`/Cobra `Changed`; a set-but-empty value is a user error and exits with a clear message)
2. Spawn override — auto-spawned standalone servers always get `--address 127.0.0.1:0` (loopback, dynamic port; product decisions 2 and 4)
3. `server.yaml` `address` (manual local runs; default `127.0.0.1:8787` if empty)

`server.yaml`'s `address` therefore applies to **manually started** servers; the auto-spawn path is always loopback + dynamic port regardless of config.

**Note**: No API keys in config — they live in env vars, added in a later phase when LLM provider integration happens.

### Runtime State File (in `internal/state`)

Stored at `ClientStateFilePath()`. JSON file (machine-only, never hand-edited):

```go
type StateFile struct {
    PID       int       `json:"pid"`
    Address   string    `json:"address"` // bound loopback address, e.g. "127.0.0.1:54321"
    StartedAt time.Time `json:"started_at"`
    Token     string    `json:"token"`   // random 64-hex instance token (32 bytes from crypto/rand)
}
```

Functions:

```go
func ReadStateFile(path string) (*StateFile, error) // (nil, nil) when missing; wrapped errors when malformed
func CreateStateFile(path string, sf *StateFile) error // atomic no-replace publication (below); EEXIST => error
func RemoveStateFile(path string) error               // ignores os.ErrNotExist
func (sf *StateFile) Valid() error                    // semantic validation (below)
```

**Atomic no-replace publication** (normative). `CreateStateFile` publishes so readers can never observe partial JSON and so an existing state file is never overwritten:
1. Create a uniquely named temp file in the same directory (`O_CREATE|O_EXCL`, mode `0600`).
2. Write the complete JSON, `Sync`, close.
3. `os.Link(temp, path)` — atomic; fails with `EEXIST` if the state file already exists (that is a conflict, not a replace).
4. Unlink the temp name; `Sync` the containing directory.

Plain `rename` is deliberately **not** used (it silently overwrites), and Linux `RENAME_NOREPLACE` is not the baseline (not portable to macOS).

**Semantic validation** (normative — `Valid()` runs before any use of a state file):
- `PID > 0`
- `StartedAt` is non-zero
- `Token` is exactly 64 lowercase hex characters
- `Address` is `127.0.0.1:<port>` with a valid non-zero port (numeric loopback only — product decision 2)

A state file that fails validation is **never trusted**: it may be removed only when the recorded process can be proven dead (see Ensure/Stop flows); otherwise it is reported as a conflict.

**The token is the ownership key — verified server-side.** The CLI presents the token as a Bearer credential; the server returns `200` from `GET /healthz` only when its own token matches (constant-time compare). A bare "PID is alive" is **never** sufficient to reuse a process, and **no process discovered from a state file is ever signaled** — stopping goes through `POST /shutdown` to the verified identity (see Lifecycle).

### Server Log

Stored at `ClientLogPath()`. Plain text, **append-mode, mode `0600`** (not 0644 — it will eventually contain request metadata and LLM errors). The spawned server's `Stdout`/`Stderr` are redirected here. Phase 1: no rotation; later phases add size limits and redaction (product spec, decision 7 context). Never log API keys.

---

## Stub API (in `internal/api` and `internal/server`)

### Wire Types

```go
// ProtocolVersion is the shared protocol revision — the single source of truth
// for both the server (HealthResponse.ProtocolVersion) and the client (Health
// comparison). Never hardcode 1 elsewhere.
const ProtocolVersion = 1

type BriefRequest struct{} // empty for Phase 1; final shape per PRODUCT.md "Client/Server API" (nested news/stocks overrides + top-level model) — the Phase 1 server ignores all fields

type BriefResponse struct {
    GeneratedAt time.Time `json:"generated_at"`
    Message     string    `json:"message"` // canned stub text
}

type HealthResponse struct {
    Token           string `json:"token"`
    ProtocolVersion int    `json:"protocol_version"` // api.ProtocolVersion (1 in Phase 1)
}

// ErrorResponse is the shared error shape {"error": "<message>"} used by every
// non-2xx response (400/401/403/404/405/413).
type ErrorResponse struct {
    Error string `json:"error"`
}
```

### Endpoints

Registered in `internal/server` using Go 1.22+ `ServeMux` pattern syntax. The API table in the product spec lists *feature* endpoints; `GET /healthz` and `POST /shutdown` are operational endpoints required by the lifecycle and are not briefing endpoints:

| Endpoint | Request | Response | Errors |
|---|---|---|---|
| `POST /brief` | optional JSON body (ignored in Phase 1) | `200` `BriefResponse` JSON | `400` malformed JSON; `405` wrong method |
| `GET /healthz` | `Authorization: Bearer <token>` | `200` `HealthResponse` JSON — the server's instance token + `protocol_version: 1` | `401` missing/mismatched token; `405` wrong method |
| `POST /shutdown` | `Authorization: Bearer <token>` | `200` `{"message":"shutting down"}` — the server initiates a bounded graceful shutdown (see below) | `401` missing/mismatched token; `405` wrong method |

- All responses are `Content-Type: application/json` — **including `404` and `405`**. Standard `ServeMux` defaults emit plain text, so the handler must not rely on them: each endpoint is registered for all methods and returns a JSON `405` for the wrong method; a root fallback (`mux.HandleFunc("/", ...)`) returns a JSON `404`.
- Error responses use the shared shape `{"error": "<message>"}`.
- Request bodies are capped at **1 MiB** (`http.MaxBytesReader`); oversized → `413` in the same error shape.
- The server sets `ReadHeaderTimeout: 5s`, `ReadTimeout: 10s`, `WriteTimeout: 30s`, `IdleTimeout: 60s`.
- `POST /brief` returns `BriefResponse{GeneratedAt: time.Now().UTC(), Message: "stub brief: no real content yet"}`.

**Authentication** (normative):
- `GET /healthz` and `POST /shutdown` require `Authorization: Bearer <token>` and compare it with `crypto/subtle.ConstantTimeCompare`.
- **A `200` alone is not identity verification**: the CLI additionally requires the returned `HealthResponse.Token` to equal the state file's token and `ProtocolVersion` to equal `api.ProtocolVersion`. Both checks happen client-side on the parsed body.
- A server started **without** `--state-file` (manual mode): `GET /healthz` is unauthenticated and returns `token: ""`; `POST /shutdown` returns `403` — a manual server is stopped by killing its process, never via the API.
- The token is never logged and never echoed in error messages.
- The CLI's lifecycle requests use an `http.Client` with the environment proxy disabled (`Transport.Proxy` is `nil` — the standalone `http.Client.Proxy` field was removed in Go 1.27) and a `CheckRedirect` that refuses to follow any redirect (`http.ErrUseLastResponse`) — a redirected `200` must not be accepted as health, and redirecting a `POST /shutdown` is undefined behavior.
- `POST /shutdown` accepts "shutdown initiated", not "process exited": the handler writes the `200` response, then triggers the internal shutdown. Callers must confirm the recorded **process** is dead before concluding the server is gone (see Stop flow) — connection refusal alone is not sufficient, because `http.Server.Shutdown` closes the listener before in-flight handlers finish.

**Threat model** (normative, scopes the token's guarantees): the instance token defends against **accidental conflicts** — stale state files, recycled PIDs, and an unrelated benign service coincidentally serving the recorded loopback port. It is **not** a security boundary against a malicious process running as the same OS user (such a process could read the `0600` state file and impersonate the token); that threat class is explicitly out of scope for Phase 1.

### Server Struct and Lifecycle

Defined in `internal/server`. The server package **never** handles OS signals or state files — those belong to `cmd/server` (see `../.agents/DESIGN_PATTERNS.md` "CLI Entrypoints").

```go
type Server struct {
    cfg          config.ServerConfig
    token        string         // "" when the server is not a managed standalone instance
    log          *slog.Logger
    shutdown     chan struct{}  // closed (once) by POST /shutdown; created in New
    shutdownOnce sync.Once
}

func New(cfg config.ServerConfig, token string, log *slog.Logger) *Server
// New always creates the shutdown channel, so Handler() is safe to use
// independently of Serve (e.g. httptest around s.Handler()) — a /shutdown
// request before Serve simply closes a channel nobody has selected on yet.

func (s *Server) Handler() http.Handler {
    // JSON error wrapper (400/401/404/405/413 in the shared {"error": ...} shape),
    // 1 MiB body cap, Bearer auth on /healthz and /shutdown.
    // Each endpoint is registered for all methods; the wrong method => JSON 405.
    mux := http.NewServeMux()
    mux.HandleFunc("/brief", s.routeBrief)      // POST only, else JSON 405
    mux.HandleFunc("/healthz", s.routeHealthz)  // GET only, else JSON 405
    mux.HandleFunc("/shutdown", s.routeShutdown) // POST only, else JSON 405
    mux.HandleFunc("/", s.notFound)             // JSON 404 fallback
    return mux
}

// Serve runs the HTTP server on ln until ctx is cancelled (the OS-signal
// context in cmd/server) or POST /shutdown closes s.shutdown — whichever
// first — then performs a graceful shutdown bounded by 5s, force-closing
// whatever remains. It returns nil on a clean shutdown.
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
    httpSrv := &http.Server{
        Handler:           s.Handler(),
        ReadHeaderTimeout: 5 * time.Second,
        ReadTimeout:       10 * time.Second,
        WriteTimeout:      30 * time.Second,
        IdleTimeout:       60 * time.Second,
    }
    errCh := make(chan error, 1)
    go func() { errCh <- httpSrv.Serve(ln) }()
    select {
    case err := <-errCh:
        if errors.Is(err, http.ErrServerClosed) { return nil }
        return err // listener failure is fatal, not a log-and-wait
    case <-ctx.Done():
    case <-s.shutdown:
    }
    dctx, dcancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer dcancel()
    if err := httpSrv.Shutdown(dctx); err != nil {
        httpSrv.Close() // force
        return err
    }
    return nil
}
```

`routeShutdown` (after Bearer auth): write the `200` response **first**, then `s.shutdownOnce.Do(func() { close(s.shutdown) })` — the response is guaranteed to be delivered before the server stops accepting, and concurrent shutdown requests cannot double-close the channel.

### Server Entrypoint (`cmd/server`)

Thin `main()` → `run(ctx) int` (per `../.agents/DESIGN_PATTERNS.md`):

```
1. Parse flags: --address (string), --state-file (string)
   - Managed mode is selected by --state-file being set (a non-empty path).
   - There is no --token flag: the managed server generates its own 64-hex token
     (crypto/rand) at startup. The token never appears in argv, so it is not
     visible via ps/process inspection (product threat model: accidental conflicts).
2. Load ServerConfig (missing file => defaults)
3. Resolve address per the precedence above (an explicitly set-but-empty --address is a user error).
   - Managed mode enforces the loopback invariant: the resolved address must be
     127.0.0.1:<port> (numeric loopback). Any other host is a usage error (exit 2) —
     a managed server must never expose the authenticated control plane off-loopback,
     and its state would fail StateFile.Valid() anyway.
4. ln, err := net.Listen("tcp", addr)   // a bind failure is a hard error (non-zero exit)
5. If managed mode (--state-file set):
   - MkdirAll(filepath.Dir(--state-file), 0700)
   - token := hex(crypto/rand 32 bytes)
   - state.CreateStateFile(--state-file, {PID: os.Getpid(), Address: ln.Addr().String(), StartedAt: now, Token: token})
     - EEXIST (an existing state file) => exit 1 with a clear conflict error. The server does
       NOT self-recover: the lifecycle lock guarantees the CLI removed any stale state before
       spawning, so EEXIST indicates a genuine conflict, not a leftover.
6. srv := server.New(cfg, token /* "" in manual mode */, logger)
7. ctx, stopSignals := signal.NotifyContext(runCtx, SIGINT, SIGTERM); defer stopSignals()
   // runCtx is the context passed into run() — tests can cancel it; signal handling lives here.
8. err := srv.Serve(ctx, ln)   // blocks until signal or POST /shutdown (graceful) or listener failure
   - nil => exit 0; listener failure => exit 1
   - NO state self-cleanup: a stopped or crashed server leaves its state file in place. The next
     CLI lifecycle operation (Ensure or Stop, under the lifecycle lock) removes it after proving
     the recorded PID is dead. This keeps state mutation single-owner (internal/state via CLI)
     and removes the stop/start race.
```

### HTTP Client (in `internal/client`)

```go
type Client struct {
    baseURL string
    http    *http.Client
}

// NewClient creates a Client for a full base URL (e.g. "http://127.0.0.1:54321").
// The URL must be http(s) with a non-empty host and NO userinfo, path (other than "/"),
// query, or fragment — Phase 1 remote endpoints have no path prefix.
// A nil httpClient selects the default: 10s overall timeout, environment proxy
// disabled (Transport.Proxy is nil — the standalone http.Client.Proxy field was
// removed in Go 1.27), and a CheckRedirect that returns http.ErrUseLastResponse
// (no redirects are ever followed — lifecycle requests must be answered directly
// by the recorded address). Response bodies are capped at 8 MiB; a larger
// response is an error (detected via *http.MaxBytesError).
func NewClient(baseURL string, httpClient *http.Client) (*Client, error)

func (c *Client) Brief(ctx context.Context, req *api.BriefRequest) (*api.BriefResponse, error)
// POST {base}/brief; nil req marshals as {}; non-2xx => wrapped error carrying status code
// and, when parseable, the server's {"error": "..."} message.

func (c *Client) Health(ctx context.Context, token string) (*api.HealthResponse, error)
// GET {base}/healthz with Authorization: Bearer <token>. Success requires ALL of:
//   - HTTP 200 (the server compared the presented token, constant-time),
//   - HealthResponse.Token == token (the server echoed back the exact token),
//   - HealthResponse.ProtocolVersion == api.ProtocolVersion.
// Any deviation is an identity failure: 401 or token mismatch => ErrForeignServer
// (wrapped with the address); protocol mismatch => ErrProtocolMismatch; any other
// status or body-shape problem => wrapped error.

func (c *Client) Shutdown(ctx context.Context, token string) error
// POST {base}/shutdown with Authorization: Bearer <token>. 2xx means shutdown
// was initiated (the process may still be draining connections); 401 =>
// ErrForeignServer; other status => wrapped error.
```

All network calls honor `ctx` for cancellation and deadlines.

---

## Spawn / Detect / Stop (in `internal/client`)

### Why

Per the product spec, a standalone local server is **not** auto-killed when the CLI exits — it stays running across invocations. The state file tracks the instance so the CLI can reuse it, and `morning server stop` shuts it down. Three safety properties shape the design:

1. **Ownership is proven by token, never by PID.** The CLI can race with itself (two terminals) and PIDs get recycled. A process is reusable only when the *live server's* authenticated `GET /healthz` accepts the state file's token **and echoes it back** — the server does the comparison, so a recycled PID serving a different (or no) token can never pass.
2. **No process discovered from a state file is ever signaled.** Stopping the managed server is an authenticated `POST /shutdown` to the verified identity; there is no check-then-signal window at all.
3. **The only signals ever sent target the direct child this CLI invocation spawned**, during failed-spawn cleanup — and every signal is *coordinated with the single `Wait`*: the reaper delivers the exit result on `waitCh` and then closes a broadcast `exitCh`, and the code rechecks `exitCh` (non-blocking) immediately before **every** signal, including the `SIGKILL` after a TERM grace expiry. A signal is therefore only ever sent while `Wait` is still pending, i.e. while the kernel still associates the PID with our child. The broadcast design matters: the readiness poll observes exit via `exitCh` without consuming `waitCh`, so observation never hides the fact that the PID is already recyclable. No safety argument rests on zombie-reaping folklore.

State file format, atomic publication, validation, and the lifecycle lock live in `internal/state`; this section is the orchestration in `internal/client`.

### Canonical API

```go
// LifecyclePaths (defined in internal/state) names every file the lifecycle
// touches, so callers — and tests — can point the whole operation at isolated
// locations. The lock path is explicit, NOT derived from a global default:
// two callers operating on different state files must not share a lock.
type LifecyclePaths struct {
    StatePath string // state file
    LogPath   string // spawned server's stdout/stderr log
    LockPath  string // persistent lifecycle lock (flock)
}

type EnsureOpts struct {
    Paths     state.LifecyclePaths // production: built from the config path helpers
    ServerBin string               // "" => locateServerBinary()
}

// EnsureResult reports the verified base URL and whether this call spawned a
// server (true only when a new process was started, not on verified reuse).
type EnsureResult struct {
    BaseURL   string
    IsStarted bool
}

// EnsureLocalServer guarantees a verified local server is running.
func EnsureLocalServer(ctx context.Context, opts EnsureOpts) (EnsureResult, error)

// StopLocalServer stops the managed local server (authenticated shutdown), if
// one can be verified. ErrNotRunning: no state file. ErrForeignServer: state
// file exists but identity could not be verified — nothing is signaled,
// nothing is removed.
func StopLocalServer(ctx context.Context, paths state.LifecyclePaths) error

var (
    ErrNotRunning       = errors.New("no local server is running")
    ErrForeignServer    = errors.New("local server identity could not be verified")
    ErrProtocolMismatch = errors.New("local server protocol version mismatch")
)

// ForeignServerError carries the offending PID/address for diagnostics;
// errors.Is(err, ErrForeignServer) holds via its Is method.
type ForeignServerError struct {
    PID     int
    Address string
    Cause   error
}
```

`cmd/cli` builds the production `LifecyclePaths` from `config.ClientStateFilePath()`, `config.ClientLogPath()`, and `config.ClientLifecycleLockPath()`. The lock file descriptor is opened with the standard Go flags, which include `O_CLOEXEC`, so a spawned server can never inherit and hold the lifecycle lock.

**Phase budgets** (normative; the caller's `ctx` always applies on top and can shorten any of them):

| Phase | Budget | Notes |
|---|---|---|
| Lock acquisition | 2s | `flock(LOCK_EX\|LOCK_NB)`, retry every 100ms |
| Identity verification (reuse) | 3s | one health round-trip, 500ms per attempt is not required here — a single 3s deadline suffices |
| Spawn readiness | 3s | poll state file every 50ms, then one health check (500ms budget) |
| Failed-spawn cleanup | 6s | fresh background context: ≤3s after `SIGTERM`, then ≤3s after `SIGKILL` |
| Stop: identity | 3s | one health round-trip |
| Stop: shutdown request | 3s | the `POST /shutdown` call itself |
| Stop: wait-for-exit | 8s | poll `processAlive` every 100ms until definitively dead (the server's own graceful bound is 5s) |

**Testability**: the lifecycle code operates through an unexported ops struct (process liveness/signals, state-file access, lock access, sleep) with production defaults; tests inject fakes to record signal calls and assert the foreign-process-never-signaled invariant. No globals, no mutable package state (per `../.agents/DESIGN_PATTERNS.md`).

### Server Binary Location

```go
func locateServerBinary() (string, error) {
    // 1. $MORNING_SERVER_BIN
    // 2. sibling of os.Executable() (e.g. /usr/local/bin/morning-server next to morning)
    // 3. exec.LookPath("morning-server")
}
```

### EnsureLocalServer Flow

Each phase runs under its own budget from the phase-budget table (the caller's `ctx` shortens everything). All failures clean up everything this call created. A single `cmd.Wait()` goroutine — `waitCh := make(chan error, 1); exitCh := make(chan struct{}); go func() { waitCh <- cmd.Wait(); close(exitCh) }()` — is started immediately after `Start` and is the **only** reaper for the child: `waitCh` carries the exit result, `exitCh` broadcasts completion (closed channels are observable many times without being consumed). `cmd.Process.Release()` is **never** called (it would contradict `Wait`'s ownership), and the goroutine simply outlives the function when the CLI exits before the (intentionally long-lived) child does.

0. **Prepare**: `MkdirAll(filepath.Dir(opts.Paths.StatePath), 0700)` (the state dir must exist before any lock, log, or state file). Acquire the **lifecycle lock** — `flock(LOCK_EX|LOCK_NB)` on `opts.Paths.LockPath`, retrying every 100ms for up to 2s or until `ctx` is done; failure → wrapped error `another process is starting the local server`. Hold the lock until the operation completes (defer release).
1. **Read / validate state**: `sf, err := state.ReadStateFile(opts.Paths.StatePath)` — **propagate `err`** (corruption, permissions, partial read → wrapped error; never fall through to spawn).
   - `sf == nil` → spawn (step 2).
   - `sf.Valid()` fails → untrusted state: if `sf.PID > 0` and its process is provably dead (liveness probe below) → `RemoveStateFile`, then spawn; otherwise (alive, or death unprovable) → `ForeignServerError` — no state change.
   - Valid state: `alive, err := processAlive(sf.PID)`:
     - `err` (unknown) → `ForeignServerError` (never guess liveness).
     - dead → `RemoveStateFile` (stale), then spawn.
     - alive → `Health(ctx, sf.Token)` (3s deadline; success = 200 + echoed token + current protocol):
        - verified → release the lock, **return** `{BaseURL: "http://" + sf.Address, IsStarted: false}` (verified reuse).
       - `401` / token mismatch / unreachable / protocol mismatch → `ForeignServerError` (no signal, no state change).
2. **Spawn** (under the lock):
   1. Locate the server binary (error: `cannot find morning-server binary`).
   2. Open the log file `O_CREATE|O_WRONLY|O_APPEND`, `0600`.
   3. Spawn:
      ```go
      cmd := exec.Command(bin, "--address", "127.0.0.1:0", "--state-file", opts.Paths.StatePath)
      cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} // detached session: survives parent exit
      cmd.Stdout, cmd.Stderr = logFile
      ```
      Note: **no `--token` argument** — the managed server generates its own token and publishes it in the state file. The token never appears in argv.
      - `cmd.Start()` fails → close the log, release the lock, return a wrapped error.
    4. Immediately start the reaper (step-4 snippet above: `waitCh` + `exitCh`), and record `spawnStart := time.Now().UTC()`.
    5. **Wait for publication + verification** (≤3s, 50ms interval):
       - Poll `ReadStateFile(opts.Paths.StatePath)` until a valid state file appears with `sf.PID == cmd.Process.Pid` **and** `!sf.StartedAt.Before(spawnStart)` (the server publishes atomically, no-replace, after a successful bind; the CLI holds the lifecycle lock, so any state file satisfying both conditions was published by this child).
       - A `ReadStateFile` **error** (anything other than the missing-file `nil, nil`) is a real I/O failure — it fails the spawn **immediately** (cleanup + wrapped cause), not after the readiness budget.
       - If `exitCh` closes during the poll → the child died early → cleanup (step 6); the child's own exit error is preserved in the returned error.
       - Then `Health(ctx, sf.Token)` (500ms budget) must verify (200 + echoed token + current protocol).
       - Success → close the parent's log fd, release the lock, **return** `{BaseURL, IsStarted: true}`. The `Wait` goroutine keeps running to reap the child when it eventually exits (normal for the CLI: the server outlives the CLI).
       - Timeout or health failure → cleanup (step 6).
    6. **Cleanup** (on timeout, health failure, child early exit, state-read failure, or `ctx` cancelled anywhere in step 2): use a **fresh** `context.WithTimeout(context.Background(), 6*time.Second)` — never the already-cancelled caller ctx:
       - **Before every signal** (`SIGTERM` and again before `SIGKILL`), recheck `exitCh` non-blockingly: if `Wait` has already completed, skip signaling entirely (its PID may already be recyclable).
       - Otherwise `SIGTERM` the child, wait on `exitCh` ≤3s; if still not done, `SIGKILL`, wait ≤3s more.
       - If the child is still not reaped after the SIGKILL grace, cleanup **returns an error** (termination unconfirmed) — it never reports success on an unconfirmed death. A non-nil exit result is reported only when the child exited on its own (early-exit path); an exit caused by our own signal is the expected outcome, not an error.
       - Re-read the state file; remove it **only if** `sf.PID == cmd.Process.Pid` (the child generates the token, so PID is the ownership marker on the spawn path).
       - Close the log, release the lock.
       - Return the wrapped error (`local server did not become ready`, `child exited early`, ...), or `ctx.Err()` when cancelled — the caller's `context.Canceled`/`context.DeadlineExceeded` identity is preserved through the chain.

Known residual case: if the CLI itself is killed (`SIGKILL`) between spawn and the server's state publication, the orphaned server keeps running untracked — it has no state file, so `morning server stop` will not find it. It cannot conflict (dynamic loopback port), but a `Setsid` session leader is **not** killed or reaped when the parent dies or the user logs out — it can persist until explicitly killed or until reboot. Accepted Phase 1 residual; the window is sub-second.

### StopLocalServer Flow

0. **Prepare**: `MkdirAll(filepath.Dir(paths.StatePath), 0700)`; acquire the lifecycle lock (same retry/deadline as Ensure).
1. `sf, err := state.ReadStateFile(paths.StatePath)` — propagate `err`; `sf == nil` → release the lock, `ErrNotRunning` (caller prints the friendly message).
2. **Validation**: invalid state is handled exactly as in Ensure step 1 — provably dead → `RemoveStateFile`, release, nil; otherwise → release, `ForeignServerError`.
3. `processAlive(sf.PID)`: unknown → release, `ForeignServerError`; dead → `RemoveStateFile`, release, nil (stale state from a crashed/stopped server).
4. `Health(ctx, sf.Token)` (3s budget; success = 200 + echoed token + current protocol): failure → release, `ForeignServerError` (no state change).
5. `Shutdown(ctx, sf.Token)` (`POST /shutdown`, 3s budget):
   - `2xx` → **wait for the recorded process to be definitively dead**: poll `processAlive(sf.PID)` every 100ms until it returns `(false, nil)`, budget 8s (the server's graceful bound is 5s). Connection refusal is *not* used as the completion signal — `http.Server.Shutdown` closes the listener while in-flight handlers still finish.
     - dead → re-read the state file and remove it **only if its PID and token still match** (guards against a concurrent generation having republished) → release, nil.
     - budget exhausted or liveness unknown → **keep** the state file, wrapped error (`local server did not confirm exit`). A fast-PID-reuse false positive is conservative (it blocks reuse) and is the correct failure direction: never delete state on an unproven death.
   - `401` → `ForeignServerError` (defensive; step 4 just verified).
   - Other error / timeout → **keep** the state file, wrapped error.

Note on zombies: a managed server is re-parented to the OS init/launchd after the spawning CLI exits, so a definitive `ESRCH` from the liveness probe means the process is fully gone — there is no zombie ambiguity on this path (unlike the spawn-cleanup path, where the child is this process's own). The one exception is `os.ErrProcessDone` from the probe: the Go runtime returns it when the probed PID is this process's **own already-reaped child** (e.g. stopping the server a sibling CLI invocation spawned in the same test/parent process). That is also a definitive death and is classified as dead, not unknown.

The CLI maps outcomes to exit codes: `0` (stopped, or none running), `2` (`ErrForeignServer`), `1` (anything else).

### processAlive

```go
// processAlive is a liveness *hint*, never an ownership decision. It returns
// (true, nil) for alive, (false, nil) for definitively dead, and (false, err)
// when the answer is unknown — callers must treat unknown as foreign, never
// as dead.
func processAlive(pid int) (bool, error) {
    if pid <= 0 { return false, errors.New("invalid pid") }
    proc, err := os.FindProcess(pid)
    if err != nil { return false, fmt.Errorf("find process %d: %w", pid, err) }
    err = proc.Signal(syscall.Signal(0)) // no-op signal
    switch {
    case err == nil, errors.Is(err, syscall.EPERM): // EPERM => different user, assume alive
        return true, nil
    case errors.Is(err, syscall.ESRCH), errors.Is(err, os.ErrProcessDone):
        // ESRCH: no such process. ErrProcessDone: this process's own
        // already-reaped child. Both are definitive death.
        return false, nil
    default:
        return false, fmt.Errorf("probe pid %d: %w", pid, err)
    }
}
```

---

## CLI Command Logic (in `cmd/cli`)

### Structure

`main()` is a two-liner calling `run(ctx, os.Args, os.Stdout, os.Stderr)`; the Cobra tree is built by an unexported `newRootCmd(out, errW) *cobra.Command` in the same package. `run` executes it with `ExecuteContext(ctx)` so `cmd.Context()` in every `RunE` carries real cancellation (SIGINT/SIGTERM from `signal.NotifyContext`), and all output goes to the injected writers so `package main` tests capture it without swapping `os.Stderr`.

```
morning
  brief [--server <url> | --standalone]   # fetch and display a stub briefing
  server
    stop                                   # stop the managed local server
```

### `morning brief`

```
1. cfg, err := config.LoadClientConfig()
2. Resolve target base URL by flag priority:
   a. --standalone: if cfg.Server.URL != "" → config.ClearServerURL()
      (transactional: mutates only server.url under the path-derived config lock;
      everything else preserved; profiles kept)
      res, err := client.EnsureLocalServer(ctx, ensureOpts)
    b. --server <url> (detected via `Flags().Changed("server")`, so an explicit
       `--server ""` fails URL validation instead of being treated as absent):
       config.SetServerConnection(url) — validates the URL strictly
       (http/https, host, no path/query/fragment/userinfo) and, in one transaction,
       sets server.url AND upserts the profile named "default"
       (product spec, Settings → Server profiles) → base = url
   c. cfg.Server.URL != "" → base = cfg.Server.URL (remote, no spawn)
   d. else → res, err := client.EnsureLocalServer(ctx, ensureOpts)
   (a and b are mutually exclusive: cmd.MarkFlagsMutuallyExclusive("server", "standalone"))
   ensureOpts.Paths = state.LifecyclePaths built from the three config path helpers.
3. If res.IsStarted, print "Starting local server..." to STDERR
    (stdout is reserved for the response body; res.IsStarted is false on verified reuse)
4. c, err := client.NewClient(base, nil)
5. resp, err := c.Brief(ctx, &api.BriefRequest{})
6. json.NewEncoder(os.Stdout).Encode(resp)   // exactly one line of JSON on stdout
```

- Success: exit 0. Failure: error to stderr, exit 1.
- Output contract: **stdout carries exactly one line of JSON** (the `BriefResponse`); everything else (progress, errors) goes to stderr. This keeps `morning brief` scriptable and matches the later TUI's non-stdout model.
- A failure after persistence (e.g. the remote URL is unreachable) does **not** roll back the saved URL — the error is reported and the user re-points with `--standalone` or a new `--server`.

### `morning server stop`

```
paths := state.LifecyclePaths{StatePath: <ClientStateFilePath()>, LogPath: <ClientLogPath()>, LockPath: <ClientLifecycleLockPath()>}
err := client.StopLocalServer(ctx, paths)
switch {
case err == nil:                                        → print "local server stopped", exit 0
case errors.Is(err, client.ErrNotRunning):              → print "no local server is running", exit 0
case errors.Is(err, client.ErrForeignServer):           → print the conflict (address, PID) to stderr, exit 2
default:                                                → print the error to stderr, exit 1
}
```

Idempotent: running with no state file is a success path.

---

## News Digest Engine (Phase 2: `internal/news`)

Product behavior (source configuration, checkbox defaults/retention, ticker filtering, topics) is normative in [PRODUCT.md → News digest](PRODUCT.md#1-news-digest). This section fixes the Phase 2 mechanics: item identity, sub-fetchers, per-source failure isolation, and the `POST /news` wire shape.

**Design constraints** (normative):

1. **Zero required keys at baseline.** RSS + YouTube channel feeds + DuckDuckGo search work with no API key. An optional `YOUTUBE_API_KEY` (transcript quality) and optional `BRAVE_API_KEY` (search quality) improve results but are never required.
2. **The first pass returns titles only.** No LLM summarization happens during `POST /news` — summaries are lazy (`POST /news/summary`, Phase 3).
3. **Per-source failure isolation**: one broken feed never fails the request; it contributes zero items and a log line.
4. **The server keeps no session state.** Checkbox state lives entirely in the CLI's session (Phase 5); the server's only contract is a stable `NewsItem.ID`.

### Domain Types and Item Identity (in `internal/api`)

```go
type NewsItem struct {
    ID          string    `json:"id"`           // Stable identity (below)
    Title       string    `json:"title"`
    Source      string    `json:"source"`       // Feed name, "YouTube: <channel>", or "[Topic: <topic>]"
    SourceURL   string    `json:"source_url"`
    PublishedAt time.Time `json:"published_at"`
}
```

**Item identity** (normative) — `ID` is the CLI-side checkbox key and must be stable across re-fetches:

| Item kind | `ID` |
|---|---|
| RSS feed article | canonical article URL (redirects resolved once at fetch time, query string stripped) |
| YouTube video | `yt:{channelID}:{videoID}` (both taken from the channel RSS entry) |
| Topic / web-search result | result URL |

Identity is computed by the fetcher at fetch time, not by the server — the server never re-derives it, and the CLI never re-derives it either.

### Engine and Fetcher Interface (in `internal/news`)

```go
// Defaults is the server.yaml news.* configuration as plain data. internal/briefing
// maps the config struct onto this at startup, so internal/news never imports internal/config.
type Defaults struct {
    Count     int
    TimeRange string
    Topics    []string
    Sources   map[string]bool // per-source enable map; missing key = enabled
}

// Fetcher pulls recent items from one configured source.
type Fetcher interface {
    Fetch(ctx context.Context) ([]api.NewsItem, error)
}

// Engine runs all enabled fetchers (plus topic queries) concurrently.
type Engine struct {
    fetchers []Fetcher
    search   *llm.WebSearch // shared search capability (Phase 3); nil => topics yield nothing (warn logged)
    defaults Defaults
}

func (e *Engine) Run(ctx context.Context, req api.NewsRequest) (api.NewsResponse, error)
```

Concrete types by default; the `Fetcher` interface is justified by three implementations + test doubles (consumer-defined, small, per `../.agents/DESIGN_PATTERNS.md`).

### `server.yaml` Extensions (Phase 2+ keys)

Phase 1's load-only `ServerConfig` gains sibling keys as phases land. A save API still does not exist — editing `server.yaml` remains a manual, user-driven act, and the loader keeps its missing-file/malformed-YAML semantics:

```yaml
address: ""            # Phase 1
news:                  # Phase 2
  count: 10            # default display count
  timeRange: 24h
  feeds:
    - name: "Yahoo News"
      url: "https://example.com/feed/rss"
  channels:
    - id: "UC..."      # YouTube channel ID
  topics: ["AI regulation", "Thai economy"]
  sources:             # per-source enable map (omitted key = enabled); key = feed `name` or the channel ID
    "Yahoo News": true
stocks:                # Phase 4
  count: 10
  refreshIntervalSeconds: 60
  moveThresholdPercent: 1.0
  cacheTTLSeconds: 60
  tickers:
    - { symbol: VOO, exchange: us }
    - { symbol: PTT, exchange: set }
llm:                   # Phase 3
  provider: claude     # claude | openai | ollama
  model: ""
  ollamaBaseURL: "http://127.0.0.1:11434"
  searchProvider: auto # auto = Brave when BRAVE_API_KEY is set, else DuckDuckGo
```

Keys are additive only; the Phase 1 loader must tolerate (not error on) these until each phase lands its own struct fields.

### Sub-Fetchers

- **`rss.go`** — one fetcher per feed URL under `server.yaml` `news.feeds`. Built on `github.com/mmcdole/gofeed` (handles RSS 2.0 and Atom). 10s HTTP timeout per feed; `ETag`/`If-Modified-Since` are stored in-process so a second refresh within the session is cheap. Entries missing a title are dropped (not an error); entries with an unparseable date get `PublishedAt` zero and sort last.
- **`youtube.go`** — two-stage, zero-key at baseline:
  1. **Discovery**: the channel RSS feed `https://www.youtube.com/feeds/videos.xml?channel_id={channelID}`, parsed with gofeed. Title/link/date from the entry; the channel display name is read once from the feed's author element and cached in-process for the `Source` tag (`"YouTube: <channel name>"`).
  2. **Transcript** (lazy — only on `POST /news/summary`, Phase 3): fetch the watch page, extract the caption `timedtext` reference from the page's embedded data, fetch the caption segments, and assemble plain text. No captions → typed `ErrNoTranscript` (surfaced, never guessed). With `YOUTUBE_API_KEY` set, the Data API caption endpoints may replace the watch-page route; decide at implementation time and record the chosen route here.
- **`scrape.go`** — goquery-based full-text extraction, used (a) for sources explicitly configured as scrape sources and (b) as the content path for summaries when the RSS entry body is too short. Heuristic: `<article>` element → `main` landmark → largest text block, in that order. Extraction yields nothing → typed `ErrNoContent`.
- **Optional `newsapi.go`** — the product spec lists "optional News API service". Do **not** implement until a provider is chosen; when it is, it joins as a fourth fetcher enabled by a `news.sources.newsapi` entry with a key from `NEWS_API_KEY`.

### Topic Queries (web search — not a fetcher)

`news.topics` entries are queried by calling the shared `llm.WebSearch` directly — one deterministic query per topic, **no LLM generation in the loop** (this is what keeps `POST /news` LLM-free, per the product spec's "no LLM summarization yet" first pass). They are never a `Fetcher` (product decision — topics have no feed). Queries run in parallel with the fetchers inside `Engine.Run`:

- Query shape: the topic string as-is; typed results become items with `Source: "[Topic: <topic>]"` and `ID` = result URL.
- Search unavailable (no key and DuckDuckGo unreachable) → topics contribute zero items and a warning is logged; the request still succeeds.
- A `Topics` field in the request is a **replacement list** (product decision, same semantics as `tickers`).

### Wire Types & Endpoint (added to `internal/api`, `internal/server`)

```go
type NewsRequest struct {
    Filter    string   `json:"filter,omitempty"`     // ticker symbol — ticker-scoped list (see below)
    Limit     int      `json:"limit,omitempty"`      // display count; 0 => server default
    Topics    []string `json:"topics,omitempty"`     // replacement list; empty => server default
    TimeRange string   `json:"time_range,omitempty"` // e.g. "24h", "7d"; empty => server default
    Sources   []string `json:"sources,omitempty"`    // per-source enable list (replacement); empty => server default
    Model     string   `json:"model,omitempty"`      // per-request LLM override (affects topic queries)
}

type NewsResponse struct {
    Items       []api.NewsItem `json:"items"`
    GeneratedAt time.Time      `json:"generated_at"`
}
```

`POST /news` handler semantics (normative):

1. **Ticker filter**: when `Filter` is set, the server **skips** all configured sources and topics and runs a single ticker-scoped web-search query via the search tool (e.g. `"<ticker> <company name> news"`); results are the entire list. Reset (`r`) is the client re-calling `POST /news` with no `Filter`.
2. **Concurrency**: enabled fetchers + topic queries run concurrently under `errgroup` with a **20s overall deadline** and per-source 10s timeouts.
3. **Partial success**: ≥1 source succeeded → `200` with the merged, deduplicated (by `ID`), `PublishedAt`-descending list truncated to `limit`. All sources failed → `502` `{"error": "all news sources failed"}`. Deadline hit → `504` in the same shape.
4. **Precedence**: explicit request field > `server.yaml` default (the CLI only sends fields the user changed; see the Client/Server API table in [PRODUCT.md](PRODUCT.md#clientserver-api)).

---

## LLM Engine & Agentic Tool Use (Phase 3: `internal/llm`)

Product behavior (provider abstraction, per-request model override, tool use, lazy summaries) is normative in [PRODUCT.md → LLM Provider Abstraction](PRODUCT.md#llm-provider-abstraction) and [→ Agentic Tool Use](PRODUCT.md#agentic-tool-use). This section fixes the Phase 3 mechanics.

**Design constraints** (normative):

1. **Acyclic imports**: `internal/llm` imports **nothing internal** — no `news`, no `stocks`, no `config` beyond the small config structs (or plain values passed by the caller). Domain tools are composed in `internal/briefing`/`cmd/server` and injected.
2. **Keys never in config files**: provider credentials are read from environment variables only (`ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, `BRAVE_API_KEY`, `YOUTUBE_API_KEY`, optional `NEWS_API_KEY`). `server.yaml` holds only provider name, model name, and non-secret settings.
3. **No fatal LLM errors**: every LLM path degrades to a typed error the caller surfaces in the panel — the process never crashes and a failed summary never blocks the briefing.

### Provider Abstraction

Built on `github.com/tmc/langchaingo` (product decision — considered-and-rejected alternatives are recorded in [PRODUCT.md](PRODUCT.md#llm-provider-abstraction)). The package exposes the minimal interface from the product spec:

```go
// Model is the LLM surface the server needs. Implemented by provider wrappers.
type Model interface {
    Generate(ctx context.Context, prompt string, tools ...llms.Tool) (string, error)
}

// Engine owns the provider selection and the model instance cache.
type Engine struct { /* provider, defaults, per-(provider,model) instance cache, Registry */ }

func NewEngine(cfg LLMConfig) (*Engine, error) // cfg: provider, default model, ollama base URL, search provider
func (e *Engine) ModelFor(override string) (Model, error) // "" => server default
```

- **Provider selection**: `server.yaml` `llm.provider` (`"claude" | "openai" | "ollama"`) + `llm.model`. Each provider is a thin wrapper file (`claude.go`, `openai.go`, `ollama.go`) around a LangChainGo `llms.Model` — no other LangChainGo surface (memory, chains) is used; if it becomes needed, revisit.
- **Per-request model override** (normative): `ModelFor(override)` resolves a provider-qualified string (e.g. `"openai:gpt-4o-mini"`; no colon → current provider, different model). The override chain is enforced once, in the caller: request `model` > client-config `llm.model` (the CLI forwards it as a request field) > `server.yaml` default.
- **Instance cache**: model clients are cached per (provider, model, baseURL) on the Engine — constructing an API client per call is a performance bug. The cache is on the struct, never a package global.
- **Missing credentials**: `NewEngine` succeeds without keys (Ollama always works; cloud providers are lazy). `ModelFor` for a keyless cloud provider → typed `ErrProviderNotConfigured`.

### Tool Interface & Registry

```go
// Tool is a callable the LLM may invoke mid-generation.
type Tool interface {
    llms.Tool                                        // name, description, JSON parameter schema
    Call(ctx context.Context, args map[string]any) (string, error)
}

type Registry struct{ /* name -> Tool */ }

func (r *Registry) Register(t Tool) error           // duplicate name => error
func (r *Registry) Tools() []llms.Tool              // LangChainGo-shaped, for the Generate call

// WebSearch is the shared search capability. It is constructed ONCE in
// internal/briefing and shared by two consumers:
//   - news.Engine (Phase 2): topic and ticker-scoped queries via the typed
//     Search method — deterministic, no LLM in the loop;
//   - the LLM as the web_search tool (below) — Call is the Tool implementation.
type WebSearch struct{ /* provider: auto | brave | duckduckgo (server.yaml llm.searchProvider) */ }

type SearchResult struct {
    Title   string
    Snippet string
    URL     string
}

func (w *WebSearch) Search(ctx context.Context, query string, maxResults int) ([]SearchResult, error)
func (w *WebSearch) Call(ctx context.Context, args map[string]any) (string, error) // Tool impl: formats results as "title — snippet (url)" lines
```

**Built-in tools (registered server-side at startup):**

1. **`web_search`** — the `Tool` implementation of the shared `WebSearch` above (Brave Search when `BRAVE_API_KEY` is set, else DuckDuckGo HTML — zero-key baseline). Params: `{query string, max_results int}`; returns the plain-text `title — snippet (url)` list. The same instance also serves topic queries (Phase 2), ticker-scoped news (Phase 2), and move explanations (Phase 4) through its typed `Search` method or as tool context.
2. **Fetcher-as-tools** — the news sources and the stock tracker are exposed as callable tools (`fetch_source {name}`, `fetch_quotes {tickers}`) so a summary can re-query a source on demand instead of relying on pre-fetched context. Constructed in `internal/briefing` from the domain engines and registered at startup — this is what keeps `internal/llm` domain-free.

**Call budget** (normative): a `Generate` with tools runs at most **3 tool rounds** and a **30s tool budget** total; exceeding either aborts generation with the partial text and a log line — no unbounded agent loops.

### Lazy Summarization (`POST /news/summary`)

```go
type NewsSummaryRequest struct {
    ItemID    string `json:"item_id"`             // NewsItem.ID from the list
    Verbosity string `json:"verbosity,omitempty"` // "" | "short" (default) | "detailed"
    Model     string `json:"model,omitempty"`
}

type NewsSummaryResponse struct {
    Title       string    `json:"title"`
    PublishedAt time.Time `json:"published_at"`
    Summary     string    `json:"summary"`
    SourceURL   string    `json:"source_url"`
}
```

Mechanics:

1. **Resolve the target** from `ItemID` alone (URL directly; `yt:{channelID}:{videoID}` → video). The client sends an ID, not a list index — the server does not depend on the client's list state.
2. **Fetch full content**: RSS entry body when long enough → else `scrape` fetch → for YouTube, the transcript stage (Phase 2). Content budget: truncate to the first ~12k characters before prompting.
3. **Prompt contract**: "Summarize the following in 2–3 sentences. Facts only, no preamble, no hedging." — `detailed` allows 3–5 sentences. Output capped at 120 words (short) / 200 (detailed); over-length output is truncated at a sentence boundary.
4. **Respond** with the wire type above; the CLI renders title / date / summary / source link (product spec, Sample run layout).

**Failure semantics** (normative):

| Failure | Status | Body |
|---|---|---|
| `ErrNoTranscript` | `422` | `{"error": "transcript unavailable", "title": …}` |
| `ErrNoContent` | `422` | `{"error": "content unavailable", "title": …}` |
| `ErrProviderNotConfigured` | `503` | `{"error": "llm provider not configured"}` |
| 15s request deadline | `504` | `{"error": "summary timed out"}` |

The Summary panel shows the notice (title + date still render); it never crashes and never auto-retries. No summary caching in Phase 3 — caching lands in Phase 7.

---

## Stock Tracking (Phase 4: `internal/stocks`)

Product behavior (ticker configuration, panel display, ticker-filtered news, auto-refresh, replacement-list semantics) is normative in [PRODUCT.md → Stock briefing](PRODUCT.md#2-stock-briefing). This section fixes the Phase 4 implementation mechanics: the data-provider stack, US/Thai symbol normalization, TTL caching, error behavior, and the `POST /stocks` wire shape.

**Design constraints** (normative):

1. **Zero required keys at baseline.** The baseline provider stack must work with no API key and no user registration. Optional key-based providers (Finnhub, Alpha Vantage, Twelve Data, EODHD — see the comparison below) remain a separate, unapproved addition.
2. **Coverage**: US equities/ETFs/indices (`VOO`, `AAPL`, `^GSPC`) **and** Thai equities/indices on the Stock Exchange of Thailand (`^SET.BK`, `PTT.BK`, `CPALL.BK`, `KBANK.BK`).
3. **Acyclic imports**: `internal/stocks` never imports `internal/news` — `internal/briefing` (or `cmd/server` wiring) composes them (PRODUCT.md, [dependency direction](PRODUCT.md#architecture)).
4. **No fatal stock errors**: a bad ticker degrades to a per-quote error row in the panel; it never fails the whole batch or the process.

### Data Source Options (US & Thailand)

Surveyed options for tracking US and Thai (SET) tickers:

| Source | US equities | Thai equities (SET) | Indexes | Key required | Notes |
|---|---|---|---|---|---|
| **Yahoo Finance** (unofficial JSON endpoints `query1/query2.finance.yahoo.com`) | ✅ | ✅ (`.BK` suffix) | ✅ (`^GSPC`, `^SET.BK`) | **None** | Single client covers both markets; soft IP rate limits (~2,000 req/h); 15-min delayed for some markets; requires a standard browser `User-Agent` header or requests 403. |
| **Stooq** (`stooq.com`, plain CSV) | ✅ (`.us` suffix) | ⚠️ index only (`^set`); individual Thai equities largely absent | ✅ | **None** | Documented CSV endpoints, no key; good as a fallback for US equities/ETFs; thin Thai equity coverage. |
| **Finnhub** | ✅ | ⚠️ exchange coverage varies by plan | ✅ | Free key (60 calls/min) | Clean REST + websocket; Thai coverage depends on exchange bundle — needs verification before adoption. |
| **Alpha Vantage** | ✅ | ⚠️ limited | ✅ | Free key (25 req/day) | `GLOBAL_QUOTE` endpoint; daily quota too small for TUI auto-refresh without heavy caching. |
| **Twelve Data / EODHD** | ✅ | ✅ (`.BK`) | ✅ | Free key (credit-limited, e.g. 800 req/day) | Best documented SET coverage of the key-based options; quota must be budgeted against the refresh interval. |
| **SET official API** (`api.set.or.th` Market Data API / SMART Marketplace) | — | ✅ (authoritative) | ✅ | Registered SET member/institutional credentials | Authoritative real-time source for SET + mai; **not usable** for a zero-config CLI — rejected for the baseline. |

**Decision** (per product acceptance criteria — no key, both exchanges, documented limits, explicit unknown-symbol behavior, per-quote staleness timestamp):

- **Primary: Yahoo Finance** — the only no-key source that covers both US and Thai equities under one client.
- **Fallback: Stooq** — engaged when Yahoo fails with a network error, non-2xx status, or 429 throttling. Stooq covers US equities/ETFs well; for Thai tickers a Stooq miss (or Stooq miss on an individual Thai equity) degrades to a per-quote error — Thai equities have no fallback by design, and that is accepted.
- Key-based providers are **out of scope** until a user requests them (see constraint 1).

### Package Layout

`internal/stocks` files (added in Phase 4; Phase 1 is unaffected):

```
internal/stocks/
  stocks.go      # TickerConfig, Quote, Provider, Tracker, NewTracker, sentinels
  normalize.go   # Yahoo/Stooq symbol normalization (pure, table-testable)
  yahoo.go       # YahooFinance provider
  stooq.go       # Stooq provider
  stocks_test.go, normalize_test.go, yahoo_test.go, stooq_test.go
```

Wire types (`StocksRequest`/`StocksResponse`) live in `internal/api` alongside the Phase 1 types; `internal/server` registers `POST /stocks`; `internal/briefing` (Phase 5) folds the Stocks section into `POST /brief`.

### Provider Types and Interface (in `internal/stocks`)

```go
// TickerConfig is one entry of server.yaml's stocks.tickers (also used in
// request bodies — see Wire Types).
type TickerConfig struct {
    Symbol   string `yaml:"symbol" json:"symbol"`     // e.g. "VOO", "PTT", "SET"
    Exchange string `yaml:"exchange" json:"exchange"` // "US" or "SET" (case-insensitive)
}

// Quote is one resolved ticker: either a price (Error empty) or a surfaced
// failure (Error non-empty, Price zero). A batch always returns one Quote
// per requested ticker, in request order.
type Quote struct {
    Symbol        string    `json:"symbol"`
    Exchange      string    `json:"exchange"`
    Price         float64   `json:"price"`
    Change        float64   `json:"change"`
    PercentChange float64   `json:"percent_change"`
    Currency      string    `json:"currency"`
    UpdatedAt     time.Time `json:"updated_at"`
    Explanation   string    `json:"explanation,omitempty"`
    Error         string    `json:"error,omitempty"`
}

// Provider fetches quotes for configured tickers.
type Provider interface {
    FetchQuotes(ctx context.Context, tickers []api.TickerConfig) ([]api.Quote, error)
}
```

**Wire-type placement** (normative): `TickerConfig` and `Quote` are **defined in `internal/api`** — they appear in request/response JSON; `internal/stocks` uses them as `api.TickerConfig` / `api.Quote`. This keeps the CLI side (`internal/render`) free of any `internal/stocks` import: the TUI sees wire types only.

**Sentinels**: `var ErrSymbolNotFound = errors.New("symbol not found")` and `var ErrNoProviderData = errors.New("no provider returned data")`. Provider HTTP errors are wrapped with context (`fmt.Errorf("yahoo fetch %s: %w", sym, err)`); `errors.Is` checks work through the chain.

### Primary Provider: Yahoo Finance

Unofficial JSON endpoints — **no key, no auth cookie**:

- **Single chart** (canonical): `GET https://query1.finance.yahoo.com/v8/finance/chart/{symbol}?interval=1d&range=1d`, where `{symbol}` is the **URL-escaped** Yahoo symbol (`^SET.BK` → `%5ESET.BK`).
- **Batch quote**: `GET https://query1.finance.yahoo.com/v7/finance/quote?symbols=A,B,C` (convenience; the chart endpoint is the canonical path because `v7/quote` has historically demanded a crumb/cookie — the implementation standardizes on `v8/finance/chart` and fans out per ticker).

**Requirements** (normative):

- `User-Agent: Mozilla/5.0` (browser-like) on every request — Yahoo 403s bare Go clients.
- Per-request `http.Client` timeout **10s**; the client **must** honor `ctx` cancellation.
- Parse from `chart.result[0].meta`: `regularMarketPrice` → `Price`, `chartPreviousClose`/`previousClose` → baseline for `Change`/`PercentChange`, `currency` → `Currency`, `regularMarketTime` → `UpdatedAt`.
- An error payload (`chart.error` with a non-`null` `code`, e.g. `"NOT_FOUND"`) maps to `ErrSymbolNotFound` for that symbol — a per-symbol degradation, not a batch failure.

**Yahoo symbol normalization** (`normalize.go`, pure function, table-tested):

| Input (`Symbol`, `Exchange`) | Yahoo symbol |
|---|---|
| `"SET"`, `"SET"` | `^SET.BK` (SET Index) |
| `"PTT"`, `"SET"` | `PTT.BK` |
| `"VOO"`, `"US"` | `VOO` |
| `"SPX"`, `"US"` (or symbol `^GSPC`) | `^GSPC` |

- `Exchange` is case-insensitive (`"set"`, `"Set"` ≡ `"SET"`); unknown exchange → wrapped `ErrSymbolNotFound` *before* any network call.
- A symbol that already carries a market suffix (`PTT.BK`, `^SET.BK`) is accepted verbatim.

### Fallback Provider: Stooq

- `GET https://stooq.com/q/l/?s={symbol}&f=sd2t2ohlcv&h&e=csv` — single line: `Symbol,Date,Time,Open,High,Low,Close,Volume`.
- Per-request timeout **10s**; same `ctx`/wrapping rules as Yahoo.
- `Change`/`PercentChange` are derived against the previous daily close: if the cache holds a prior close for the symbol use it, otherwise mark the quote with a zero change and an empty `Change` basis (the TUI renders `±0.0%` with the `UpdatedAt` timestamp rather than a wrong number).
- Stooq's "No data" line maps to `ErrSymbolNotFound` for that symbol.

**Stooq symbol normalization**:

| Input | Stooq symbol |
|---|---|
| `"VOO"`, `"US"` | `voo.us` |
| `"SPX"`, `"US"` | `^spx` |
| `"SET"`, `"SET"` | `^set` |
| `"PTT"`, `"SET"` | not covered → `ErrSymbolNotFound` (Thai equities are Yahoo-only) |

### The `Tracker` Engine (orchestration, cache, explanation)

```go
type Tracker struct {
    primary    Provider
    fallback   Provider
    cache      *sync.Map // map[string]cacheEntry; key: normalized "SYMBOL@EXCHANGE"
    ttl        time.Duration     // default 60s, configurable via server.yaml (stocks.cacheTTLSeconds)
    explainer  Explainer         // consumer-defined interface; nil disables explanations
    log        *slog.Logger
}

type Explainer interface {
    ExplainMove(ctx context.Context, q *api.Quote) (string, error)
}
```

`NewTracker(primary, fallback Provider, ttl time.Duration, explainer Explainer, log *slog.Logger) *Tracker`.

**Fetch lifecycle** (per `Tracker.FetchQuotes`, normative):

1. **Cache first**: a cache entry younger than `ttl` short-circuits — this coalesces the TUI's auto-refresh `POST /stocks` ticks (default interval 60s from `server.yaml`) so a 30s refresh cadence never hammers the upstream APIs.
2. **Primary**: `primary.FetchQuotes` for the uncached tickers.
3. **Fallback**: only for tickers whose primary result was a network error, non-2xx status, 429, or parse failure — `fallback.FetchQuotes` for exactly those. `ErrSymbolNotFound` from Yahoo is **not** retried against Stooq for Thai equities (known gap); for US tickers a Yahoo `NOT_FOUND` still retries Stooq (e.g. a Yahoo symbol gap).
4. **Per-quote degradation**: any ticker still unresolved becomes `Quote{Symbol, Exchange, Error: err.Error()}`. `FetchQuotes` returns `nil` error as long as *every* ticker produced a `Quote` (success or degraded row); it returns an error only when *no* ticker produced a quote at all (`ErrNoProviderData`), or when `ctx` is canceled.
5. **Move explanation**: for each successful quote with `|PercentChange| >= 1.0` (configurable via `server.yaml` `stocks.moveThresholdPercent`), call `explainer.ExplainMove` with a bounded context (**5s**); set `Quote.Explanation` on success, leave it empty on failure (explanation is cosmetic — it never fails the row). A quiet move leaves `Explanation` unset — no LLM spend.

**Explanation input** (dependency-safe): `Explainer` is defined in the consuming package (`internal/briefing` or `cmd/server` wiring) and implemented there using the ticker-scoped web-search query the product spec already requires for the News panel's ticker filter. `internal/stocks` stays free of `internal/news` imports.

### Wire Types & Endpoint (added to `internal/api`, `internal/server`)

Phase 1's stub `POST /brief` is unchanged; these are additive.

```go
type StocksRequest struct {
    Limit     int            `json:"limit,omitempty"`
    Tickers   []TickerConfig `json:"tickers,omitempty"` // replacement list, not a merge
    Verbosity string         `json:"verbosity,omitempty"`
    Model     string         `json:"model,omitempty"`
}

type StocksResponse struct {
    Quotes      []Quote   `json:"quotes"`
    GeneratedAt time.Time `json:"generated_at"`
}
```

- `POST /stocks`: request > client config > `server.yaml` default for every field; `Tickers` is a **replacement** list (product spec, bookmarked ticker semantics). `Limit` caps the number of quotes returned (most-recently-configured first) *after* fetching, so the response never contains a ticker the user did not ask for.
- Request bodies keep the Phase 1 **1 MiB** cap and JSON error shape; wrong method → JSON `405`.
- The endpoint's handler must fit the auto-refresh budget: per-ticker fan-out runs with a `errgroup` of bounded concurrency (4), so a 10-ticker refresh stays under ~2s end-to-end when providers are healthy.

### Edge Cases & Risks

| Risk / edge case | Impact | Mitigation |
|---|---|---|
| Yahoo IP throttling (429) during a long TUI session | Auto-refresh goes stale | 60s TTL cache coalesces ticks; 429 hands off to Stooq for US tickers; per-quote `UpdatedAt` keeps staleness visible. |
| Yahoo unofficial endpoint schema change | Silent parse drift | Parser tests against recorded fixture JSON; a missing `meta` field is a parse error → fallback path → per-quote error, never a zero-valued fake quote. |
| Invalid / delisted ticker | One bad row fails the batch | Per-quote `Error` field; other rows return normally (normative partial-success contract). |
| SET closed while US open (or vice versa) | Stale-looking prices | `UpdatedAt` per quote is the only staleness signal; the TUI shows the quote timestamp, and `Change` is always relative to the provider's previous close. |
| Circular `news` ↔ `stocks` import | Build failure | `Explainer` interface owned by the consumer; `briefing`/wiring supplies the implementation (constraint 3). |
| Stooq has no Thai equity coverage | Thai fallback impossible | Accepted by design (Decision above); Thai `ErrSymbolNotFound` surfaces a clear row error, never a crash. |

### Tests (added to the Verification Plan)

- **`internal/stocks`** (table-driven, `httptest.Server` for both providers):
  - `normalize.go`: US and SET normalization tables (including case-insensitive exchange, pre-suffixed passthrough, unknown-exchange rejection before any HTTP call).
  - Yahoo: fixture chart JSON → correct `Price`/`Change`/`PercentChange`/`Currency`/`UpdatedAt`; `chart.error` `NOT_FOUND` → `ErrSymbolNotFound`; 429 → wrapped error; 500 → wrapped error.
  - Stooq: fixture CSV line → correct parse; `"No data"` line → `ErrSymbolNotFound`.
  - `Tracker`: cache hit within TTL issues **zero** upstream requests; 429/500 from Yahoo → Stooq called for the affected tickers only; Yahoo `NOT_FOUND` on a Thai equity → no Stooq call, row carries `Error`; all-tickers-failed → `ErrNoProviderData`; partial failure → `nil` error with degraded row; `Explainer` invoked only when `|PercentChange| >= threshold`; explainer timeout leaves `Explanation` empty.
  - **`internal/server`**: `POST /stocks` happy path (JSON shape, one row per requested ticker, request order preserved), replacement `tickers` list honored, `limit` truncation, wrong method → JSON `405`.

---

## Unified Briefing & TUI Rendering (Phase 5: `internal/briefing`, `internal/render`)

Product behavior (66/33 layout, panel navigation, save inclusion matrix, page template) is normative in [PRODUCT.md → Briefing output](PRODUCT.md#3-briefing-output), [→ Interactive Rendering](PRODUCT.md#interactive-rendering), and [→ Panel Navigation](PRODUCT.md#panel-navigation). This section fixes the Phase 5 mechanics.

**Design constraints** (normative):

1. **The CLI never fetches or calls the LLM directly.** `internal/render` talks to the server exclusively over HTTP through `internal/client` — the TUI has zero imports of `news`, `stocks`, or `llm`.
2. **Nothing is written to disk until the user presses `s`.** The TUI is a review step, not an auto-writer; `q` quits and writes nothing.
3. **All state mutation happens inside the Bubble Tea `Update` loop.** Network calls run as background `tea.Cmd`s; messages flow back into the loop. No goroutine touches the model directly.

### Server Orchestrator (in `internal/briefing`)

```go
type Orchestrator struct {
    news   *news.Engine
    stocks *stocks.Tracker
    llm    *llm.Engine          // tool registry already composed (see below)
}

func (o *Orchestrator) Run(ctx context.Context, req api.BriefRequest) (api.BriefResponse, error)
```

- `Run` fans out the news flow and the stocks flow **concurrently** via `errgroup` under a **25s overall deadline** (each half already has its own internal deadlines — Phase 2/4 — so the outer one is a safety net).
- **Partial failure** (normative): one half fails → `200` with the failed half's section carrying the inline `error` (its `items`/`quotes` empty), the healthy half normal — e.g. `{"news": {"items": [...]}, "stocks": {"error": "all quotes failed"}}`. The TUI renders the failed panel with a notice. Both fail → `502` in the shared error shape.
- **`POST /brief` wire shape** (replaces the Phase 1 stub `BriefRequest`/`BriefResponse`; the stub's `Message` field is dropped):

```go
type BriefRequest struct {
    News   *NewsRequest    `json:"news,omitempty"`   // full nested override set (Phase 2)
    Stocks *StocksRequest  `json:"stocks,omitempty"` // full nested override set (Phase 4)
    Model  string          `json:"model,omitempty"`  // top-level override, applies to both halves
}

// Section wrappers embed the section wire types and add the inline error slot
// the partial-failure contract needs (embedded fields flatten in JSON).
type NewsSection struct {
    NewsResponse
    Error string `json:"error,omitempty"` // non-empty => this half failed; Items empty
}

type StocksSection struct {
    StocksResponse
    Error string `json:"error,omitempty"`
}

type BriefResponse struct {
    GeneratedAt time.Time     `json:"generated_at"`
    News        NewsSection   `json:"news"`
    Stocks      StocksSection `json:"stocks"`
}
```

- **Precedence** (normative, enforced once in `Run`): request field > client-config value (forwarded by the CLI as request fields) > `server.yaml` default. The top-level `model` fills any `news.model`/`stocks.model` the request left empty.
- **Tool wiring** happens here (or in `cmd/server`, whichever owns the composition root): `Orchestrator` construction registers the fetcher-as-tools and `web_search` into the `llm.Registry` — this is the single place the acyclic import rule is satisfied by wiring, not by package layout.

### TUI (in `internal/render`)

Charm stack (Bubble Tea / Bubbles / Lip Gloss / Glamour — see the [Future-phase dependencies](#future-phase-dependencies-p2p7) table). Entry point is called from `cmd/cli`'s `brief` command after the server is ensured:

```go
func Run(ctx context.Context, cl *client.Client, cc config.ClientConfig) error
```

**Model** (single struct, all fields mutated only in `Update`):

```go
type model struct {
    news       []api.NewsItem
    stocks     []api.Quote
    summary    *summaryState        // nil when the Summary panel is closed
    focus      panel                // news | stocks | summary
    cursors    [2]int               // per-panel scroll position
    checked    map[string]bool      // NewsItem.ID => included; default true on first sight
    summaries  map[string]string    // NewsItem.ID => rendered summary text (only opened items)
    filter     string               // active ticker filter, "" = default list
    loading    [panel]bool          // per-panel spinner state
    errMsg     map[panel]string     // per-panel error notice
    settings   *settings.Model      // nil unless the overlay is open
    refresh    time.Duration        // stocks auto-refresh interval
    savedPaths []string             // "saved to …" status line history
}
```

**Message-driven flow** (async commands, all server I/O):

| Trigger | `tea.Cmd` | Resulting messages |
|---|---|---|
| Startup | `POST /brief` | `briefDone` / `briefErr` → seeds both panels + `checked` defaults |
| `Enter` on a title | `POST /news/summary` | `summaryDone` / `summaryErr` → opens/updates Summary panel |
| `Enter` on a ticker | `POST /news {filter: ticker}` | `newsDone` (filtered list; `checked` map untouched) |
| `r` | `POST /news` (no filter) | `newsDone` (default list restored) |
| Auto-refresh tick | `POST /stocks` | `stocksDone` (cursor position preserved across refreshes) |
| Settings apply | per-field `POST /news` / `POST /stocks` | as above |

**Layout** (Lip Gloss):
- Main row: fixed **66% / 33%** width split (News / Stocks), each pane a `viewport` with its own `↓ more` hint when overflowing. Fixed — not user-resizable (product decision).
- Summary panel: **full-width, pops up below the row** when a title is opened (the 66/33 row is unaffected); closes with `esc`. Content order: title, date, summary (Glamour-rendered Markdown), source link at the bottom. Loading state = `spinner` widget until `summaryDone`.
- Focused pane gets a highlighted border; footer line shows context-sensitive hints (`filtered: VOO — r to reset`, `s to save`, etc.).

**Keyboard** (implements the [Panel Navigation](PRODUCT.md#panel-navigation) scheme):

| Key | Action |
|---|---|
| `Tab` / `Shift+Tab` | Cycle focus: News → Stocks → Summary (if open) → News |
| `1` / `2` / `3` | Direct jump (Summary only when open) |
| `↑` / `↓` | Navigate/scroll in the focused panel |
| `Space` (News) | Toggle the focused item's inclusion checkbox — keyed by `NewsItem.ID` in `checked` |
| `Enter` (News) | Open Summary panel for the focused title (lazy summary) |
| `Enter` (Stocks) | Ticker-filtered News re-fetch |
| `r` (News focused) | Reset the News list to default |
| `esc` (Summary open) | Close the Summary panel |
| `c` | Open the Settings overlay (Phase 6) |
| `s` | Save the markdown page (below) |
| `q` / `Ctrl+C` | Quit, nothing written |

**Auto-refresh**: a `tea.Tick` (re-armed on every `stocksDone` and on refresh-rate changes) dispatches the stocks re-fetch at `refresh` interval while the session is open.

### Save Flow (`s`)

1. Build the page strictly per the **save inclusion matrix** ([PRODUCT.md](PRODUCT.md#3-briefing-output)) — for each item in the **currently shown** News list:
   - checked + summary present in `summaries` → title + summary (as rendered);
   - checked + never opened → **title only** — zero LLM work at save time (normative);
   - unchecked → omitted.
   - Stocks rows: always written (no checkboxes).
2. **Page template** (mirrors the product spec's template exactly): YAML front matter (`date`, `tags: [morning-briefing]`), `# Morning Briefing — YYYY-MM-DD`, `## News` bullets (`- Title (Source)` with an indented `- **Summary**: …` sub-bullet where applicable), `## Stocks` bullets (`- **TICKER**: price (±x.x%) — explanation.`).
3. **Output path**: `output.path` from the client config with `YYYY-MM-DD` substitution, defaulting to `~/morning-briefings/YYYY-MM-DD.md` when unset. Parent directory created `0700` if missing.
4. **Atomic write**: temp file in the same directory → write → `fsync` → `rename` → directory `fsync` (same discipline as config writes). File mode `0644` (a personal briefing, not a secret).
5. Status line: `saved to <path>`; the session continues (the user can press `s` again after further edits, or `q` to quit).

---

## Settings Overlay & Live Sync (Phase 6: `internal/render/settings`)

Product behavior (tabs, apply/persistence semantics, server profiles) is normative in [PRODUCT.md → Settings](PRODUCT.md#settings). This section fixes the Phase 6 mechanics.

**Design constraints** (normative):

1. **Suspend, don't reflow**: the overlay is a full-screen Bubble Tea view swapped over the briefing view; the briefing model is preserved untouched underneath and redraws when the overlay closes.
2. **One atomic commit**: `esc` applies *all* edited fields in a **single** client-config transaction (the Phase 1 narrow-transaction API — one lock, one read, one atomic write). No field is persisted individually.
3. **No cancel/discard state** in this phase: an edited field is committed by `esc` (product decision; a draft mode may be added later without breaking the contract).

### Model

```go
type Model struct {
    tab      int        // 0 General | 1 News | 2 Stocks
    fields   fieldSet   // loaded snapshot of client.yaml (the diff baseline)
    edits    map[field]any // only dirty fields
    cursor   int        // field index within the active tab
    editing  bool       // inline edit mode for the focused text field
    errLine  string     // inline validation error (cleared on next valid edit)
}
```

**Tabs and fields** (exactly the product spec's three tabs):

| Tab | Fields (client.yaml keys) |
|---|---|
| General | `output.path`, `llm.model`, `server` profile switch (+ add/remove named `{name, url}` entries), `stocks.refreshIntervalSeconds` |
| News | `news.count`, `news.topics` (add/remove list), `news.timeRange`, `news.sources` (per-source `space` toggles), `news.verbosity` |
| Stocks | `stocks.count`, `stocks.bookmarks` (add/remove list of `SYMBOL@exchange`) |

### Navigation

`←`/`→` switch tabs; `↑`/`↓` move between fields; `enter` enters inline edit of the focused field (list fields: `enter` adds, `delete`/`backspace` removes at the cursor position); `space` toggles checkbox fields (sources); `esc` closes **and applies** (or just closes when nothing changed — the file is not rewritten).

### Inline Validation (before `esc` can commit)

| Field | Rule |
|---|---|
| `output.path` | non-empty; valid path template (`YYYY-MM-DD` allowed anywhere) |
| `llm.model` | empty (server default) or the Phase 3 override format: `model` or `provider:model` |
| `refreshIntervalSeconds` | integer ≥ 5 (sub-5s hammers the zero-key providers) |
| `news.count` / `stocks.count` | integer 1–50 |
| `news.timeRange` | matches a supported set (`"6h"`, `"24h"`, `"7d"`, …) |
| `stocks.bookmarks` entries | strict `SYMBOL@exchange` form; the symbol normalizer from Phase 4 (`internal/stocks`) is reused for the validation — an unknown exchange is rejected in the field, not at fetch time |
| `server` profile URL | the Phase 1 strict-URL rules (http/https, host, no path/query/fragment/userinfo) |

Invalid input is rejected **in the field** with an inline message; the field keeps its last valid value and is not marked dirty.

### Apply & Live-Sync Dispatch (on `esc`)

1. **Persist**: if `edits` is non-empty, one transactional client-config write (the Phase 1 `yaml.Node` round-trip discipline applies — unknown sibling fields survive).
2. **Dispatch re-fetches immediately** (no restart — normative, per the product spec):

| Changed field(s) | Effect |
|---|---|
| `news.count`, `news.topics`, `news.timeRange`, `news.sources`, `llm.model` | re-fetch the News panel (`POST /news` with the changed fields) |
| `stocks.bookmarks` | re-fetch Stocks (`POST /stocks` with the new **replacement** ticker list) |
| `stocks.refreshIntervalSeconds` | re-arm the `tea.Tick` auto-refresh |
| `llm.model` (also) | takes effect on the **next** `POST /news/summary`; already-rendered summaries are not regenerated |
| `output.path` | client-side only; next save |
| `server` profile switch | copies the profile URL into `server.url` (same transaction) **and** — when the URL actually changed — tears down and re-ensures the connection: standalone mode re-runs `EnsureLocalServer`; remote mode re-creates the HTTP client. The overlay closes **after** the new connection is verified, or stays open with an error line if it fails |

3. **Status line** reports `settings saved` / `saved, re-fetching news…` etc.

---

## Production Polish & Operational Runbooks (Phase 7)

Product intent is roadmap slice 7 ([PRODUCT.md → Roadmap](PRODUCT.md#roadmap)): error handling/retries, caching, scheduling docs, deployment docs. This section fixes the Phase 7 mechanics.

### Log Rotation & Redaction

Extends the Phase 1 `server.log` discipline (append, `0600`):

- **Size-based rotation**: before appending, if the log is ≥ 10 MiB, rename to `server.log.1` (overwriting an existing `.1`) and start fresh. One generation — no unbounded history.
- **Redaction filter** (normative): a single scrubbing `io.Writer` wraps the log sink. It masks:
  - `Authorization: Bearer <token>` values (the 64-hex instance token) — always, even if a handler accidentally logs the header;
  - any `*_API_KEY` value appearing in an error or debug line, by matching the key's value read from the environment at startup (never log the variable name + value pair);
  - request/response bodies are **never** logged (request logging is method + path + status + latency only).
- The CLI side logs nothing beyond one-line stderr diagnostics; all server detail lives in `server.log`.

### Retry & Caching

- **Retries**: zero-key providers get one retry with 2s backoff on `429`/`5xx` only (Yahoo, Stooq, DuckDuckGo). LLM calls are **never** auto-retried (cost) — the user retries via `Enter`.
- **Caching** (server-side, in-process, TTL-bounded):
  - News fetcher results: 5-minute TTL per source (a manual re-fetch via `r`/ticker filter bypasses the cache — freshness is the point of those actions).
  - Summaries: no cache (Phase 3 decision stands; revisit only if a provider is the bottleneck).
  - Stock quotes: the Phase 4 60s TTL already covers this — unchanged.

### Scheduling (runbooks, committed as `docs/scheduling.md`)

Interactive sessions (`morning brief` opens a TUI) are not cron-shaped, so scheduling docs cover two modes:

- **macOS `launchd`** (`~/Library/LaunchAgents/com.user.morning-brief.plist`): `StartCalendarInterval` at a user-chosen time launching the CLI via `osascript`-free `Terminal` open **or** a detached `script`/`tmux` session for headless capture; `RunAtLoad: false`. The runbook shows the plist and the `launchctl load`/`unload` commands.
- **Linux systemd user units** (`~/.config/systemd/user/morning-brief.{service,timer}`): `OnCalendar=` timer + `Type=forking` service opening a `tmux` session (the TUI needs a PTY); `ExecStart=tmux new-session -d -s morning-brief morning brief`. Plus a plain **cron** line for users who prefer it (`0 7 * * * tmux new-session -d -s morning-brief morning brief`).
- The runbook must state: the server is **not** scheduled — scheduling the CLI is enough because the CLI auto-spawns/reuses the local server (Phase 1 lifecycle).

### Deployment Topology (runbook, committed as `docs/deployment.md`)

- **Standalone** (default): nothing to deploy; the lifecycle section above covers it.
- **Remote / always-on (trusted)**: same `cmd/server` binary, started manually (systemd system unit or container) **without** `--state-file`; `--address` from `server.yaml`. The CLI points at it with `--server <url>`. Normative caveats carried from the product spec: remote mode is **trusted-endpoints-only** — there is no remote auth in this phase (open question in the product spec), so the runbook mandates loopback/LAN-private binding and states that public exposure is out of scope.

---

## Verification Plan

Automated verification is the acceptance gate; the manual smoke run is a secondary, fully isolated check. The old plan's destructive real-user-directory tests are retired.

### Required checks (all must pass)

```
gofmt -l .                    # no output
go vet ./...
go test ./...
go test -race ./...
go build ./...
GOOS=darwin GOARCH=arm64 go build ./...   # cross-compile gate: every supported OS/arch
GOOS=darwin GOARCH=amd64 go build ./...
GOOS=linux  GOARCH=arm64 go build ./...
GOOS=linux  GOARCH=amd64 go build ./...
```

### Unit tests (table-driven, `t.TempDir()`, `httptest`)

- **config**: load missing file → zero value; load valid; load malformed → wrapped error; `SetServerConnectionTo` preserves unknown sibling keys, comments (where yaml.v3 retains them), **and `server.profiles`** (round-trip through `yaml.Node`; assert semantic preservation, **not** byte-for-byte formatting); writes are atomic (a simulated interrupted write never leaves a truncated file); concurrent transactions under the path-derived config lock never corrupt the document, and a custom path uses its own sidecar lock; URL validation rejects non-http(s), hostless, paths, queries, fragments, and userinfo **before** writing; `SetServerConnection` upserts the `default` profile; `ClearServerURL` clears `url` but keeps profiles; path helpers honor `HOME`/`XDG_STATE_HOME` overrides via `t.Setenv` (environment-dependent tests never run in parallel).
- **state (internal/state)**: create/read round-trip; `CreateStateFile` never overwrites an existing file (`EEXIST`); **readers never observe partial JSON** (concurrent-reader race test); remove on missing path → nil; malformed JSON → wrapped error, not a zero value; `Valid()` rejects PID ≤ 0, zero `StartedAt`, empty/non-64-hex tokens, non-loopback and malformed addresses; lifecycle lock: acquire/release, contended acquire does not block, `ctx` cancellation, lock is released after the holder's fd is closed (crash simulation in a subprocess).
- **client**: `Brief` success (assert exact URL join, method, body), non-2xx (assert status in error), malformed JSON, `context.DeadlineExceeded`, pre-cancelled `ctx`, oversized response → error; `NewClient` URL validation table; the default client follows **no redirects** (a 302 to a 200 is not accepted as health); `Health` 200 with echoed token verified, 200 with a different token → `ErrForeignServer`, 401 → `ErrForeignServer` (address in error), protocol mismatch → `ErrProtocolMismatch`, wrong status; `Shutdown` 2xx, 401 → `ErrForeignServer`, non-2xx.
- **server**: `POST /brief` happy path (assert JSON shape), empty body, malformed JSON → 400, oversized body → 413, `GET /brief` → JSON 405, unknown path → JSON 404, `Content-Type: application/json` on **all** responses; `GET /healthz` with correct Bearer → 200 + token + `protocol_version: 1`, missing/wrong Bearer → 401; untokened server: `GET /healthz` → 200 with `token: ""`, `POST /shutdown` → 403; `Handler()` is usable **without `Serve`**: an authenticated `POST /shutdown` through `httptest.NewServer(s.Handler())` does not panic (shutdown channel exists from `New`) and a second concurrent shutdown request cannot double-close it; `POST /shutdown` with correct Bearer → 200 then `Serve` returns cleanly (bounded shutdown), wrong Bearer → 401 and server keeps serving; `Serve`: listener failure → non-nil error; ctx cancel → clean nil return; shutdown timeout → forced close, non-nil error.
- **stocks (internal/stocks, Phase 4)**: see [Stock Tracking → Tests](#tests-added-to-the-verification-plan) for the full table-driven suite (normalization, Yahoo/Stooq fixture parsing, fallback and cache behavior, partial-success contract, `POST /stocks` endpoint shape).
- **news (internal/news, Phase 2)**: `rss` — gofeed fixture (RSS 2.0 and Atom) → correct `ID`/`Title`/`Source`/`SourceURL`/`PublishedAt`; canonical-URL identity (redirect resolved once, query stripped); entry with missing title dropped, unparseable date → zero time sorting last; malformed feed XML → wrapped error for that source only. `youtube` — channel RSS fixture → `ID` of the form `yt:{channelID}:{videoID}`, channel name read from the author element and cached; transcript assembly from fixture timedtext segments; fixture page with no captions → `ErrNoTranscript`. `scrape` — fixture HTML: `<article>` wins over `main` wins over largest block; page with no extractable content → `ErrNoContent`. `Engine.Run` — two fake fetchers under `errgroup`: one failing still returns the other's items with `nil` error; both failing → `ErrAllSourcesFailed`; merged list deduplicated by `ID` and sorted `PublishedAt` descending; `limit` truncation; ticker `Filter` set → fetchers **not** invoked, search-tool path used instead; topic query with search registry nil → zero topic items, no error.
- **llm (internal/llm, Phase 3)**: `ModelFor` override table (empty → default provider+model; `provider:model` → qualified; bare model → current provider, new model; unknown provider → wrapped error); keyless cloud provider → `ErrProviderNotConfigured`; instance cache returns the same `Model` for the same (provider, model) and a distinct one for a different pair; `Registry.Register` duplicate name → error, `Tools()` shape matches the LangChainGo `llms.Tool` contract; tool-call budget: a fake `Model` requesting 4 rounds is cut off at 3 with the partial text; `web_search` tool: Brave path (fixture `httptest` server) and DuckDuckGo path both produce the `title — snippet (url)` list, unreachable search → wrapped error, not a panic.
- **briefing (internal/briefing, Phase 5)**: `Run` with fake news/stocks engines — both succeed → `BriefResponse` with both sections; news fails / stocks succeeds (and the mirror) → partial-success contract (200, failed section carries the inline error); both fail → `ErrAllSectionsFailed`; precedence resolution: request field > forwarded client field > default, and top-level `model` fills empty `news.model`/`stocks.model` only.
- **render (internal/render, Phase 5)**: the save builder is a pure function over (shown list, `checked`, `summaries`) and is table-tested against the **full save inclusion matrix** (all 4 rows × a stocks-present variant): checked+opened → title + summary; checked+never-opened → **title only** (assert zero LLM-client calls during save); unchecked+opened and unchecked+unopened → absent; stocks rows always present. Front matter (`date`, `tags: [morning-briefing]`) and the `YYYY-MM-DD` path substitution (default `~/morning-briefings/`) are asserted byte-exactly against the product template; atomic write: a simulated failure between temp-write and rename leaves the target file untouched. Focus cycling (`Tab`/`Shift+Tab` over the 2- and 3-panel states, `1`/`2`/`3` direct jump including the "summary not yet open" rejection) is driven through `Update` with synthetic `tea.KeyMsg`s.
- **settings (internal/render/settings, Phase 6)**: validation table (bookmark `SYMBOL@exchange` via the Phase 4 normalizer, refresh ≥ 5, counts 1–50, URL rules, `timeRange` set) — each invalid input leaves the field at its last valid value and un-dirty; dirty tracking: only edited fields are in `edits`; apply with zero edits → the config writer is **not** called; apply with edits → exactly **one** config transaction and the expected re-fetch dispatch commands (news fields → `POST /news` fields, bookmarks → `POST /stocks` replacement list, refresh rate → tick re-arm, model → next-summary-only); profile switch → `server.url` copied and, only when the URL changed, the connection re-ensure is dispatched.
- **stop (unit, injected ops)**: no state file → `ErrNotRunning`; dead PID → state removed, nil; alive PID + 401 health → `ErrForeignServer`, **and** a sentinel "foreign" process is provably never signaled and the state file is untouched (assert via recorded ops); shutdown initiated → waits until the recorded PID is definitively dead, state removed only when PID **and** token still match; death-wait budget exhausted → state kept, wrapped error; liveness unknown → `ErrForeignServer` (or state kept), never removal.
- **cmd/server flags (unit)**: managed mode (set `--state-file`) with a non-loopback resolved address (e.g. `0.0.0.0:8787`) → usage error before listening; managed mode publishes a 64-hex token it generated itself (never an external value).

### Integration tests (real process, hermetic)

In `internal/client` (or a dedicated `internal/client/e2e_test.go`), with all paths under `t.TempDir()`:

1. **Full lifecycle**: build the real `cmd/server` binary once into a temp dir (`go build -o`), point `EnsureOpts.ServerBin` at it. `EnsureLocalServer` → assert returned URL serves `Brief`, state file holds the child's PID + a 64-hex token that `GET /healthz` echoes back (the token is server-generated; the CLI must verify it, not supply it), `IsStarted: true`; second `EnsureLocalServer` → same URL and **same** state-file PID, `IsStarted: false` (reuse, no second process); `StopLocalServer` → process definitively dead, state file gone; again → `ErrNotRunning`.
2. **Concurrent starts**: two goroutines call `EnsureLocalServer` simultaneously with the same paths → exactly one server process ends up verified; both callers return the same base URL; no orphaned second process; lock released afterward.
3. **Ensure vs Stop race**: `EnsureLocalServer` and `StopLocalServer` run concurrently → serialized by the lifecycle lock; final state is consistent (no running server without state, no state file for a dead process).
4. **Stale state**: write a state file with a dead PID → `EnsureLocalServer` removes it, spawns fresh, new PID.
5. **Foreign state**: start a non-morning server on a loopback port, write a state file with a different token and that live PID → `EnsureLocalServer` returns `ErrForeignServer` and does **not** touch the process (assert it is still alive and serving afterward).
6. **Invalid state**: state file with a malformed token / non-loopback address / dead-PID-unknown → refused as foreign when death is unprovable; removed when provably dead.
7. **Spawn failure cleanup**: `ServerBin` pointing at a binary that exits immediately (or a bad path) → `EnsureLocalServer` errors, no state file, no orphaned process (assert no zombie via the recorded PID, no leftover lock holders), log file closed.
8. **Cancellation**: cancel `ctx` mid-spawn → child is terminated and reaped (verified dead, not a zombie), state removed only if the child published it (PID match), lock released.
9. **Crash recovery**: SIGKILL a running managed server → next `EnsureLocalServer` removes the dead-PID state and spawns fresh; `StopLocalServer` on dead-PID state → nil.
10. **Server conflict exit**: a managed-mode `cmd/server` started while a state file exists exits 1 with a conflict error and leaves the existing state file untouched.
11. **Briefing assembly over a real HTTP server (no network I/O)**: assemble a real `internal/server.Server` through `httptest.NewServer` with the real `Orchestrator` but injected fake news/stocks engines and a fake LLM (constructor injection — no env keys, no external calls). Assert: `POST /brief` with nested `news`/`stocks` overrides returns both sections with the overrides applied end-to-end (precedence chain observable in the fake engines' recorded args); `POST /news` with `filter` returns only the ticker-scoped items; `POST /news/summary` with a transcript-less fixture ID → `422` `transcript unavailable` and with a fixture transcript → the wire shape `{title, published_at, summary, source_url}`.

Run all integration tests under `-race`.

### Manual smoke run (optional, isolated)

Only after the automated gates pass. Everything runs under a **temporary HOME** so the real user's config/state is never touched:

```bash
export SMOKE_HOME="$(mktemp -d)"          # trap 'rm -rf "$SMOKE_HOME"' EXIT
export HOME="$SMOKE_HOME" XDG_STATE_HOME="$SMOKE_HOME/.local/state"
mkdir -p bin
go build -o bin/morning ./cmd/cli && go build -o bin/morning-server ./cmd/server
export MORNING_SERVER_BIN="$(pwd)/bin/morning-server"
STATE_DIR="$SMOKE_HOME/.local/state/morning-cli"    # macOS: "$SMOKE_HOME/Library/Application Support/morning-cli"

./bin/morning brief          # spawn: one JSON line on stdout, $STATE_DIR/server.lock appears
./bin/morning brief          # reuse: same state-file PID
./bin/morning server stop    # "local server stopped", state file gone
./bin/morning server stop    # "no local server is running", exit 0
./bin/morning brief --server http://127.0.0.1:9999   # fails (nothing there) but client.yaml now has the URL + default profile
./bin/morning brief --standalone   # URL cleared, local server spawned again
```

**Phase 2+ extension** (added as each phase lands, same temporary-HOME discipline): with a fixture `server.yaml` (one RSS feed, one YouTube channel, two tickers) and provider keys set, run `./bin/morning brief` in a real terminal and manually verify, in order: titles with source tags render (P2); `Enter` on a title shows the Summary panel with loading → summary, `esc` closes it (P3); `Enter` on a ticker re-filters the News panel and `r` resets (P4); the 66/33 layout, focus cycling, `space` checkboxes, and `s` writing a markdown page that matches the save inclusion matrix byte-for-byte on re-check (P5); `c` opens the Settings overlay and `esc` persists exactly one config change with the live re-fetch (P6). The interactive steps cannot be script-asserted — this stays a manual gate.

---

## Notes for Implementation (invariants)

1. **Identity before action**: reuse and stop both require the live server's authenticated `GET /healthz` to return 200 **and echo back the state file's token** **and** report the current protocol version. `processAlive` is a liveness hint only — a PID is a hint; the token is proof, and the server does the comparison.
2. **No PID-from-disk signaling**: `StopLocalServer` never signals anything — it sends `POST /shutdown` to the verified identity and removes state only after the recorded process is **definitively dead** (connection refusal is not the completion signal). The only signals ever sent target the direct child of the current invocation, and only while its `Wait` is still pending — the code rechecks the reaper's `exitCh` broadcast before every signal.
3. **Lifecycle lock**: every Ensure/Stop operation holds the exclusive `flock` on the explicit `LifecyclePaths.LockPath` from first state read to final state removal. The lock file is persistent, never deleted, and the kernel releases it on holder death — there is no stale-lock recovery path to implement or test beyond crash-release. Lock paths are per-location (never a global default), and Go's `O_CLOEXEC` guarantees a spawned server cannot inherit the lock fd.
4. **Atomic no-replace publication**: state appears via temp-file + `Sync` + `os.Link` + directory `Sync`; readers never observe partial JSON; an existing state file is never overwritten (conflict is an error, and `cmd/server` exits 1 on it). The server publishes *after* a successful bind with a token **it generated itself**, so the state file always describes a listening server whose credential never crossed the process boundary via argv.
5. **Strict validation, no silent errors**: `StateFile.Valid()` gates all use; managed mode additionally validates its own token and enforces numeric-loopback addressing before listening; `ReadStateFile` errors are always propagated; liveness "unknown" is treated as foreign, never as dead.
6. **Single reaper, no Release**: one `cmd.Wait()` goroutine per spawned child is the only reaper; `cmd.Process.Release()` is never called. Failed-spawn and cancellation cleanup run under a fresh background context bounded by the phase budgets — never the already-cancelled caller ctx.
7. **Readiness is `GET /healthz`, never `POST /brief`** — the real `/brief` will perform paid LLM work, and polling it at startup would cause duplicate, costly, side-effecting calls.
8. **Dynamic port, not fixed port**: `127.0.0.1:0` removes the fixed-port collision class; the bound port comes back through the state file. Manual servers keep the `127.0.0.1:8787` default via `server.yaml`.
9. **No server self-cleanup**: a stopped or crashed server leaves its state file; the next CLI lifecycle operation (under the lock) removes it after proving the recorded PID dead. State mutation has exactly one owner (`internal/state`, invoked by the CLI lifecycle and by `cmd/server` at publish time only).
10. **Signal handling lives in `cmd/server`** (`signal.NotifyContext` derived from the context passed into `run`); `internal/server.Serve(ctx, ln)` owns the bounded shutdown state machine, its shutdown channel exists from `New` (so `Handler()` is safe standalone), and it stays signal-free and testable.
11. **Listener failure is fatal**: a bind/listen error returns from `Serve` and exits non-zero — never log-and-wait-forever.
12. **Loopback-only, token-protected control plane**: managed mode enforces `127.0.0.1` at address resolution; Bearer tokens are compared in constant time and are server-generated (never in argv, never logged); lifecycle HTTP clients have the environment proxy disabled (`Transport.Proxy` is nil) and follow no redirects. **Threat model**: accidental conflicts (stale state, recycled PIDs, unrelated benign services) — not malicious same-user processes.
13. **Append-mode log** at `LifecyclePaths.LogPath` (0600) accumulates debugging output across restarts; no secrets or tokens in it, ever.
14. **Errors**: wrapped with context at each layer (`fmt.Errorf("ensure local server: %w", err)`); sentinels (`ErrNotRunning`, `ErrForeignServer`, `ErrProtocolMismatch`) plus the `ForeignServerError` type for the states the CLI distinguishes and reports.
15. **Phase budgets are explicit**: the phase-budget table is normative — lock acquisition, identity verification, spawn readiness, failed-spawn cleanup, stop identity, shutdown request, and wait-for-exit each have their own bound; nested waits must fit their enclosing budget, and the caller's `ctx` can always shorten them.
16. **Doc drift guard**: if the product spec's Client/Server API table or the local-server-discovery section changes, the Wire Types note and the Ensure/Stop flows in this file must be re-checked — the Phase 1 stub ignores all `BriefRequest` fields, so request-shape changes are forward-compatible, but lifecycle changes are not.
