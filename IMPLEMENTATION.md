# Phase 1 Implementation: Client/Server Split

This document captures the full architecture and design for Phase 1 of `morning-brief` (Client/Server scaffold). It is written to enable a future session or agent to resume implementation without re-reading the full product spec or conversation history.

## Overview

Phase 1 goal: scaffold two Go binaries (CLI client and server) in one module, with a stub HTTP endpoint, standalone auto-spawn/detect logic via a lockfile, `--server`/`--standalone` flag persistence to config, and `morning server stop` to kill the spawned server. No LLM, news, stocks, or TUI work — just enough to prove the architecture end-to-end.

**Module**: `github.com/toucham/morning-brief` (go 1.26.2)

**Binaries**:
- `cmd/cli/main.go` → compiled to `morning` (or `bin/morning`) — CLI entrypoint
- `cmd/server/main.go` → compiled to `morning-server` (or `bin/morning-server`) — server entrypoint

**User-facing commands**:
- `morning brief` — fetch a stub briefing from the server, print it. Flags: `--server <url>` (use and persist remote), `--standalone` (use local), neither (use whatever is saved in config, or local if nothing saved).
- `morning server stop` — stop a locally spawned server and clean up the lockfile.

---

## Directory Layout

```
cmd/
  cli/
    main.go        # CLI binary entrypoint; defines Cobra root "morning" + subcommands (brief, server stop)
  server/
    main.go        # Server binary entrypoint; loads ServerConfig, creates Server, calls ListenAndServe()

internal/
  client/          # API client + local server spawn/detect + lockfile management (standalone mode)
  server/          # HTTP server: Server struct, Handler(), endpoint handlers for POST /brief
  config/          # Config loading: ClientConfig, ServerConfig structs; XDG path helpers
  api/             # Shared wire types: BriefRequest, BriefResponse — imported by client and server
```

Rationale: canonical Go project structure with clear separation. `cmd/{cli,server}` contain only the two binaries' `main()` functions and their direct command/entrypoint logic (Cobra definitions in `cmd/cli`, config loading + server startup in `cmd/server`). All implementation details (HTTP client, server handlers, config parsing, wire types) live in `internal/*` so they can be imported by both binaries without duplication. This matches the spec's own Architecture section description.

---

## Dependencies

Add when implementing (not yet):

```
go get github.com/spf13/cobra@latest  # CLI framework
go get gopkg.in/yaml.v3@latest        # YAML config
```

**Rationale for Cobra**: Phase 1 needs a two-level subcommand tree (`morning brief`, `morning server stop`) plus mutually-exclusive flags (`--server` vs `--standalone`) with good `--help`. Stdlib `flag` doesn't handle this cleanly. Cobra also pairs naturally with Charm's Bubble Tea (the TUI framework for later phases), so adopting it now avoids a rewrite.

**Rationale for yaml.v3**: Direct struct unmarshal/unmarshal suffices. Avoid `viper` (env/flag-merging magic, remote config support) — it would obscure the explicit `--server` → `client.yaml` persistence behavior the spec cares about.

**Stdlib coverage**: `net/http` (Go 1.22+ `ServeMux` with method+pattern routes), `os/exec` + `syscall.SysProcAttr` (process spawning), `os.FindProcess`/`Signal(0)` (liveness checks) cover everything else.

---

## Configuration

### Paths (Go stdlib, platform-native)

Use Go's standard library `os.UserConfigDir()` and `os.UserCacheDir()` functions to resolve platform-specific paths. This eliminates hand-rolled environment-variable logic and ensures correct paths on every platform: Linux follows XDG conventions, macOS uses native `Library/` directories, and Windows uses native `%AppData%` / `%LocalAppData%` locations. Paths will differ by OS — this is intentional and correct.

**Rationale**: No custom helpers needed — call `os.UserConfigDir()` and `os.UserCacheDir()` directly. Both can return an error (e.g., `$HOME` unset); propagate it in the path functions rather than silently falling back. Note: Go stdlib provides no `UserStateDir()` equivalent; use `os.UserCacheDir()` for lockfile and log paths since they are regenerable, ephemeral runtime artifacts.

