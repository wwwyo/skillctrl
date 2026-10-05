# Local skill workflows

Detailed command behavior, adapter differences, and lock compatibility. For an introduction and installation, see [README](../README.md).

## Set up a repository

`skillctrl` works on a Git repository that has a skills directory. Create the
two paths it reads:

```
.agents/skills/<name>/                  imported and adapted skills
.agents/skillctrl/intents/<name>.md     what your customization must keep doing
```

The native project lock stays at the repository root: `skills-lock.json` keeps
ordinary registrations in the format used by `npx skills`. Aliases, merged
inputs, and supplemental source provenance live in
`.agents/skillctrl/upstreams.json`; they do not add custom entries or fields to
the native lock. Native commands restore and update ordinary registrations;
use skillctrl for aliases and merges, which native skills does not implement.

`.agents/skillctrl/intents/lock.json` separately records the accepted hash of each
whole skill directory with both an upstream registration and saved intent.
Intent-free imports and handwritten skills remain excluded. Acquisition never
writes accepted hashes; `check` reports local drift and pending cleanup offline.

Existing native locks retain their version, other providers, unknown fields,
and exact bytes when their registrations are unchanged. Supplemental metadata
is bound to its native entry: an independent native edit or removal wins over
old metadata. Legacy version 3 and older skillctrl extensions remain readable;
a successful import moves recognized alias/merge registrations into the private
tracking file. Read-only commands and dry runs never migrate anything. Only
when no root lock exists does a successful import move the legacy native lock
from `.agents/.skill-lock.json` to the root.


## Work with skills

```sh
# search the public index; installs nothing
skillctrl find review --owner owner

# list installed project skills
skillctrl list

# check local accepted hashes offline without changing files
skillctrl check

# import a skill
skillctrl add owner/repo:chosen-skill

# refresh the original without AI or acceptance
skillctrl update chosen-skill

# Save requirements, then edit and verify the skill directly
mkdir -p .agents/skillctrl/intents
cp requirements.md .agents/skillctrl/intents/chosen-skill.md

# what still differs from the accepted hashes
skillctrl check

# compute and save the hash after verifying a deliberate edit
skillctrl record chosen-skill

# remove the skill, its saved intent, and registrations
skillctrl remove chosen-skill
```

`owner/repo` and `chosen-skill` are placeholders; replace them with an inspected
source and skill. An intent describes behavior, for example: "Keep instructions
concise and require tests before completing code changes."

`add`, `update`, and merged inputs use a replaceable acquisition adapter. The
default `skills` adapter invokes the pinned `skills` CLI (the same package used
by `npx skills`); `--adapter gh` invokes `gh skill install`. `--adapter git`
retains the direct Git importer for existing integrations. Set
`SKILLCTRL_ADAPTER` to choose a default; an explicit flag takes precedence.
`--adapter` accepts only `skills`, `gh`, or `git`. Invalid flag values and invalid
effective `SKILLCTRL_ADAPTER` values are rejected before command execution, even
for local-only commands and dry runs. Missing tools are errors, not automatic fallbacks. Install the adapter tools
with mise; the repository pins `skills` 1.7.0 and `gh` 2.101.0 with a seven-day
release cooldown. The skills executable must be on PATH; skillctrl does not
use an unpinned `npx` download.

```sh
skillctrl --adapter skills add owner/repo:chosen-skill
skillctrl --adapter gh add owner/repo:chosen-skill
skillctrl list
skillctrl check chosen-skill
```

| Command | Responsibility |
| --- | --- |
| `find` / `search` | Search skills.sh for `skills` or `git`; use `gh skill search` for `gh`. The skills 1.7.0 find command has no JSON API, so its public index is queried directly. |
| `add` / `install`, `update` | Delegate acquisition to the adapter, then validate, import, and register originals without AI or accepted hashes. Each update fetches an original into empty staging, preserving local adaptations when that original is unchanged. |
| `list` / `ls` | Read project skill directories, including handwritten skills. |
| `check` | Report local accepted-hash drift offline without importing or reviewing; optional names scope the report. No acquisition adapter executable is needed. |
| `remove` / `rm` | Remove project content, its saved intent, and registrations locally; no network or adapter executable is needed. |
| `merge` | skillctrl-specific routing skill containing ordered upstream originals; no intent or AI required. |
| `record` | Compute current skill-directory hashes and create or replace the named lock entries after verifying content yourself. |
| `ci`, `schedule` | Optional automation; `ci plan` selects fixed-commit inputs and `ci prompt` provides reviewer instructions. |

