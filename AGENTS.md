# Agent Guidelines for Morning Brief Development

This file contains development guidelines for implementing the morning-brief project. All LLM coding agents (Claude, Copilot, Cursor, etc.) should read and apply these guidelines to ensure consistent, idiomatic, and maintainable Go code.

This is the LLM-agnostic `AGENTS.md` standard — the agent-facing counterpart to `README.md` (which is for humans). Detailed guidance lives in `.agents/` subdirectory.

## Agent Discovery

This file is the single source of truth for agent-facing guidelines and is auto-loaded by the agents below. **Do not duplicate its content** — keep the per-tool entry points as thin symlinks to this file so the rules are loaded on every session without manual prompting.

| Agent | Entry point (symlink → `AGENTS.md`) |
|-------|--------------------------------------|
| OpenCode, Codex, Gemini, most AGENTS.md-aware tools | `AGENTS.md` (this file, read directly) |
| Claude Code | `CLAUDE.md` |
| Cursor | `.cursorrules` |
| GitHub Copilot (agent mode) | `.github/copilot-instructions.md` |

If you add a new agent tool that uses a different conventions file, add a symlink to this file and a row to the table above. The detailed style, architecture, design, and testing guides live in [`.agents/`](.agents/) and are referenced throughout this file.

## Quick Reference

| Document | Purpose |
|----------|---------|
| **[docs/PRODUCT.md](docs/PRODUCT.md)** | **Canonical product specification** — features, behavior, API, roadmap. Wins over all other docs for product behavior |
| **[docs/IMPLEMENTATION.md](docs/IMPLEMENTATION.md)** | System implementation guide (Phases 1–7): directory layout, config structs, spawn/detect/stop lifecycle, CLI resolution flow, news/LLM/stocks/briefing/TUI/settings mechanics, verification plan. Wins over other docs for implementation mechanics |
| **[.agents/ARCHITECTURE.md](.agents/ARCHITECTURE.md)** | Project structure, when to create packages/structs/interfaces, domain logic placement, package boundaries |
| **[.agents/CODE_STYLE.md](.agents/CODE_STYLE.md)** | Naming conventions, documentation, comments, code organization, nil checks |
| **[.agents/DESIGN_PATTERNS.md](.agents/DESIGN_PATTERNS.md)** | Dependency injection, error handling, interfaces, composition, concurrency, testing patterns |
| **[.agents/TESTING.md](.agents/TESTING.md)** | Test organization, table-driven tests, mocks, error testing, concurrency, coverage |

---

## How to Use These Guides

### Before Writing Code
1. Read **[docs/PRODUCT.md](docs/PRODUCT.md)** to understand the product behavior you are implementing (it wins for behavior)
2. Read **[docs/IMPLEMENTATION.md](docs/IMPLEMENTATION.md)** to understand the scope and mechanics of the phase you are implementing (Phases 1–7)
3. Read **[.agents/ARCHITECTURE.md](.agents/ARCHITECTURE.md)** to understand package structure and where your code belongs
4. Check **[.agents/DESIGN_PATTERNS.md](.agents/DESIGN_PATTERNS.md)** for the appropriate patterns

### While Writing Code
1. Apply naming conventions from **[.agents/CODE_STYLE.md](.agents/CODE_STYLE.md)**
2. Refer to **[.agents/CODE_STYLE.md](.agents/CODE_STYLE.md)** for documentation and comments
3. Use patterns from **[.agents/DESIGN_PATTERNS.md](.agents/DESIGN_PATTERNS.md)** for interfaces and error handling

### When Writing Tests
1. Follow **[.agents/TESTING.md](.agents/TESTING.md)** for test organization and patterns

### After Writing Code
Use this **Code Review Checklist** before submitting:

- [ ] Package organization matches the Phase 1 layout `cmd/{cli,server}` + `internal/{api,client,config,server,state}` (later phases add `render`, `news`, `stocks`, `briefing`, `llm` — see docs/PRODUCT.md and .agents/ARCHITECTURE.md)
- [ ] All exports have doc comments (see [.agents/CODE_STYLE.md](.agents/CODE_STYLE.md))
- [ ] Errors are wrapped with context using `fmt.Errorf("%w", ...)`
- [ ] Error strings are lowercase with no trailing punctuation (e.g. `errors.New("state file is nil")`)
- [ ] No boolean error flags (`hasError`/`isError`); return an `error` value instead
- [ ] Initialisms are consistently cased (`apiURL`, `userID`, `ServeHTTP` — never `apiUrl`, `userId`)
- [ ] Empty slices use the nil form `var s []T`; preallocate known capacities (`make([]T, 0, cap)`)
- [ ] Nil checks use guard clauses (early returns)
- [ ] Concrete types by default; any interface is small (1-3 methods), consumer-defined, and justified by a second implementation or a test double; add a compile-time assertion (`var _ Iface = (*T)(nil)`) where a type is a contract implementer
- [ ] No global state; dependencies injected via constructors
- [ ] `context.Context` is first parameter for functions that need cancellation
- [ ] Naming follows conventions: lowercase packages, CamelCase exports, consistent receivers
- [ ] Tests use table-driven pattern where applicable
- [ ] Deferred cleanup (`defer`) for files, locks, goroutines
- [ ] Goroutine lifetimes are obvious: context-cancellable and synchronized exit (no fire-and-forget)
- [ ] No panic for expected errors; return errors instead
- [ ] Type assertions use comma-ok idiom (safe, not panicking)

