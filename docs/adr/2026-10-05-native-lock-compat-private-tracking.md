# Keep native lock compatibility; isolate skillctrl-specific tracking

- Status: Accepted
- Date: 2026-10-05

Kept the native `skills-lock.json` format and location unchanged — a
skillctrl-only format would break the existing skills CLI — and isolated
skillctrl's own merge/alias tracking separately. CI and scheduled runs are
built from the same basic commands; AI invocation, timers, and PR publishing
live in outer workflows, not in the CLI. Part of
[PR #9](https://github.com/wwwyo/skillctrl/pull/9), shipped in v0.3.0.
