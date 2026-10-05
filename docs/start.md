# Set up skillctrl in the current repository

This is a complete setup procedure for an AI agent. Follow it when the user asks
you to install the skillctrl CLI and its skill. The README is the manual setup
reference; you do not need it to complete this procedure.

## Choose the repository and preserve its setup

Read the current repository's agent instructions and inspect its tool-manager
configuration. Work in the repository the user selected. Use `cd` to enter it
before running management commands; skillctrl resolves the enclosing Git root
and has no `--repo` or global skill-installation option.

Check that Git is available and identify the checkout with
`git rev-parse --show-toplevel`. Run setup examples from that root:

```sh
cd "$(git rev-parse --show-toplevel)"
```

Keep unrelated edits, staging, tool pins, and existing skill-manager registrations. Do not create another worktree or switch
to a personal dotfiles repository. If the user has not selected a repository and
several write targets are plausible, clarify the target before installing.

## Install or reuse the CLI and acquisition tools

Check `skillctrl --version` and `skillctrl --help`, using the repository's tool
manager if necessary. Before any import, require the current in-place,
no-reviewer command surface: root help must include `list`, `check`, `record`,
and `--adapter skills|gh|git`, and must not expose `--repo`, `--worktree-provider`,
or the local `intent` command. A binary that accepts `add` syntax but has the old
worktree or reviewer behavior is incompatible with this guide.

Reuse an installation only after this compatibility check. Preserve compatible
tool pins. If the CLI is missing or incompatible, prefer the repository's
existing installation method and select a compatible published release. These
are the supported distribution paths:

```sh
# mise: install locally and save an exact version, respecting a seven-day cooldown
mise use --path ./mise.toml --pin --minimum-release-age 7d github:wwwyo/skillctrl@latest
mise exec -- skillctrl --version

# Go: use when Go is the selected installation method
go install github.com/wwwyo/skillctrl@latest
"$(go env GOPATH)/bin/skillctrl" --version

# Homebrew: use when Homebrew is the selected installation method
brew tap wwwyo/tap
brew install wwwyo/tap/skillctrl
skillctrl --version
```

Choose one method. The mise examples explicitly select a configuration inside
this repository; use its existing repo-local configuration file instead of
`./mise.toml` if repository instructions select another file. Never let setup
write an inherited ancestor configuration.

`@latest` resolves a published version; follow repository pinning and release-age
rules. Do not replace an existing global installation or
change its version merely to complete repository-local setup. Use the resolved
executable or the tool manager's execution wrapper for the remaining commands.
Recheck the required command surface after installation. If no compatible
published release meets the repository's cooldown, report that setup cannot
complete yet; do not invoke an older `add` or bypass the cooldown. Make sure the
executable is reachable by this agent; a successful installation with an
inaccessible executable is incomplete setup.

Inspect `skillctrl add --help`. The default acquisition adapter is `skills`,
which requires Node.js and a pinned `skills` executable on PATH. Install missing
dependencies through the repository's tool manager. For mise, only when these
tools are needed and no compatible pins already exist:

```sh
mise use --path ./mise.toml --pin --minimum-release-age 7d node@lts npm:skills@latest
mise exec -- skills --version
```

Keep an explicitly selected `gh` or `git` adapter if the user or repository
already chose it. `gh` needs GitHub CLI; `git` uses Git directly. Do not silently
switch adapters when a tool is missing, or use an unpinned npx download. If the
installed CLI lacks a feature needed here, resolve a published compatible
version through the chosen manager rather than guessing unsupported flags.

## Install the repository-local skill

Run from the selected repository. Create its skills directory if it is missing,
then import only this package, using the CLI invocation resolved above:

```sh
mkdir -p .agents/skills
skillctrl add wwwyo/skillctrl --skill skillctrl
```

For a mise-managed CLI and adapter, the import invocation is:

```sh
mise exec -- skillctrl add wwwyo/skillctrl --skill skillctrl
```

The source package is `.agents/skills/skillctrl/` in `wwwyo/skillctrl`. The installed
package must be `.agents/skills/skillctrl/` in the target repository, including
`SKILL.md`, references, and other bundled resources. Inspect any existing package
before replacing it; preserve local customization if the import reports pending
edits. Do not reset edits just to make setup succeed.

The managed paths are:

```text
.agents/skills/<name>/                  skill body and bundled resources
.agents/skillctrl/intents/<name>.md     optional local customization requirements
skills-lock.json                      upstream registration at repository root
.agents/skillctrl/intents/lock.json     recorded hashes for upstream + intent skills
```

Keep the project lock at root `skills-lock.json`. Existing version-1 locks keep
other-provider registrations and unknown fields. The legacy
`.agents/.skill-lock.json` is used only when the root lock is absent and migrates
after a successful import; read-only commands do not move it.

Do not create intent or run `record` for an uncustomized installation. Intent is
optional, and the hash lock only covers skills with both an upstream
registration and saved intent. Setup does not require CI, a scheduler, model
credentials, or a locally launched reviewer.

## Verify and hand over

Read the installed `.agents/skills/skillctrl/SKILL.md` and check its relative
resources. Run `skillctrl list` through the resolved invocation and confirm the
installed `skillctrl` entry. Inspect new files, the repository diff, and root
`skills-lock.json`; the returned `repo` must identify the selected checkout.

Check the current agent's existing skill-discovery configuration. Reuse its
repository skill directory convention. If a relative link is required, follow
the repository's existing layout and preserve existing entries. Distinguish
files installed from a skill confirmed available to the current agent. Report
any required reload or new session only when the agent's configuration requires
it; do not claim that copying files alone enabled the skill.

Report the actual checkout, CLI version and invocation, adapter, installed paths,
verification results, and any remaining activation step. Do not commit, push,
open a PR, or configure automation unless the user or repository workflow
already authorized it. Use the installed skill for subsequent skill-management
requests; ordinary CLI commands operate in place and do not launch an AI agent.