Path functions to implement (in `internal/config`):
```go
func ClientConfigPath() (string, error)      // {os.UserConfigDir()}/morning-cli/client.yaml
func ServerConfigPath() (string, error)      // {os.UserConfigDir()}/morning-server/server.yaml
func ClientLockfilePath() (string, error)    // {os.UserCacheDir()}/morning-cli/server.lock
func ClientLogPath() (string, error)         // {os.UserCacheDir()}/morning-cli/server.log
```

**Platform-specific paths** (reference; actual locations determined at runtime):
| Platform | Config example | Cache/state example |
|----------|---|---|
| Linux | `~/.config/morning-cli/client.yaml` | `~/.cache/morning-cli/server.lock` |
| macOS | `~/Library/Application Support/morning-cli/client.yaml` | `~/Library/Caches/morning-cli/server.lock` |
| Windows | `%AppData%\morning-cli\client.yaml` | `%LocalAppData%\morning-cli\server.lock` |

### Client Config

Stored at the path returned by `ClientConfigPath()` (platform-specific location determined by `os.UserConfigDir()`).

YAML file, struct:
```go
type ClientConfig struct {
    Server ServerConn `yaml:"server,omitempty"`
}

type ServerConn struct {
    URL string `yaml:"url,omitempty"`  // "" => standalone mode
}
```

Load/Save functions:
```go
func LoadClientConfig() (ClientConfig, error)  // missing file => zero value, no error
func SaveClientConfig(cfg ClientConfig) error  // mkdir -p parent, write 0644
```

**Design for future growth**: Later phases add sibling keys to `ClientConfig` (`news:`, `stocks:`, `llm:`, `output:`) without reshaping `ServerConn` or the Load/Save signatures.

### Server Config

Stored at the path returned by `ServerConfigPath()` (platform-specific location determined by `os.UserConfigDir()`).

YAML file, struct:
```go
type ServerConfig struct {
    Address string `yaml:"address,omitempty"`  // bind address; default "127.0.0.1:8787"
}
```

Load/Save functions:
```go
func LoadServerConfig() (ServerConfig, error)
func SaveServerConfig(cfg ServerConfig) error
```

**Note**: No API keys in config — they live in env vars, added in a later phase when LLM provider integration happens. This struct can grow without reshaping when that arrives.

### Lockfile

Stored at the path returned by `ClientLockfilePath()` (platform-specific location determined by `os.UserCacheDir()`).

JSON file (not YAML — machine-only, never hand-edited), struct:
```go
type Lockfile struct {
    PID       int       `json:"pid"`
    Address   string    `json:"address"`        // e.g. "127.0.0.1:8787"
    StartedAt time.Time `json:"started_at"`
}
```

Functions:
```go
func ReadLockfile(path string) (*Lockfile, error)    // propagate os.ErrNotExist
func WriteLockfile(path string, lf *Lockfile) error  // mkdir -p, json.Marshal, 0644
func RemoveLockfile(path string) error               // ignore os.ErrNotExist
func (lf *Lockfile) IsAlive() bool                   // see Liveness Check below
```

### Server Log

Stored at the path returned by `ClientLogPath()` (platform-specific location determined by `os.UserCacheDir()`).

Plain text log file. Spawned server's `Stdout` and `Stderr` are redirected here for debugging. Append-mode, 0644.

---

## Stub API (in `internal/api` and `internal/server`)

### Request/Response Types

Wire types defined in `internal/api`:

```go
type BriefRequest struct{} // empty for Phase 1; later phases add filter/limit fields

type BriefResponse struct {
    GeneratedAt time.Time `json:"generated_at"`
    Message     string    `json:"message"`  // canned stub text
}
```

### Endpoints

Registered in `internal/server` using Go 1.22+ `ServeMux` pattern syntax:

**`POST /brief`** (the only endpoint at this phase)
- Read body as `BriefRequest` (empty for now, but JSON-deserialize to set the pattern).
- Return canned `BriefResponse{ GeneratedAt: time.Now().UTC(), Message: "stub brief: no real content yet" }`.
- Content-Type: `application/json`.

