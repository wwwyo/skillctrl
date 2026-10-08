# Limit lock and check coverage to upstream-registered skills

- Status: Accepted
- Date: 2026-10-03

Limited the lock, status reporting, and automated checks to skills that have
an upstream registration (and saved intent). Accepting deliberately edited
self-made skills a second time would double-manage them, so ineligible
records are pruned on the next lock write. Merged
[PR #8](https://github.com/wwwyo/skillctrl/pull/8) as `ae2f332` (released in
v0.2.1); macOS and Ubuntu CI, CodeQL, Pullfrog, and CodeRabbit all passed.