---

## Key Principles Summary

### Architecture
- **Thin cmd/, thick internal/**: Entrypoints only in `cmd/`; all logic in `internal/` (Cobra command definitions in `cmd/cli` are the sanctioned exception)
- **One responsibility per package**: `client`, `server`, `config`, `api`, `state`
- **Platforms**: macOS and Linux only; do not add or imply Windows support
- **Concrete-first, consumer-defined interfaces**: start with concrete types; add a small interface in the consuming package only when a second implementation or a test double genuinely needs one

### Design
- **Dependency injection**: Pass dependencies as constructor params; introduce consumer-owned interfaces only on proven need
- **Error wrapping**: Add context using `fmt.Errorf("%w", ...)` as errors bubble up
- **Small interfaces**: when one is added, 1-3 methods; allows composition and easy mocking
- **Defer for cleanup**: Guarantees cleanup on early return

### Code Style
- **Naming**: lowercase packages, CamelCase exports, consistent receivers (c, s, etc.)
- **Initialisms**: consistent casing everywhere — `apiURL`, `userID`, `ServeHTTP` (never `apiUrl`, `userId`)
- **Booleans**: clear adjectives/predicates; no boolean error flags — return an `error` value
- **Error strings**: lowercase, no trailing punctuation; wrap with `%w`
- **Slices**: prefer nil slices (`var s []T`); preallocate known capacities
- **Interfaces**: consumer-defined, small, compile-time asserted (`var _ Iface = (*T)(nil)`)
- **Goroutines**: obvious lifetimes — context-cancellable, synchronized exit
- **Guard clauses**: Early returns reduce nesting
- **Comments**: Explain why, not what; document all exports
- **No global state**: Explicit dependencies
- **Full reference**: [.agents/CODE_STYLE.md](.agents/CODE_STYLE.md)

### Testing
- **Table-driven**: Multiple test cases in one test function
- **Lightweight mocks**: Define in test files, inject via constructor
- **Error testing**: Test both success and failure paths
- **Race detector**: `go test -race ./...`

---

## References

These guides are based on:
- [Effective Go](https://go.dev/doc/effective_go)
- [Go Code Review Comments](https://go.dev/wiki/CodeReviewComments)
- [Google's Go Style Guide](https://google.github.io/styleguide/go/)
- [Standard Go Project Layout](https://github.com/golang-standards/project-layout)
- [Go Error Handling (Go 1.13+)](https://go.dev/blog/go1.13-errors)

---

## Quick Start Template

When creating a new file in `internal/`, follow this template:

```go
// Package mypackage does X.
package mypackage

import (
    "context"
    "errors"
    "fmt"
)

// ErrSomething indicates a problem.
var ErrSomething = errors.New("something went wrong")

// MyType represents X.
type MyType struct {
    field1 string
    field2 int
}

// NewMyType creates a new MyType.
func NewMyType(field1 string) *MyType {
    return &MyType{field1: field1}
}

// DoSomething performs an action.
func (m *MyType) DoSomething(ctx context.Context) error {
    if m == nil {
        return errors.New("MyType is nil")
    }
    
    // Real logic here
    
    return nil
}

// privateHelper is an unexported helper.
func (m *MyType) privateHelper() string {
    return fmt.Sprintf("helper: %s", m.field1)
}
```

And for tests (`mypackage_test.go`):

```go
package mypackage

import (
    "context"
    "testing"
)

func TestNewMyType(t *testing.T) {
    m := NewMyType("test")
    if m == nil {
        t.Fatal("expected non-nil MyType")
    }
}

func TestMyType_DoSomething(t *testing.T) {
    tests := []struct {
        name    string
        field1  string
        wantErr bool
    }{
        {"success", "value", false},
        {"error", "", true},
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            m := NewMyType(tt.field1)
            err := m.DoSomething(context.Background())
            if (err != nil) != tt.wantErr {
                t.Errorf("DoSomething() error = %v, wantErr %v", err, tt.wantErr)
            }
        })
    }
}
```

---

## When in Doubt

1. **Project structure**: See [.agents/ARCHITECTURE.md](.agents/ARCHITECTURE.md)
2. **Naming**: See [.agents/CODE_STYLE.md](.agents/CODE_STYLE.md)
3. **How to organize code**: See [.agents/ARCHITECTURE.md](.agents/ARCHITECTURE.md) + [.agents/DESIGN_PATTERNS.md](.agents/DESIGN_PATTERNS.md)
4. **Error handling**: See [.agents/DESIGN_PATTERNS.md](.agents/DESIGN_PATTERNS.md)
5. **Testing**: See [.agents/TESTING.md](.agents/TESTING.md)
6. **General best practices**: See [.agents/DESIGN_PATTERNS.md](.agents/DESIGN_PATTERNS.md)

---

*This file follows the AGENTS.md standard for LLM-agnostic coding agent guidance. See https://github.com/agentsmd/agents.md for more information.*
