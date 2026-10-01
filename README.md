![skillctrl](docs/wordmark.svg)

# skillctrl

[![MIT License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

[日本語](docs/README.ja.md) · [Requirements](docs/requirements.md) · [CI integration](docs/ci.md) · [Parity](docs/parity.md)

Manage agent skills in a Git repository without losing the intent behind your
local adaptations.

A skill is usually installed from somewhere else, then edited to fit your
machine. The next update overwrites those edits, and there is no record of what
they were for. `skillctrl` keeps the reason next to the skill: when an upstream
release changes a skill you have edited, the change is re-applied to your
version instead of replacing it, and the hashes of what you have actually
accepted are recorded in Git.

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

Each path installs the same binary. `skillctrl --version` prints the version.

## Use

```sh
skillctrl find browser --owner vercel-labs   # search the public index, install nothing
skillctrl add vercel-labs/agent-browser --skill agent-browser
skillctrl update                            # refresh every registered skill
skillctrl update agent-browser              # refresh one
skillctrl remove agent-browser
skillctrl status                            # what differs from the accepted hashes
skillctrl record agent-browser              # accept a deliberate manual edit
skillctrl schema                            # machine-readable command description
```

Results are JSON on stdout; logs and errors go to stderr. Exit `2` means
adaptation finished with unresolved skills, exit `1` means failure. The work
stays in the worktree either way, so nothing is lost and nothing is committed,
pushed, or opened as a pull request for you.

Inside a repository, the current checkout is used. `--repo <path>` selects
another one. `add`, `update`, and `remove` require a clean, isolated checkout;
`record` runs in place and never touches your staging area, because accepting a
pending edit is exactly what it is for.

## How it works

### Originals are read from Git, not installed

A skill is imported by reading tracked files straight out of a shallow clone.
No upstream installer, script, or hook ever runs. Binary content and executable
bits are preserved, and an import is refused when it contains a symlink, a
submodule, a `.gitignore` or `.gitattributes`, or a file your repository's own
ignore rules would exclude - each of those could change what "the same skill"
means later.

A skill is selected by the name in its `SKILL.md` frontmatter or by its
directory name. If the name resolves to more than one file, or to none, the
import stops rather than guessing.

When a skill's original has not changed, nothing is imported. That is what keeps
an adaptation alive across an update that did not actually touch it.

### Your intent is the specification

Each adapted skill has an intent document:

```
.agents/skillctrl/intents/<name>.md
```

Describe what the skill must keep doing, not how to implement it. When an update
would break that, the skill body, description, and examples are rewritten to
satisfy the current intent - the actual repair, not a replayed patch.

Editing or deleting an intent document is itself a decision and never triggers
adaptation on its own. A skill whose saved hash already matches is never
reviewed again, even if the diff is large.

Two locks record different things:

| Lock | Holds |
| --- | --- |
| `.agents/.skill-lock.json` (version 3) | where each skill came from: source, path, original tree hash, fetched commit |
| `.agents/skillctrl/intents/lock.json` (version 2) | the accepted Git tree hash of each whole skill directory |

The accepted hash covers the entire directory - body, references, scripts, and
executable mode - and never the intent document. When adaptation is ambiguous,
the skill is left untouched, its old hash is kept, and the run reports it as
unresolved rather than pretending the work is done.

A deleted skill with a surviving intent is reported, never silently restored.
A skill with no intent is recorded as-is; the tool does not invent one.

### Isolation

The installer refuses to run in a dirty checkout and never mutates the one you
point it at:

- a linked worktree is reused as-is;
- otherwise `--worktree-provider git` (the default) creates a detached Git
  worktree in a temporary directory;
- `--worktree-provider orca` delegates to an Orca worktree for callers that
  manage worktrees that way. Set `SKILLCTRL_WORKTREE_PROVIDER` to choose
  without a flag.

Intent review runs in an isolated agent process with no session, no context
files, no skills, no extensions, no prompt templates, and no auto-approval, in a
checkout containing nothing but the selected skill. Its toolchain is resolved
from the trusted configuration prepared for the run, never from the repository
under review.

Skill bodies are patched into the Git index only, never into your working tree,
so the repair stays inspectable.

### Verification

Anything an agent produces is treated as untrusted until a separate,
credential-free path re-checks it:

- the plan is recomputed from the trusted source, never taken from the agent;
- the patch may only touch the selected skills;
- every reviewed skill must appear exactly once as accepted or unresolved;
- an unresolved skill must be byte-identical to its input;
- a removed skill with a surviving intent cannot be accepted;
- the inference credential must not appear in the report, the result, the patch,
  or any staged blob;
- publication is refused if the pull request closed, moved, came from a fork, or
  now targets its own base branch, and a rerun never posts a duplicate comment.

Use the plan and validation phases as reusable commands:

```sh
skillctrl plan --base <commit> [--head <commit>] [--since <commit>]
skillctrl ci prepare|export|apply|publish <directory>
skillctrl schedule prepare|restore|publish <directory>
skillctrl prompt            # the review contract, embedded in this binary
```

[docs/ci.md](docs/ci.md) has a complete workflow, and
[docs/parity.md](docs/parity.md) maps every behavior to the test that pins it.

## Development

```sh
mise install
mise exec -- go test ./...
mise exec -- go vet ./...
mise exec -- go build .
```

Read [docs/requirements.md](docs/requirements.md) before changing behavior, and
[docs/releasing.md](docs/releasing.md) before cutting a release.

Licensed under the [MIT license](LICENSE).
