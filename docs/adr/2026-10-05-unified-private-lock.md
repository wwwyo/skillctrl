# Consolidate upstream tracking and accepted hashes in one private lock

- Status: Accepted
- Date: 2026-10-05

Consolidated skillctrl's own upstream tracking and accepted hashes into a
single private lock. The native lock keeps its ownership, and the
operational boundary is preserved — acquisition info is written by
add/update, accepted hashes only by an explicit `record`.
[PR #12](https://github.com/wwwyo/skillctrl/pull/12).