Acquisition runs in a disposable directory and home, so installer-owned global
locks do not replace or relocate the project's root `skills-lock.json`. Only
selected source registrations are imported, preserving unrelated lock metadata.
GitHub CLI's native tracking metadata remains in the downloaded SKILL.md.
GitHub CLI acquisition resolves existing token or Keychain-backed authentication
before changing home, then passes it only to the acquisition process while
removing the inference credential. Native acquisition
tools are trusted executables; downloaded skill scripts are not executed.
Discovery, release/ref selection, and downloaded file modes follow the chosen
backend; switching backends can produce a different original to inspect and customize directly.
Do not interpret the adapters as identical upstream resolvers. The direct Git
backend records source commits; command adapters record paths and content hashes
without inventing a commit identifier. Intent review, import protection, accepted
hashes, and CI validation are shared by all adapters.

Adapter selection is per invocation. Backend discovery, source resolution and
file modes may differ; choose an adapter explicitly when that distinction matters.
The source repository stores its own merged registration in private tracking,
so it does not mark its distributable package as a native installed dependency.
Japanese skill guides display metadata as documentation, without installable
frontmatter, preventing accidental selection of the translation.

CI is optional. Local commands need neither an AI reviewer nor its toolchain.
Use your editor or existing agent to customize content before recording it.

`record NAME` computes the current whole skill-directory hash and creates or
replaces the entry for NAME in `.agents/skillctrl/intents/lock.json`. NAME selects
a skill; you do not supply a hash. Unchanged content produces the same hash. It
does not update native registrations or original-source tracking. It updates
only the accepted lock. It runs no reviewer, changes no skill content, and leaves the
Git index alone. Use it after deliberately editing and checking a managed skill;
it records your acceptance rather than verifying that the saved intent is met.
`record --dry-run` previews the operation without computing or comparing hashes;
use `check [NAME...]` to inspect local drift.
Only named upstream-registered skills with saved intent advance; other eligible hashes remain unchanged. Ineligible entries are pruned. With no names, `record` only prunes ineligible entries and accepts no content, including after every intent file has been deleted.

`ci plan` selects from fixed commits for a CI job without fetching upstreams.
`ci prompt` prints the embedded review instructions after preparation, for the
external job to pass to its reviewer. These helpers live under `ci` and are not
needed for local editing or recording.

`check` reports local drift in `local` without contacting upstreams. It compares
skill-directory hashes with the accepted lock and does not run a model to assess
whether the current instructions satisfy the intent. Differences are reported
in JSON, not treated as command failures. With no names, it checks all skills
with both upstream registration and saved intent; explicit names select a subset.
CI uses fixed commits so another job can recompute the
exact review input. Upstream changes are acquired only by an explicit `update`
or `schedule prepare`; a new upstream version alone does not change local
acceptance or the CI plan.

`ci` separates preparation, agent-output validation, and publication for a
pull-request workflow. `schedule` prepares upstream updates and can publish a
draft update PR after validation. Neither installs a workflow nor starts a
timer: an external CI platform or scheduler must call the phases described in
[CI integration](ci.md).

To update originals deliberately, first inspect local customization, then run:

```sh
skillctrl update chosen-skill
git diff -- .agents/skills/chosen-skill skills-lock.json .agents/skillctrl/upstreams.json
# Edit and verify the skill against its saved intent
skillctrl record chosen-skill
git diff -- .agents/skills/chosen-skill .agents/skillctrl/intents/lock.json
```

Single-input updates replace changed originals; merged updates refresh the
registered references and retain the routing body. The Git diff shows the
imported change, not a separately generated comparison of old and new upstream
originals. Edit the skill directly to preserve its saved intent, verify the
result, then run `record chosen-skill` to accept it. Intent-free skills remain
outside the accepted lock. Inspect the resulting changes before committing them.

Results are JSON on stdout; logs and errors go to stderr. Exit `0` means success
and exit `1` means failure. These local
commands change the selected repository in place and never commit, push, or open
a pull request. The `ci` and `schedule` phases are the deliberate exception: they
exist to commit a validated repair, push it, and report on the pull request, and
they are documented separately in [CI integration](ci.md).


