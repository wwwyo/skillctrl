![skillctrl](docs/wordmark.svg)

# skillctrl

[![MIT License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

[日本語](docs/README.ja.md) · [Requirements](docs/requirements.md) · [CI integration](docs/ci.md) · [Parity](docs/parity.md)

Manage agent skills in a Git repository without losing the intent behind your
local adaptations.

A skill is installed from somewhere else, then edited to fit your machine. The
next update overwrites those edits, and nothing records what they were for.
`skillctrl` keeps the reason next to the skill: when an upstream release changes
a skill you have edited, the change is re-applied to your version instead of
replacing it, and the hashes of what you have actually accepted are recorded in
Git.

English is the default for output, documentation, and review reports.

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
adaptation finished with unresolved skills, exit `1` means failure. Either way
the work stays in the working directory, and skillctrl never commits, pushes, or
opens a pull request for you.

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
