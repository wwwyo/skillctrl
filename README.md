<picture>
  <source media="(prefers-color-scheme: dark)" srcset="assets/wordmark-dark.svg">
  <img src="assets/wordmark.svg" alt="skillctrl" width="312" height="88">
</picture>

# skillctrl

[![Go 1.27](https://img.shields.io/badge/go-1.27-blue.svg)](go.mod) [![MIT License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

[日本語](docs/README.ja.md) · [Requirements](docs/requirements.md) · [CI integration](docs/ci.md) · [Parity](docs/parity.md)

Manage agent skills in a Git repository without losing the intent behind your
local adaptations.

A skill is installed from somewhere else, then edited to fit your machine. The
next update overwrites those edits, and nothing records what they were for.
`skillctrl` keeps the reason next to the skill: when an upstream release changes
a skill you have edited, the change is re-applied to your version instead of
replacing it, and the hashes of what you have actually accepted are recorded in
Git.

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
accepted hash of each whole upstream-managed skill directory. Only skills
registered in the upstream lock are included in accepted hashes, `status`, and
automatic intent review. Handwritten skills are excluded even if they have an
intent document; their intentional edits need no second acceptance record.
Existing handwritten hash entries are removed the next time an installer,
`record`, or CI phase writes the accepted lock. `status` remains read-only and
reports pending cleanup through `lock_changed` without selecting those skills.

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

# check upstream updates without changing project files
skillctrl check

# import a skill
skillctrl add owner/repo --skill chosen-skill

# ... then write .agents/skillctrl/intents/chosen-skill.md

# refresh it; the skill body is repaired to satisfy the saved intent
skillctrl update chosen-skill

# what still differs from the accepted hashes
skillctrl status

# accept a deliberate manual edit
skillctrl record chosen-skill

# drop the upstream registration; the intent file stays
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
Missing tools are errors, not automatic fallbacks. Install the adapter tools
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
| `add` / `install`, `update` | Delegate acquisition to the adapter, then validate, import, and reapply saved intent. Each update fetches an original into empty staging, preserving local adaptations when that original is unchanged. |
| `list` / `ls` | Read project skill directories, including handwritten skills. |
| `check` | Fetch and compare upstream originals without importing or reviewing. |
| `remove` / `rm` | Remove project content and registration locally; no network or adapter executable is needed. |
| `status`, `record`, `merge` | skillctrl's accepted hashes and saved-intent workflow. `status` checks local acceptance, while `check` checks upstream updates. |
| `plan`, `prompt`, `schema`, `ci`, `schedule` | Inspection and optional automation. |

Acquisition runs in a disposable directory and home, so installer-owned global
locks do not replace or relocate the project's root `skills-lock.json`. Only
selected source registrations are imported, preserving unrelated lock metadata.
GitHub CLI's native tracking metadata remains in the downloaded SKILL.md.
GitHub CLI acquisition resolves existing token or Keychain-backed authentication
before changing home, then passes it only to the acquisition process while
removing the inference credential. Native acquisition
tools are trusted executables; downloaded skill scripts are not executed.
Discovery, release/ref selection, and downloaded file modes follow the chosen
backend; switching backends can produce a different original and trigger review.
Do not interpret the adapters as identical upstream resolvers. The direct Git
backend records source commits; command adapters record paths and content hashes
without inventing a commit identifier. Intent review, import protection, accepted
hashes, and CI validation are shared by all adapters.

CI is optional: these commands work locally, and intent adaptation needs a
configured reviewer toolchain and model, independently of CI.

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

One managed skill can track multiple upstream inputs in a `sources` array.
First save the integration policy in
`.agents/skillctrl/intents/combined.md`. Then select the inputs explicitly:

```sh
skillctrl merge combined \
  --from owner/discovery:find-skills \
  --from owner/authoring:skill-creator

# Checks every registered original and adapts the merged skill when needed
skillctrl update combined
```

The repositories above are placeholders.
The full input list replaces the target's previous registrations. Each input
records its own source, skill name, path, and original tree hash (and a commit when available). Existing
single-source registrations retain their format.

Originals live in `.agents/skills/combined/.skillctrl-sources/<index>/` so resource
names cannot collide and review can use the complete inputs. The reviewer keeps
those originals intact and integrates their behavior into the current entrypoint
according to the intent. Conflicts remain unresolved with the old accepted hash.
Changing an intent alone does not re-run integration; deliberately edit and verify
the merged skill when applying a new policy to unchanged originals.

Use the `repo` returned by `merge` or `update` for subsequent inspection. The
result is one repository-local skill, and can be distributed as that directory.
Its snapshot manifests are excluded from skillctrl's discovery. Older binaries
cannot manage multi-source lock entries. The optional reviewer toolchain and
models must be configured as described in [CI integration](docs/ci.md).

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
  update` lets the reviewer edit the working directory and leaves the result
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

Read [docs/requirements.md](docs/requirements.md) before changing behavior,
[docs/ci.md](docs/ci.md) before wiring it into CI, and
[docs/releasing.md](docs/releasing.md) before cutting a release.
[docs/parity.md](docs/parity.md) maps every behavior to the test that pins it
and names what is not covered.

Licensed under the [MIT license](LICENSE).
