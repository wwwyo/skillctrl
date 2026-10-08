# Resolve the latest published version at install time instead of pinning it in the README

- Status: Accepted
- Date: 2026-10-03

Changed the README so install examples resolve the latest published version
instead of naming the current release
([#7](https://github.com/wwwyo/skillctrl/pull/7)). A pinned version in the
text drifts stale on every release and forces a doc edit each time; the
version a user actually runs is pinned in their own tool configuration.
Shipped v0.2.0 and completed switching dotfiles' CLI and management skill
over to it.
