# Agent Guidelines for Morning Brief Development

This file contains development guidelines for implementing the morning-brief project. All LLM coding agents (Claude, Copilot, Cursor, etc.) should read and apply these guidelines to ensure consistent, idiomatic, and maintainable Go code.

This is the LLM-agnostic `AGENTS.md` standard — the agent-facing counterpart to `README.md` (which is for humans). Detailed guidance lives in `.agents/` subdirectory.

## Quick Reference

| Document | Purpose |
|----------|---------|
| **[IMPLEMENTATION.md](IMPLEMENTATION.md)** | Phase 1 detailed design: directory layout, config structs, spawn/lockfile logic, CLI resolution flow, verification plan |
| **[.agents/ARCHITECTURE.md](.agents/ARCHITECTURE.md)** | Project structure, when to create packages/structs/interfaces, domain logic placement, package boundaries |
| **[.agents/CODE_STYLE.md](.agents/CODE_STYLE.md)** | Naming conventions, documentation, comments, code organization, nil checks |
| **[.agents/DESIGN_PATTERNS.md](.agents/DESIGN_PATTERNS.md)** | Dependency injection, error handling, interfaces, composition, concurrency, testing patterns |
| **[.agents/TESTING.md](.agents/TESTING.md)** | Test organization, table-driven tests, mocks, error testing, concurrency, coverage |

---

## How to Use These Guides

### Before Writing Code
1. Read **IMPLEMENTATION.md** to understand Phase 1 scope and design
2. Read **[.agents/ARCHITECTURE.md](.agents/ARCHITECTURE.md)** to understand package structure and where your code belongs
3. Check **[.agents/DESIGN_PATTERNS.md](.agents/DESIGN_PATTERNS.md)** for the appropriate patterns

### While Writing Code
1. Apply naming conventions from **[.agents/CODE_STYLE.md](.agents/CODE_STYLE.md)**
2. Refer to **[.agents/CODE_STYLE.md](.agents/CODE_STYLE.md)** for documentation and comments
3. Use patterns from **[.agents/DESIGN_PATTERNS.md](.agents/DESIGN_PATTERNS.md)** for interfaces and error handling

### When Writing Tests
1. Follow **[.agents/TESTING.md](.agents/TESTING.md)** for test organization and patterns

### After Writing Code
Use this **Code Review Checklist** before submitting:

- [ ] Package organization matches `cmd/{cli,server}` + `internal/{api,client,config,server}`
- [ ] All exports have doc comments (see [.agents/CODE_STYLE.md](.agents/CODE_STYLE.md))
- [ ] Errors are wrapped with context using `fmt.Errorf("%w", ...)`
- [ ] Nil checks use guard clauses (early returns)
- [ ] Interfaces are small (1-3 methods) and defined in consumer packages
- [ ] No global state; dependencies injected via constructors
- [ ] `context.Context` is first parameter for functions that need cancellation
- [ ] Naming follows conventions: lowercase packages, CamelCase exports, consistent receivers
- [ ] Tests use table-driven pattern where applicable
- [ ] Deferred cleanup (`defer`) for files, locks, goroutines
- [ ] No panic for expected errors; return errors instead
- [ ] Type assertions use comma-ok idiom (safe, not panicking)

---

## Key Principles Summary

### Architecture
- **Thin cmd/, thick internal/**: Entrypoints only in `cmd/`; all logic in `internal/`
- **One responsibility per package**: `client`, `server`, `config`, `api`
- **Define interfaces where consumed**: Consumer package owns interface definitions

### Design
- **Dependency injection**: Pass dependencies as constructor params, use interfaces
- **Error wrapping**: Add context using `fmt.Errorf("%w", ...)` as errors bubble up
- **Small interfaces**: 1-3 methods; allows composition and easy mocking
- **Defer for cleanup**: Guarantees cleanup on early return

### Code Style
- **Naming**: lowercase packages, CamelCase exports, consistent receivers (c, s, etc.)
- **Guard clauses**: Early returns reduce nesting
- **Comments**: Explain why, not what; document all exports
- **No global state**: Explicit dependencies

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
