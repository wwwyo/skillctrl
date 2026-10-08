# Architecture decision records

Dated decision records for skillctrl, migrated from the project wiki's
decision log so the rationale lives and ages with the code. Each file records
one decision or milestone; filenames are `<date>-<slug>.md`.

| Date | Record |
| --- | --- |
| 2026-10-02 | [Port to Go and publish as MIT-licensed OSS](2026-10-02-go-port-oss-release.md) |
| 2026-10-02 | [Ship the initial release after verifying real installs on all three paths](2026-10-02-initial-release-verified.md) |
| 2026-10-02 | [Apply the English default to code comments and public reviews](2026-10-02-english-reviews-and-comments.md) |
| 2026-10-02 | [Adopt multi-source tracking for merged skills and the wordmark](2026-10-02-multi-source-tracking-wordmark.md) |
| 2026-10-03 | [Resolve the latest published version at install time instead of pinning it in the README](2026-10-03-readme-resolves-latest-version.md) |
| 2026-10-03 | [Re-assess saved intent on each update instead of caching decisions](2026-10-03-reassess-intent-per-update.md) |
| 2026-10-03 | [Make the unified skillctrl skill the only entrypoint](2026-10-03-single-skill-entrypoint.md) |
| 2026-10-03 | [Limit lock and check coverage to upstream-registered skills](2026-10-03-checks-scoped-to-registered-skills.md) |
| 2026-10-03 | [Ship v0.2.1](2026-10-03-v0-2-1-release.md) |
| 2026-10-05 | [Edit intent and skill bodies directly; accept only through an explicit record](2026-10-05-direct-edit-explicit-record.md) |
| 2026-10-05 | [Consolidate first-time setup in docs/start.md and Japanese docs under docs/ja/](2026-10-05-start-md-and-docs-ja.md) |
| 2026-10-05 | [Manage the distributed skill through the ordinary skill lifecycle](2026-10-05-distributed-skill-normal-lifecycle.md) |
| 2026-10-05 | [Keep native lock compatibility; isolate skillctrl-specific tracking](2026-10-05-native-lock-compat-private-tracking.md) |
| 2026-10-05 | [Ship the command/record boundary rework as v0.3.0](2026-10-05-v0-3-0-release.md) |
| 2026-10-05 | [Acquire through a compatible skills CLI or pinned npx, never a required install](2026-10-05-acquisition-via-pinned-npx.md) |
| 2026-10-05 | [Separate PR hash-mismatch review from scheduled upstream updates](2026-10-05-separate-pr-check-and-scheduled-update.md) |
| 2026-10-05 | [Consolidate upstream tracking and accepted hashes in one private lock](2026-10-05-unified-private-lock.md) |
| 2026-10-06 | [Ship v0.3.1 and announce it publicly](2026-10-06-v0-3-1-release.md) |
