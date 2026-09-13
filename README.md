# Morning Brief

A Go CLI that summarizes morning news and stock moves via an LLM briefing.

> **Status: Phase 1 complete, Phases 2–7 planned.** Roadmap slice 1 (client/server scaffold) is in place with the base HTTP endpoints and CLI plumbing. News, LLM, stocks, TUI, and settings (Phases 2–7) are planned — see [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) for system architecture and [docs/PRODUCT.md](docs/PRODUCT.md) for feature specifications.

## Documents

| Document | Purpose |
|---|---|
| [docs/PRODUCT.md](docs/PRODUCT.md) | **Canonical product specification** — features, behavior, API, roadmap |
| [docs/IMPLEMENTATION.md](docs/IMPLEMENTATION.md) | Implementation guide, Phases 1–7 (mechanics, lifecycle, verification) |
| [AGENTS.md](AGENTS.md) | Agent development guidelines (follow when implementing) |

**Precedence**: for product behavior, `docs/PRODUCT.md` wins; for implementation mechanics, `docs/IMPLEMENTATION.md` wins. The Obsidian note `Projects/Morning Brief CLI.md` is superseded by `docs/PRODUCT.md`.

## Development

- Go 1.26.2, module `github.com/toucham/morning-brief`
- Supported platforms: **macOS and Linux** (Windows is out of scope)
- Required checks before considering work done: `gofmt -l .`, `go vet ./...`, `go test ./...`, `go test -race ./...`, `go build ./...`