Note: No `/healthz` or other endpoints at Phase 1. The spec's Roadmap item 1 and Client/Server API table mention only `/brief` for this phase; staleness detection and spawn readiness use different mechanisms (see Spawn / Detect / Lockfile Logic section).

### Server Struct and Lifecycle

Defined in `internal/server`:

```go
type Server struct {
    cfg config.ServerConfig
    // httpServer *http.Server (created in ListenAndServe)
}

func New(cfg config.ServerConfig) *Server

func (s *Server) Handler() http.Handler {
    mux := http.NewServeMux()
    mux.HandleFunc("POST /brief", s.handleBrief)
    return mux
}

func (s *Server) ListenAndServe() error {
    addr := s.cfg.Address
    if addr == "" { addr = "127.0.0.1:8787" }
    
    httpSrv := &http.Server{Addr: addr, Handler: s.Handler()}
    
    // Install SIGTERM/SIGINT handler -> httpSrv.Shutdown(ctx)
    // This allows "morning server stop" to kill the process gracefully.
    
    return httpSrv.ListenAndServe()
}
```

### HTTP Client (in `internal/client`)

```go
type Client struct {
    baseURL string
    http    *http.Client
}

func New(baseURL string) *Client {
    return &Client{
        baseURL: baseURL,
        http: &http.Client{Timeout: 10 * time.Second},
    }
}

func (c *Client) Brief(ctx context.Context, req api.BriefRequest) (*api.BriefResponse, error) {
    // POST /brief, JSON marshal/unmarshal
}
```

---

## Spawn / Detect / Lockfile Logic (in `internal/client`)

### Design: Why spawn/detect is needed

Per the spec, a standalone local server is **not** auto-killed when the CLI exits — it "stays running across invocations for faster subsequent starts." This is why the lockfile exists: to track whether a server is already running locally so the CLI can reuse it, and why `morning server stop` exists: to give the user an explicit way to shut that persistent server down.

### Liveness Check

`IsAlive()` on a `Lockfile` (pure PID check, no HTTP involved):
```go
func (lf *Lockfile) IsAlive() bool {
    proc, err := os.FindProcess(lf.PID)
    if err != nil { return false }
    err = proc.Signal(syscall.Signal(0))  // signal 0 = no-op, just check if we can signal
    if err == nil || errors.Is(err, syscall.EPERM) { return true }  // EPERM => different user, assume alive
    if errors.Is(err, syscall.ESRCH) { return false }  // no such process => stale
    return false
}
```

### Spawn / Detect / Reuse

**Default bind address**: `127.0.0.1:8787` — note that the spec's "Open Questions" section lists the exact port/address and loopback-only binding as unresolved. This is a placeholder pending resolution; loopback-only is preferred for security.

```go
const defaultAddress = "127.0.0.1:8787"

func locateServerBinary() (string, error) {
    // 1. Check MORNING_SERVER_BIN env var
    // 2. Check sibling of os.Executable() (e.g. if cli is at /usr/local/bin/morning, check /usr/local/bin/morning-server)
    // 3. Fall back to exec.LookPath("morning-server")
}

func EnsureLocalServer(lockPath, logPath string) (address string, err error) {
    // 1. Try to read lockfile
    // 2. If exists and IsAlive() -> reuse, return address
    // 3. Else (stale or missing):
    //    a. Locate server binary
    //    b. Spawn: exec.Command(binPath, "--address", defaultAddress)
    //       - Set SysProcAttr{Setsid: true} for detached session (Unix only)
    //       - Redirect Stdout/Stderr to logPath for debugging
    //       - cmd.Start(), then cmd.Process.Release() (no need to reap parent's short lifetime)
    //    c. Record PID
    //    d. Poll POST /brief until it responds or timeout (~2s) — see waitForReady below
    //       (no dedicated health endpoint; use the actual endpoint for readiness)
    //    e. Write fresh lockfile
    //    f. Return address
}

func waitForReady(baseURL string, timeout time.Duration) error {
    // Retry client.Brief(ctx, BriefRequest{}) every ~50ms until success or timeout
    // Use a context with the timeout; back off on connection-refused errors
    // Once POST /brief responds with any status/body, consider the server ready
}
```

