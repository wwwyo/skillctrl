# Separate PR hash-mismatch review from scheduled upstream updates

- Status: Accepted
- Date: 2026-10-05

Split automation into two agent workflows: PR-time review that audits only
hash mismatches, and scheduled updates that fetch upstream and adjust along
saved intent. Did not add intent-change detection; operating guidance lives
in the distributed skill's reference.
[PR #11](https://github.com/wwwyo/skillctrl/pull/11).
