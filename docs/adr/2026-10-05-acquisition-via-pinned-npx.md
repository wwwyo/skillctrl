# Acquire through a compatible skills CLI or pinned npx, never a required install

- Status: Accepted
- Date: 2026-10-05

Stopped requiring users to install the dependency CLI separately for the
default backend: use an existing compatible `skills` CLI when present,
otherwise invoke a pinned version through `npx`. Rejected bundling a runtime
or building a custom installer — both make distribution and maintenance
heavier. Also moved implementation-level version/cache details out of the
runtime skill and personal pin/cooldown rules out of the README.
[PR #10](https://github.com/wwwyo/skillctrl/pull/10).