### Stop Local Server

```go
func StopLocalServer(lockPath string) error {
    lf, err := ReadLockfile(lockPath)
    if errors.Is(err, os.ErrNotExist) {
        fmt.Println("no local server is running")
        return nil
    }
    if err != nil { return err }
    
    // Send SIGTERM (graceful shutdown, triggers http.Server.Shutdown() in cmd/server)
    proc, _ := os.FindProcess(lf.PID)
    if proc != nil {
        _ = proc.Signal(syscall.SIGTERM)
    }
    
    // Poll IsAlive() with backoff (~100ms per iteration) up to ~3s
    // If still alive, send SIGKILL
    
    // Always remove the lockfile regardless of signal outcome
    return RemoveLockfile(lockPath)
}
```

---

## CLI Command Logic (in `cmd/cli` and `cmd/server`)

### `morning brief` (defined in `cmd/cli`)

Cobra `RunE` pseudocode:
```
1. Load ClientConfig from internal/config
2. Determine base URL by checking flags in priority order:
   a. If --standalone flag: clear config.Server.URL, save, call internal/client.EnsureLocalServer
   b. Else if --server <url> flag: set config.Server.URL = <url>, save, use it
   c. Else if config.Server.URL already set: use it (remote, no spawn)
   d. Else: call internal/client.EnsureLocalServer (default standalone)
3. Construct HTTP client via internal/client.New(baseURL)
4. Call client.Brief(ctx, api.BriefRequest{})
5. Print raw stub response (real rendering via Bubble Tea is a later phase)
```

Flags:
- `--server <url>`: use a remote server and persist the URL to `client.yaml` so it becomes the default
- `--standalone`: clear any saved `server.url`, revert to standalone mode
- These two flags are mutually exclusive: `cmd.MarkFlagsMutuallyExclusive("server", "standalone")`

### `morning server stop` (defined in `cmd/cli`)

Subcommand under `server`:
```
Call internal/client.StopLocalServer(internal/config.ClientLockfilePath())
```

### `cmd/server` (server binary entrypoint)

Simple entrypoint that loads config and starts the HTTP server:
```
1. Parse optional --address flag (overrides config)
2. Load ServerConfig via internal/config.LoadServerConfig()
3. Override Address if --address flag was provided
4. Create internal/server.Server with the config
5. Call server.ListenAndServe() (blocks until SIGTERM/SIGINT triggers graceful shutdown)
```

### Root Command Structure

```
morning                     # root command (defined in cmd/cli/main.go or a subpackage)
  brief [--server <url> | --standalone]  # fetch and display a stub briefing
  server
    stop                    # stop a locally spawned standalone server
```

---

## Verification Plan (for when logic is implemented)

**Platform note**: Paths below use Linux-style examples (`~/.cache/`, `~/.config/`) for clarity. On macOS, these resolve to `~/Library/Caches/` and `~/Library/Application Support/` respectively; on Windows, to `%LocalAppData%` and `%AppData%`. All paths are determined at runtime via `os.UserCacheDir()` and `os.UserConfigDir()`.

**Setup**:
- `go build ./...` succeeds with no errors
- Build named binaries: `go build -o bin/morning ./cmd/cli && go build -o bin/morning-server ./cmd/server`
- Set `MORNING_SERVER_BIN=$(pwd)/bin/morning-server` in env, or put `bin/` on `PATH`

**Test 1: Spawn path (first run, no lockfile)**
```bash
# Remove stale lockfile (adjust path for your platform; example uses Linux)
rm -f ~/.cache/morning-cli/server.lock
./bin/morning brief
```
Expected:
- Stub JSON printed to stdout
- Lockfile created at the path determined by `ClientLockfilePath()` (e.g., `~/.cache/morning-cli/server.lock` on Linux)
- `cat` the lockfile, note the PID
- Verify: `ps -p <pid>` shows the process is running with command `morning-server` or similar

