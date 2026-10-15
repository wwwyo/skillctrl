# Merge upstream changes into adapted skills with a stored base

- Status: Accepted
- Date: 2026-10-15

A scheduled update timed out after a single-input update replaced every
changed skill with its new original: the Git diff mixed upstream changes with
discarded adaptations, so the agent re-read whole skills (over 10,000 changed
lines) and rebuilt each adaptation from intent. `update` now applies only the
upstream difference.

## Decision

Update performs a file-by-file three-way merge: base is the previously
imported original, local is the current skill, upstream is the new original.
Unchanged upstream files are never merged. Text conflicts receive diff3
markers; other conflicts keep the local side. The JSON report lists each
changed file, its result, and conflict line ranges, so automation can give an
agent the upstream diff, the conflicts and the saved intent instead of the
whole skill.

The base is stored as a pristine snapshot in `.agents/skillctrl/originals/<name>/`
and verified against the registered tree hash before use. Without a verified
snapshot, the base comes from the destination's Git objects or, for the direct
Git adapter, the recorded source commit. Failing all of them, update replaces
the skill as before and reports why; the next update merges.

## Rejected alternatives

- **Fetch only the recorded source commit.** The default `skills` adapter and
  `gh` adapter record no commit, so most registrations could never merge.
  Finding a commit through the direct Git backend would silently switch
  adapters. The fetch remains a fallback for Git-adapter registrations.
- **Keep the base inside the skill directory.** It would ship a duplicate
  manifest to agents and change the accepted hash. Merged skills differ: their
  `references/` originals are the content agents read.
- **Store only adapted files, or Git objects under a private ref.** Partial
  snapshots lose the base of files edited later; private refs are not fetched
  by ordinary CI checkouts. Full snapshots cost working-tree space, but Git
  stores unchanged files once because they share blobs with the skill.
- **Fail the command on conflicts.** Conflicts are expected results, like hash
  drift; failing would hide the clean merges prepared in the same run.

## Consequences

The first update after upgrading creates snapshots. A skill whose upstream
already changed and whose base cannot be recovered is replaced once more. Tools
that scan nested manifests may see snapshot manifests under
`.agents/skillctrl/originals/`; skillctrl's own discovery excludes them.
