<picture>
  <source media="(prefers-color-scheme: dark)" srcset="assets/wordmark-dark.svg">
  <img src="assets/wordmark.svg" alt="skillctrl" width="312" height="88">
</picture>

# skillctrl

[![Go 1.27](https://img.shields.io/badge/go-1.27-blue.svg)](go.mod) [![MIT License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

[日本語](docs/ja/README.md) · [Command details](docs/usage.md) · [Agent workflow guide](.agents/skills/skillctrl/references/ci.md)

Manage agent skills in a Git repository while keeping the intent behind your
local customizations. skillctrl tracks upstream originals and the content you
have verified; your editor or existing agent makes the changes.

## Work with skills

### Keep your intent alongside the skill

An imported skill often needs different instructions for your project. Save the
behavior you want to preserve in `.agents/skillctrl/intents/<name>.md`, then edit
the skill directly. For example: "Keep instructions concise and require tests
before completing code changes." The intent gives you or your agent a basis for
reviewing future changes, instead of preserving a patch that may stop making sense.

The [skillctrl skill](.agents/skills/skillctrl/SKILL.md) guides your agent through finding,
installing, creating, improving, updating, and removing skills. Its authoring
guide is loaded when needed; separate discovery and creation skills are unnecessary.
The CLI itself never starts an AI agent.

### Make acceptance deliberate

`add` imports and registers originals. `record NAME` saves the current hash of
that skill's complete directory after you have checked it, including references,
scripts, and executable modes. It changes the accepted lock, not the skill.
Only skills with both upstream registration and saved intent enter this lock;
handwritten skills and uncustomized imports need no second acceptance step.

```sh
skillctrl add owner/repo:chosen-skill
mkdir -p .agents/skillctrl/intents
# Write the intent and edit the skill with your editor or existing agent
# Verify the behavior and inspect the complete skill directory
skillctrl record chosen-skill
skillctrl check chosen-skill
```

`check` reports local differences without changing files or contacting upstreams.
It does not decide whether a skill meets its intent, and reported drift alone is
not a command failure. Editing content never advances the accepted hash automatically.
The intent document itself is outside the hash.

### Update originals, then review the adaptation

`update NAME` fetches the registered original. If it has not changed, your local
adaptation is preserved. If it has changed, inspect the imported diff, adapt the
skill to its saved intent, verify it, and explicitly record the result:

```sh
skillctrl update chosen-skill
git diff -- .agents/skills/chosen-skill skills-lock.json .agents/skillctrl/lock.json
# Edit and verify the updated skill against its intent
skillctrl record chosen-skill
```

Commands operate in the current repository in place. Unrelated edits and staging
are preserved; an import refuses to replace a skill containing pending edits.
Use `cd` to select another repository.

### Combine sources without losing their identities

`merge` puts complete originals under one skill and generates a root `SKILL.md`
that routes to `references/<upstream-skill-name>/`. Each original keeps its
resources and source registration. Customize the routing and its intent directly;
later `update` refreshes the originals while preserving your root entrypoint.
Explicitly running `merge` again replaces the source list and regenerates routing.

```sh
skillctrl merge owner/first:first-skill owner/second:second-skill --name combined
```

`add` and `merge` share `owner/repo:skill` inputs. Single-skill `add --name NAME`
can choose a local name while tracking the original upstream name. Inputs above
are placeholders; use inspected sources.

Acquisition defaults to `skills`: reuse a compatible CLI on PATH, or use npx.
Installing skills separately is optional;
the npx path requires Node.js and npm.
`--adapter gh` and `--adapter git` select GitHub CLI or direct Git instead.
Root `skills-lock.json` keeps native registrations. Alias/merge tracking, extra provenance,
and explicitly recorded hashes share `.agents/skillctrl/lock.json`.
Use `find`, `list`, and `remove` for discovery, inspection, and cleanup.
See [command details](docs/usage.md) for adapters, locks, and edge cases.

## Set up with your agent

Give your agent this prompt in the target repository:

```text
Read https://raw.githubusercontent.com/wwwyo/skillctrl/main/docs/start.md and install the skillctrl CLI and skill in this repository.
```

[docs/start.md](docs/start.md) covers CLI installation, acquisition dependencies,
the skill package, and checking whether your agent can load it.

## Manual installation

From the target Git repository, install the CLI:

```sh
mise use --path ./mise.toml github:wwwyo/skillctrl@latest
mise exec -- skillctrl --help
```

Before importing, require help to show `list`, `check`, `record`, and
`--adapter skills|gh|git`, with no `--repo`, `--worktree-provider`, or `intent`
command. Before relying on npx, require help to mention `pinned npx`.
See
[setup](docs/start.md) for Go and Homebrew alternatives and the full compatibility check.

For the default adapter, reuse an existing compatible `skills` CLI or provide
Node.js/npm for the pinned npx path. Install Node.js only if needed, preserving
existing compatible pins, then import the package:

```sh
mise use --path ./mise.toml node@lts
mkdir -p .agents/skills
mise exec -- skillctrl add wwwyo/skillctrl:skillctrl
```

CLI and skill are separate artifacts. Results are JSON on stdout; diagnostics
go to stderr. Exit `0` means success and `1` means failure.

## Optional automation

Use separate agent workflows: PR checks review only hash drift reported by
`check`; scheduled updates call `update`, adapt and verify changed skills, and
open a draft PR. Verified content is accepted with `record NAME`.
See the [agent workflow guide](.agents/skills/skillctrl/references/ci.md).

## Development

```sh
mise install
mise exec -- go test ./...
mise exec -- go vet ./...
mise exec -- go build .
```

Here, `mise exec -- skillctrl` builds and runs the current checkout without
replacing a global installation. Read [requirements](docs/requirements.md),
[behavior coverage](docs/parity.md), and [release instructions](docs/releasing.md)
for development details. Licensed under [MIT](LICENSE).

The distributable skill is itself managed here in `.agents/skills/skillctrl/`:
`.agents/skillctrl/lock.json` tracks its discovery and authoring inputs, and
`.agents/skillctrl/intents/skillctrl.md` records the integration policy.
Use the same `update skillctrl`, direct editing, `record skillctrl`, and `check skillctrl` flow.
