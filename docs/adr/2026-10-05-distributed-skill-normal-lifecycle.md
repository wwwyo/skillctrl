# Manage the distributed skill through the ordinary skill lifecycle

- Status: Accepted
- Date: 2026-10-05

Moved the distributed `skillctrl` skill onto the same lifecycle it manages —
merge, update, check, and record — with no special source-directory flag and
no duplicate package. Shortened the README around features and
representative flows; the split between the AI-facing `docs/start.md` and
the README follows the dotfiles documentation procedure. Part of
[PR #9](https://github.com/wwwyo/skillctrl/pull/9), shipped in v0.3.0.
