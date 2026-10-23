# Recover three-way bases from upstream tree history

Status: accepted. Supersedes [2026-10-15: Three-way update](2026-10-15-three-way-update.md)
for base storage and resolution. File reconciliation and merged-reference behavior
remain unchanged.

## Context

Native skills and gh registrations record an original skill tree hash but no
source commit. A missing local base previously forced replacement even when the
registered tree was reachable in upstream history. Replacement deletes local-only
files, including tests. Keeping a second complete original in the consumer
repository also duplicates content and enlarges checkouts and review diffs.

The earlier ADR considered fetching a recorded commit, but did not consider
looking up the recorded tree directly after fetching upstream history.

## Decision

Resolve the previous original only for a changed single-input update, in order:

1. Read the registered `skillFolderHash` from the consumer's Git objects.
2. Fetch upstream history into disposable bare storage with `--filter=blob:none`,
   then read that tree identifier directly. This works with skills, gh, and git
   acquisition adapters and does not require a recorded commit or skill path.
3. Retain the direct Git adapter's recorded source commit fetch as a last resort,
   including its commit/path identity check.

Verify recovered files against the registered tree hash before reconciliation.
Use the existing source validator: `sourceUrl`, when present, must identify the
same GitHub repository as `source`; otherwise derive the URL from `source`.
Do not broaden supported protocols, execute upstream scripts, or change the
chosen acquisition adapter. A missing tree/hash retains the reasoned `replaced`
fallback, which can delete
local-only files and requires review before acceptance. Fetch failures, invalid
source URLs, unreadable trees, or verification errors abort preparation if the
recorded-commit fallback cannot recover a verified base. A transient failure must
not cause destructive replacement.

Fetch all advertised branch and tag histories without a depth limit. A default
branch fetch can miss a tree retained on another branch or release tag; shallow
fetches can miss arbitrarily old imports. Private pull-request refs and unreachable
objects are outside the search. Fetch commits and trees first; a promisor remote
loads blobs only for the requested tree. Missing blob IDs are prefetched in one
batch per tree to avoid a separate network round trip for each file. Servers that ignore filtering can send
all blobs; correctness is preserved but transfer savings are not guaranteed.
Share one history fetch per validated source within preparation, and discard it
afterward. Do not add persistent caches, private refs, or history to consumer
repositories. The fresh bare repository has no inherited shallow boundary;
existing shallow acquisition clones are not reused for this step.

Do not create, refresh, read, or automatically delete
`.agents/skillctrl/originals/`. Existing copies are ignored and left byte-for-byte
untouched, even by remove or merge. Keep the legacy discovery exclusion so stale
manifests cannot become skills. After upgrading all callers, users can inspect
and manually remove the directory (`git rm -r -- .agents/skillctrl/originals`
for tracked copies). Automatic deletion would discard pending edits and would
make a routine update perform an unrelated repository cleanup. Removing files
from the current tree does not shrink existing Git history.

Merged routing skills keep their registered originals under `references/`,
including the existing numeric-layout compatibility. They are part of the skill's
content, not single-input merge bases.

JSON keeps the existing statuses and adds `base: upstream-history`; `repository`
and `upstream` continue to identify local objects and recorded-commit recovery.
The obsolete snapshot `original` path is omitted. `changes` reports upstream file
changes; conflict reports retain paths and line ranges. To resolve binary or
deletion conflicts, inspect the new original at the registered upstream source
and skill path rather than a stored local duplicate.

## Tradeoffs and alternatives

- Each changed source whose base is absent locally needs a full history fetch on
  each update invocation. This costs network time and commit/tree bandwidth,
  but removes duplicated originals and persistent cache lifecycle concerns.
  Unchanged originals do not trigger base fetches. Explicit update already needs
  upstream access; `check`, record, remove, and dry runs stay offline.
- Keeping snapshots would improve offline base availability, but preserves the
  repository growth and double management the user chose to remove. No snapshot
  compatibility fallback is retained.
- Fetching a tree hash directly is smaller but servers need not allow arbitrary
  tree wants. Fetching advertised history provides a portable reachability path.
- A rewritten upstream can remove the only base. The recorded-commit fallback
  may still recover it; otherwise replacement reports why. A network or
  verification error aborts preparation if commit recovery also fails, preserving
  the installed skills rather than replacing them.
- Fetching only one branch or a bounded depth is faster but silently loses valid
  bases. No such optimization is included without a separate decision and tests.

## Verification

CLI tests use local Git fixture repositories through URL rewriting, without
network access. They cover commit-free skills/gh registrations, history recovery,
local-object priority, unavailable trees and rewritten history, ignored legacy
copies, local-only files, conflicts, and unchanged merged routing behavior.
