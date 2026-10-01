# skillctrl

A Go CLI for managing agent skills while preserving locally recorded intent.

## Structure

- `docs/`: shared project requirements and documentation.
- `internal/`: implementation and colocated tests.
- Root Go command: `go install github.com/wwwyo/skillctrl@<version>`.

## Development

Manage tools with mise. Pin tool and dependency versions, respecting a seven-day release cooldown.

```sh
mise install
mise exec -- go test ./...
mise exec -- go vet ./...
mise exec -- go build .
```

Read `docs/requirements.md` before implementation or release changes. English is the default for CLI messages, help, adaptation prompts, and public documentation. Preserve the original safety invariants and lock compatibility. Do not copy personal skill bodies, intent documents, credentials, or dotfiles-specific configuration into this repository.

Code explains how; tests specify observable behavior; commit messages explain why. Use GoDoc for documentation comments and explain non-obvious rejected alternatives in ordinary comments.
