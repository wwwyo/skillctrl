# Re-assess saved intent on each update instead of caching decisions

- Status: Accepted
- Date: 2026-10-03

Kept the existing approach for applying saved intent to upstream updates:
detect drift against the accepted lock, then let the agent reconcile the
diff, the current intent, and the relevant skill bodies at that moment.
Withdrew the proposal to add a decision cache, deterministic replay, or a
dedicated judgment script — reproducing the same text matters less than
making the right call on each update, and the cost of re-judging was not a
real problem. No implementation change.
