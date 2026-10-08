# Edit intent and skill bodies directly; accept only through an explicit record

- Status: Accepted
- Date: 2026-10-05

Users edit intent documents and skill bodies directly; acceptance happens
only through an explicit `record`. Removed the local CLI's AI launch,
dedicated intent operations, and automatic worktree creation — the CLI
resolves the Git root from the working directory and modifies that
repository in place, with `cd` as the only way to choose a target
(auto-created worktrees would have added target-moving and external-tool
dependencies). The everyday `check` only inspects already-acquired content;
fetching upstream is separated into `update` and the scheduler. Implemented
in [PR #9](https://github.com/wwwyo/skillctrl/pull/9), shipped in v0.3.0.