**Test 2: Reuse path (second run, lockfile still valid)**
```
./bin/morning brief
```
Expected:
- Same stub output
- **Lockfile PID is unchanged** — no new process spawned
- Verify: `pgrep -fl morning-server` shows exactly one process, with PID matching the lockfile

**Test 3: Stale-lockfile path (kill process, lockfile untouched)**
```bash
# Kill the server by PID from lockfile (adjust path for your platform; example uses Linux)
kill -9 $(cat ~/.cache/morning-cli/server.lock | jq .pid)
./bin/morning brief
```
Expected:
- Detects dead PID via `Signal(0)` check (no HTTP involved)
- Spawns a new server
- Lockfile now contains a **different PID**
- Verify: `cat ~/.cache/morning-cli/server.lock` shows new PID, `ps -p <new-pid>` shows it running

**Test 4: `morning server stop`**
```
./bin/morning server stop
```
Expected:
- Process for the PID in the lockfile is gone
- Lockfile file is deleted
- Verify: `ps -p <pid>` fails (no such process), lockfile is gone from the path determined by `ClientLockfilePath()` (e.g., `~/.cache/morning-cli/server.lock` on Linux)
- Run again with no lockfile:
  ```bash
  ./bin/morning server stop
  ```
  Expected: prints "no local server is running" (or similar friendly message), exits 0

**Test 5: `--server` persistence**
```bash
./bin/morning brief --server http://127.0.0.1:9999
```
Expected:
- Command fails with a connection error (nothing listening on that port) — this is expected
- But **before** the error, check: `cat $(morning-cli-config-path)/client.yaml` contains `server: { url: http://127.0.0.1:9999 }` (where `morning-cli-config-path` resolves to `~/.config/morning-cli` on Linux, `~/Library/Application Support/morning-cli` on macOS, etc.)
- Run again with no flags:
  ```
  ./bin/morning brief
  ```
  Expected: attempts to use `http://127.0.0.1:9999` (the persisted URL), does not spawn a local server

**Test 6: `--standalone` persistence**
```bash
./bin/morning brief --standalone
```
Expected:
- Falls through to `EnsureLocalServer` (reuses if a server from Test 2 is still up, else spawns fresh)
- Check: `cat $(morning-cli-config-path)/client.yaml` shows `server: {}` or `server: { url: '' }` or absent (platform-specific config directory as per Test 5)

**Test 7: Mutual exclusivity**
```
./bin/morning brief --server http://x --standalone
```
Expected:
- Cobra rejects with "flags cannot be used together" or similar, does not proceed

---

## Notes for Implementation

- **Graceful shutdown**: `cmd/server` should install a signal handler (SIGTERM/SIGINT) that calls `http.Server.Shutdown(ctx)` on the running server, not just letting the process exit — this allows `morning server stop` to trigger a clean shutdown instead of a hard kill.
- **Process.Release()** after `Start()` is important so the parent CLI can exit without blocking on the child, and without creating a zombie.
- **Spawn readiness via `POST /brief` retries**: since there's no dedicated `/healthz` endpoint, `waitForReady` should poll `client.Brief(ctx, BriefRequest{})` with ~50ms backoff, tolerating connection-refused errors until the server is ready. This proves both network reachability and that the server is actually responding to requests.
- **PID liveness is the only staleness check**: `IsAlive()` uses `Signal(0)` alone — no HTTP call involved in deciding whether a lockfile is stale.
- **Append-mode log file** (path determined by `ClientLogPath()`, e.g. `~/.cache/morning-cli/server.log` on Linux): redirect `cmd/server`'s stdout/stderr here so multiple server restarts accumulate debugging output instead of overwriting.
- **Error handling**: `EnsureLocalServer` should return clear errors (can't find binary, spawn failed, never became ready). `StopLocalServer` should be idempotent (missing lockfile => print "no local server is running" and return nil, not error).