## Merge upstream skills

`add` and `merge` use the same positional `owner/repo:skill` inputs.
`add` imports separate skills; `merge` imports ordered originals into one
routing skill, with each input registered in its `sources` array. Neither
requires intent or a reviewer:

```sh
# Import separate skills
skillctrl add owner/first:first-skill owner/second:second-skill

# Combine the same inputs under one routing skill
skillctrl merge --name combined \
  owner/first:first-skill \
  owner/second:second-skill
skillctrl update combined
```

The repositories above are placeholders. The generated root `SKILL.md` routes
tasks to `references/first-skill/SKILL.md`, `references/second-skill/SKILL.md`, and so
on. Each original retains its bundled resources and relative resource paths.
Snapshots are excluded from skillctrl's independent skill discovery. Check the
target agent's discovery behavior when enabling the package; agents that scan
nested manifests may expose the preserved originals separately. Distinct source skill
names are required (case-insensitively); collisions are rejected before fetching.
Handwritten reference files remain untouched. Legacy numeric snapshots keep
their locations on update; explicitly re-running merge moves registered originals
to named references and regenerates routing. Explicitly running
`merge` again replaces the ordered source list and regenerates routing; `update`
refreshes originals while preserving the current root entrypoint.

To customize integration, write the target intent and edit the routing skill directly:

```sh
mkdir -p .agents/skillctrl/intents
cp integration-requirements.md .agents/skillctrl/intents/combined.md
# Edit and verify .agents/skills/combined/SKILL.md
skillctrl record combined
```

Only `combined.md` guides that review. Existing input skills and their local
intents are neither combined nor deleted. Keep the original snapshots intact during customization. Do not record
unresolved integration. Operations use the selected repository in place. Older skillctrl binaries do not read the private tracking file.

For a single input, `add owner/repo:upstream-name --name local-name` changes the
local directory and registration name, preserving original file bytes and
frontmatter. Subsequent `update local-name` resolves the recorded upstream name;
`check local-name` checks only the local accepted hash. `--name` requires exactly one selected skill; omit it
when importing several separate skills. The acquisition-compatible
`add owner/repo --skill upstream-name` syntax also remains available. Do not
combine its `--skill` with qualified owner/repo:skill inputs. Positional inputs must have distinct skill names, including across repositories.


## What it will and will not do

- **Originals are prepared in isolation.** The selected adapter runs outside
  the caller's repository. Imported files are checked for symlinks, Git control
  files, and destination ignore rules before any result is promoted. The direct
  Git adapter reads tracked blobs without running upstream installers or hooks
  and preserves their executable modes. Command adapters use their native
  discovery and installation behavior; installed skill scripts are not executed
  by skillctrl.
- **An unchanged original is never re-imported.** That is what keeps an
  adaptation alive across an update that did not touch it.
- **The accepted hash covers the whole skill directory** - body, references,
  scripts, and executable mode - and never the intent document. Changing or
  deleting an intent is a decision in itself and never triggers adaptation on its
  own. An ambiguous skill is left untouched with its old hash and reported as
  unresolved. A deleted skill with a surviving intent is reported, not restored.
- **Pending edits are allowed.** `add`, `merge`, `update`, and `remove` preserve
  unrelated working files and the caller's staging area. An import that would
  replace a skill directory containing pending edits is refused before any skill
  is imported; unchanged originals do not overwrite those edits. Main checkouts
  and linked worktrees are both modified in place. skillctrl never creates a
  worktree or changes the working directory. Run commands from the target repository;
  change repositories with `cd` before invoking skillctrl.
- **AI execution belongs to the external workflow.** Local commands do not
  start a reviewer. CI preparation provides trusted configuration and review
  instructions; the adopting workflow runs its agent and supplies the artifacts.
  `skillctrl ci apply` applies a validated patch to the Git index only.
- **Agent output is untrusted.** A separate, credential-free path re-derives the
  plan and refuses an edit outside the selected skills, an incomplete
  accepted/unresolved partition, a changed unresolved skill, or the inference
  credential appearing in a report, result, patch, or staged blob. See
  [CI integration](ci.md) for the exact contract.
