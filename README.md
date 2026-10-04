<picture>
  <source media="(prefers-color-scheme: dark)" srcset="assets/wordmark-dark.svg">
  <img src="assets/wordmark.svg" alt="skillctrl" width="312" height="88">
</picture>

# skillctrl

[![Go 1.27](https://img.shields.io/badge/go-1.27-blue.svg)](go.mod) [![MIT License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

[日本語](docs/README.ja.md) · [Requirements](docs/requirements.md) · [CI integration](docs/ci.md) · [Parity](docs/parity.md)

Manage agent skills in a Git repository without losing the intent behind your
local adaptations.

Import and register upstream originals with `add`, `merge`, and `update`.
Save customization requirements separately with `intent set`, then explicitly
apply them with `intent apply` or accept a verified manual edit with `record`.
Acquisition never invokes a model or advances accepted content hashes.

English is the default for output, code comments, documentation, and review reports.
The linked Japanese README provides a translated introduction.

## Install

```sh
# Go
go install github.com/wwwyo/skillctrl@latest

# mise (GitHub release backend)
mise use -g github:wwwyo/skillctrl@latest

# Homebrew
brew tap wwwyo/tap
brew install wwwyo/tap/skillctrl
```

All three paths install the same command and report the same version. A
`go install` build and a release archive are not byte-identical, because the
release archive pins its version through linker flags. `skillctrl --version`
prints the version in either case.

## Set up a repository

`skillctrl` works on a Git repository that has a skills directory. Create the
two paths it reads:

```
.agents/skills/<name>/                  imported and adapted skills
.agents/skillctrl/intents/<name>.md     what your customization must keep doing
```

The project lock stays at the repository root: `skills-lock.json` records
where each skill came from; `.agents/skillctrl/intents/lock.json` records the
accepted hash of each whole skill directory that has both an upstream
registration and saved intent. Intent-free imports and handwritten skills stay
outside this accepted lock. Existing ineligible entries are pruned on the next
accepted-lock write; acquisition does not write that lock. `status` remains
read-only and reports pending cleanup through `lock_changed`.

Existing `npx skills` project locks (version 1) are read in place, preserving their
version, other providers, and unknown fields. New locks use version 1, with extra
Git source metadata for skillctrl. Legacy version 3 records remain readable. If
only `.agents/.skill-lock.json` exists, a successful import migrates it to the
root; `status` and dry runs never move it. An existing root lock takes precedence.
Merged entries use skillctrl's `sources` extension; manage those with skillctrl.

## Work with skills

```sh
# search the public index; installs nothing
skillctrl find review --owner owner

# list installed project skills
skillctrl list

# check upstream updates and local accepted hashes without changing files
skillctrl check

# import a skill
skillctrl add owner/repo --skill chosen-skill

# refresh the original without AI or acceptance
skillctrl update chosen-skill

# save requirements separately, then explicitly apply them with AI
skillctrl intent set chosen-skill --file requirements.md
skillctrl intent apply chosen-skill

# what still differs from the accepted hashes
skillctrl status

# accept a deliberate manual edit
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
skillctrl --adapter skills add owner/repo --skill chosen-skill
skillctrl --adapter gh add owner/repo --skill chosen-skill
skillctrl list
skillctrl check chosen-skill
```

| Command | Responsibility |
| --- | --- |
| `find` / `search` | Search skills.sh for `skills` or `git`; use `gh skill search` for `gh`. The skills 1.7.0 find command has no JSON API, so its public index is queried directly. |
| `add` / `install`, `update` | Delegate acquisition to the adapter, then validate, import, and register originals without AI or accepted hashes. Each update fetches an original into empty staging, preserving local adaptations when that original is unchanged. |
| `list` / `ls` | Read project skill directories, including handwritten skills. |
| `check` | Report upstream updates and local accepted-hash drift without importing or reviewing; optional names scope both reports. |
| `remove` / `rm` | Remove project content, its saved intent, and registrations locally; no network or adapter executable is needed. |
| `merge` | skillctrl-specific routing skill containing ordered upstream originals; no intent or AI required. |
| `intent set/remove/apply`, `status`, `record` | skillctrl-specific intent and acceptance workflow. `status` checks local acceptance offline; `intent apply` explicitly reviews named skills. |
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
backend; switching backends can produce a different original to review with intent apply.
Do not interpret the adapters as identical upstream resolvers. The direct Git
backend records source commits; command adapters record paths and content hashes
without inventing a commit identifier. Intent review, import protection, accepted
hashes, and CI validation are shared by all adapters.

CI is optional: these commands work locally, and intent adaptation needs a
configured reviewer toolchain and model, independently of CI.

`record NAME` hashes the current whole skill directory and updates only the
accepted lock. It runs no reviewer, changes no skill content, and leaves the
Git index alone. Use it after deliberately editing and checking a managed skill;
it records your acceptance rather than verifying that the saved intent is met.
Only named upstream-registered skills with saved intent advance; other eligible hashes remain unchanged. Ineligible entries are pruned.

`ci plan` selects from fixed commits for a CI job without fetching upstreams.
`ci prompt` prints the embedded review instructions after preparation, for the
external job to pass to its reviewer. These helpers live under `ci` and are not
needed for local `intent apply`, which uses the embedded instructions directly.

`check` keeps upstream changes in `updates` and local drift in `local`.
Local drift compares skill-directory hashes with the accepted lock; it does not
run a model to assess whether the current instructions satisfy the intent.
Use `status` for the same local check without network access. CI uses fixed
commits rather than current working files or newly fetched originals so another
job can recompute the exact review input.

`ci` separates preparation, agent-output validation, and publication for a
pull-request workflow. `schedule` prepares upstream updates and can publish a
draft update PR after validation. Neither installs a workflow nor starts a
timer: an external CI platform or scheduler must call the phases described in
[docs/ci.md](docs/ci.md).

Results are JSON on stdout; logs and errors go to stderr. Exit `2` means
adaptation finished with unresolved skills, exit `1` means failure. These local
commands leave the work in the working directory and never commit, push, or open
a pull request. The `ci` and `schedule` phases are the deliberate exception: they
exist to commit a validated repair, push it, and report on the pull request, and
they are documented separately in [docs/ci.md](docs/ci.md).

## Agent skill

The distributable [skillctrl skill](skills/skillctrl/SKILL.md) guides agents through
discovery, named imports, customization, updates, and removal. It includes an
on-demand authoring guide for creating and improving skills, without requiring
separate `find-skills` or `skill-creator` installations.

Once this package is published in the repository, import it into your chosen
skills repository:

```sh
skillctrl --repo /absolute/path/to/project add wwwyo/skillctrl --skill skillctrl
```

The CLI must already be installed and the target must contain `.agents/skills/`.
Inspect the working copy returned in the command's JSON output before integrating
the result. The source package lives in `skills/`; installed skills live in the
target's `.agents/skills/`. The skill and CLI are separate artifacts.

## Merge upstream skills

`add` with several `--skill` values imports separate skills. `merge` imports
ordered originals into one routing skill, with each input registered in its
`sources` array. It requires neither intent nor a reviewer:

```sh
skillctrl merge --name combined \
  --from owner/first:first-skill \
  --from owner/second:second-skill
skillctrl update combined
```

The repositories above are placeholders. The generated root `SKILL.md` routes
tasks to `references/first-skill/SKILL.md`, `references/second-skill/SKILL.md`, and so
on. Each original retains its bundled resources and relative resource paths.
Snapshots are excluded from independent skill discovery. Distinct source skill
names are required (case-insensitively); collisions are rejected before fetching.
Handwritten reference files remain untouched. Legacy numeric snapshots keep
their locations on update; explicitly re-running merge moves registered originals
to named references and regenerates routing. Explicitly running
`merge` again replaces the ordered source list and regenerates routing; `update`
refreshes originals while preserving the current root entrypoint.

To customize integration, save a separate target intent and apply it explicitly:

```sh
skillctrl intent set combined --file integration-requirements.md
skillctrl intent apply combined
```

Only `combined.md` guides that review. Existing input skills and their local
intents are neither combined nor deleted. Review preserves the source snapshots;
unresolved integration keeps the old accepted hash. Use the returned `repo`
path for subsequent operations. Older binaries cannot manage multi-source entries.

For a single input, `add --name local-name --skill upstream-name` changes the
local directory and registration name, preserving original file bytes and
frontmatter. Subsequent `update local-name` and `check local-name` resolve the
recorded upstream name. `--name` requires exactly one selected skill; omit it
when importing several separate skills.

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
  is imported; unchanged originals do not overwrite those edits. If the target is
  a linked worktree, it is modified in place. For a main checkout, a separate
  worktree starts from its current commit and carries tracked changes and
  non-ignored untracked files; the original checkout remains untouched. Use the
  returned `repo` path to inspect the result. `--worktree-provider orca` delegates
  worktree creation to Orca.
- **Isolation flags do not hide files.** Intent review runs the agent with no
  session, no context files, no skills, no extensions, no prompt templates, and
  no auto-approval. Those stop discovery and unattended action; they do not stop
  the agent from reading files in the checkout it was told to review. The agent's
  toolchain is resolved in the directory prepared for the run - never in your
  shell's directory and never in the repository under review - and the resolved
  values take precedence over inherited ones.
- **Local repair edits the working tree; CI repair does not.** `skillctrl
  intent apply NAME` lets the reviewer edit the working directory and leaves the result
  there for you to inspect. `skillctrl ci apply` applies a validated patch to the
  Git index only.
- **Agent output is untrusted.** A separate, credential-free path re-derives the
  plan and refuses an edit outside the selected skills, an incomplete
  accepted/unresolved partition, a changed unresolved skill, or the inference
  credential appearing in a report, result, patch, or staged blob. See
  [docs/ci.md](docs/ci.md) for the exact contract.

## Development

```sh
mise install
mise exec -- go test ./...
mise exec -- go vet ./...
mise exec -- go build .
```

With mise active in this repository, `skillctrl` resolves to `tools/dev/skillctrl`.
The launcher builds the current checkout before every invocation and forwards
arguments, the caller's working directory, and the exit status to that binary.
Go's build cache keeps unchanged builds fast. The local binary is ignored by
Git; the launcher does not replace a globally installed version. Without shell
activation, run `mise exec -- skillctrl --help`.

Read [docs/requirements.md](docs/requirements.md) before changing behavior,
[docs/ci.md](docs/ci.md) before wiring it into CI, and
[docs/releasing.md](docs/releasing.md) before cutting a release.
[docs/parity.md](docs/parity.md) maps every behavior to the test that pins it
and names what is not covered.

Licensed under the [MIT license](LICENSE).
