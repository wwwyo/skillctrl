# skillctrl

A Go CLI for managing agent skills while preserving locally recorded intent.

## Structure

- `docs/`: shared project requirements and documentation.
- `internal/`: implementation and colocated tests.
- `tools/release/`: the release archive and tap formula builder.
- Root Go command: `go install github.com/wwwyo/skillctrl@<version>`.

## Development

The command tree is built with `github.com/spf13/cobra`: use it for commands, flags, help, and argument validation rather than parsing arguments by hand. Manage tools with mise. Pin tool and dependency versions, respecting a seven-day release cooldown.

```sh
mise install
mise exec -- go test ./...
mise exec -- go vet ./...
mise exec -- go build .
```

Read `docs/requirements.md` before implementation or release changes. English is the default for CLI messages, help, adaptation prompts, and public documentation. Preserve the original safety invariants and lock compatibility. Do not copy personal skill bodies, intent documents, credentials, or dotfiles-specific configuration into this repository.

Code explains how; tests specify observable behavior; commit messages explain why. Use GoDoc for documentation comments and explain non-obvious rejected alternatives in ordinary comments.

## Agent integrations

Pullfrog's repository configuration is recorded in `.github/pullfrog.config.sh`. The workflow is managed by Pullfrog; keep it unchanged. Initial and subsequent commit reviews are enabled, with manual requests through `@pullfrog`. Organization instructions are inherited.

Langfuse session tracing is a personal opt-in in ignored local files: `.claude/settings.local.json`, `.codex/langfuse.json`, `.pi/settings.json` (with `.pi/npm/`), and `mise.local.toml` for Devin. Never commit tracing credentials or personal opt-in files. The optional Orca setup script copies these files from the main checkout when the user's dotfiles helper is available; set `SKILLCTRL_WORKTREE_SETUP_HELPER` to use another helper. Start Codex at the repository root so it finds the local tracing configuration.

## Glossary

**Upstream input (`Input`)**: A named skill in a GitHub repository whose original content contributes to a managed skill.

**Merged skill**: One managed skill derived from an ordered collection of upstream inputs according to its saved intent.

**Saved intent**: The user's requirements for the behavior of a skill, including the integration policy of a merged skill.
