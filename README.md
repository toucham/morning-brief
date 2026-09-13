# Morning Brief

A Go CLI that summarizes morning news and stock moves via an LLM briefing.

> See [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) for system architecture and [docs/PRODUCT.md](docs/PRODUCT.md) for feature specifications.

## Documents

| Document | Purpose |
|---|---|
| [docs/PRODUCT.md](docs/PRODUCT.md) | **Canonical product specification** — features, behavior, API |
| [AGENTS.md](AGENTS.md) | Agent development guidelines (follow when implementing) |

**Precedence**: `docs/PRODUCT.md` wins for product behavior. The Obsidian note `Projects/Morning Brief CLI.md` is superseded by `docs/PRODUCT.md`.

## Development

- Go 1.26.2, module `github.com/toucham/morning-brief`
- Supported platforms: **macOS and Linux** (Windows is out of scope)
- Required checks before considering work done: `gofmt -l .`, `go vet ./...`, `go test ./...`, `go test -race ./...`, `go build ./...`
