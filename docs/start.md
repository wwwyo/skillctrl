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

Check `skillctrl --version` and `skillctrl --help`, using the existing tool
manager if necessary. Before any import, require the current in-place,
no-reviewer command surface: root help must include `list`, `check`, `record`,
and `--adapter skills|gh|git`, and must not expose `--repo`, `--worktree-provider`,
or the local `intent` command. A binary that accepts `add` syntax but has the old
worktree or reviewer behavior is incompatible with this guide.

Reuse an installation only after this compatibility check. Preserve compatible
tool pins. If the CLI is missing or incompatible, prefer the user's
existing installation method and select a compatible published release. Choose
one of the supported distribution paths below. Install the CLI globally by
default; the skill package remains repository-local. Follow explicit user or
repository instructions if they select a different installation scope or method.

```sh
# Go: use when Go is the selected installation method
go install github.com/wwwyo/skillctrl@latest
"$(go env GOPATH)/bin/skillctrl" --version

# Homebrew: use when Homebrew is the selected installation method
brew tap wwwyo/tap
brew install wwwyo/tap/skillctrl
skillctrl --version

# mise
mise use --global github:wwwyo/skillctrl@latest
mise exec -- skillctrl --version
```

Reuse compatible global tools. Do not change tool configuration in the target
repository merely to install the CLI or its runtime dependencies.

`@latest` resolves a published version; follow repository pinning and release-age
rules. Reuse an existing compatible installation. The remaining
examples use `skillctrl` on PATH. If needed, substitute the resolved executable
(such as `"$(go env GOPATH)/bin/skillctrl"`) or the tool manager's execution
wrapper (such as `mise exec -- skillctrl` for a mise-managed install).
The chosen invocation must expose Git and the selected adapter's runtime tools
on PATH. If a tool manager supplies those dependencies, use its execution wrapper
even when the CLI itself was installed through Go or Homebrew; dependency probes
in a different environment do not make the tools available to skillctrl.
Recheck the required command surface after installation. If no compatible
published release meets the repository's release-age policy, report that setup cannot
complete yet; do not invoke an older `add` or bypass the cooldown. Make sure the
executable is reachable by this agent; a successful installation with an
inaccessible executable is incomplete setup.

Inspect `skillctrl add --help`. The default acquisition adapter is `skills`,
which reuses a stable `skills` 1.x version at least 1.7.0 on PATH. If it is absent,
incompatible, or cannot report its version, skillctrl runs
`npx --yes --ignore-scripts skills@1.7.0`. A separate skills installation is optional.
The npx path requires Node.js/npm; the package requires Node.js 22.20.0 or newer.
Confirm that help mentions `pinned npx` before relying on this behavior; older
skillctrl releases require a separately installed skills CLI. Install missing
Node.js through the user's existing tool manager and verify `node --version`
and `npx --version` through its execution wrapper when necessary. If the target
environment uses mise and no compatible pin already exists, the example is:

```sh
mise use --global node@lts
mise exec -- node --version
mise exec -- npx --version
```

Keep an explicitly selected `gh` or `git` adapter if the user or repository
already chose it. `gh` needs GitHub CLI; `git` uses Git directly. Do not silently
switch adapters when a tool is missing, or use an unpinned npx download. The pinned
npx path may download the package on first use and caches it without modifying
the project's package.json or installing a global CLI. Respect an explicitly
configured npm cache; otherwise use the caller's ~/.npm outside disposable staging.
Acquisition failures are errors, not a reason to retry with another backend. If the
installed CLI lacks a feature needed here, resolve a published compatible
version through the chosen manager rather than guessing unsupported flags.

## Install the repository-local skill

Run from the selected Git repository and import only this package, using the CLI
invocation resolved above. The CLI creates missing skill directories:

```sh
skillctrl add wwwyo/skillctrl:skillctrl
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
skills-lock.json                      native upstream registrations at repository root
.agents/skillctrl/lock.json             provenance and explicitly recorded hashes
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
`skills-lock.json` and `.agents/skillctrl/lock.json` when present; the returned `repo` must identify the selected checkout.

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
