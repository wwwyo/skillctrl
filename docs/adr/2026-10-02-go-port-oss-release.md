# Port to Go and publish as MIT-licensed OSS

- Status: Accepted
- Date: 2026-10-02

Port the existing dotfiles-internal implementation to Go while keeping its
requirements unchanged, and publish it as open source:

- License: MIT.
- Language default: English for code comments, help, public docs, PRs, and bot
  reviews; Japanese translations live under `docs/ja/`.
- Install paths: mise, a custom Homebrew tap, and `go install`.
- CLI: built on Cobra for the command tree, flags, help, and argument
  validation (explicit user request).

The full requirement set these choices implement is preserved in
[docs/requirements.md](../requirements.md).
