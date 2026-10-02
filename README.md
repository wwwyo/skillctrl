![skillctrl](docs/wordmark.svg)

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
go install github.com/wwwyo/skillctrl@v0.1.0

# mise (GitHub release backend)
mise use -g github:wwwyo/skillctrl@v0.1.0

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

The two lock files are created by the tool. `.agents/.skill-lock.json` records
where each skill came from; `.agents/skillctrl/intents/lock.json` records the
accepted hash of each whole skill directory.

## Work with skills

```sh
# search the public index; installs nothing
skillctrl find browser --owner vercel-labs

# import a skill
skillctrl add vercel-labs/agent-browser --skill agent-browser

# ... then write .agents/skillctrl/intents/agent-browser.md

# refresh it; the skill body is repaired to satisfy the saved intent
skillctrl update agent-browser

# what still differs from the accepted hashes
skillctrl status

# accept a deliberate manual edit
skillctrl record agent-browser

# drop the upstream registration; the intent file stays
skillctrl remove agent-browser
```

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
`.agents/skillctrl/intents/combined.md` and prepare a clean checkout through your
repository's normal workflow. Then select the inputs explicitly:

```sh
skillctrl merge combined \
  --from owner/discovery:find-skills \
  --from owner/authoring:skill-creator

# Checks every registered original and adapts the merged skill when needed
skillctrl update combined
```

The repositories above are placeholders. `merge` requires a build containing
this feature; until a release includes it, build from this source checkout.
The full input list replaces the target's previous registrations. Each input
records its own source, skill name, commit, path, and original tree hash. Existing
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

- **Originals are read from Git, not installed.** Tracked files come straight
  out of a shallow clone. No upstream installer, script, or hook runs. Binary
  content and executable bits are preserved; an import containing a symlink, a
  submodule, a `.gitignore` or `.gitattributes`, or a file your repository would
  ignore is refused, and a name that does not resolve uniquely is not guessed.
- **An unchanged original is never re-imported.** That is what keeps an
  adaptation alive across an update that did not touch it.
- **The accepted hash covers the whole skill directory** - body, references,
  scripts, and executable mode - and never the intent document. Changing or
  deleting an intent is a decision in itself and never triggers adaptation on its
  own. An ambiguous skill is left untouched with its old hash and reported as
  unresolved. A deleted skill with a surviving intent is reported, not restored.
- **The working directory you point at is not always left alone.** `add`,
  `update`, and `remove` refuse a dirty checkout. If the target is already a
  linked worktree, that worktree is the working copy and is modified. If it is a
  main checkout, a detached worktree is created in a temporary directory and the
  main checkout is left untouched. `--worktree-provider orca` delegates worktree
  creation to Orca.
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
