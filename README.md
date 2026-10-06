<picture>
  <source media="(prefers-color-scheme: dark)" srcset="assets/wordmark-dark.svg">
  <img src="assets/wordmark.svg" alt="skillctrl" width="312" height="88">
</picture>

[![Go 1.27](https://img.shields.io/badge/go-1.27-blue.svg)](go.mod) [![MIT License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

[日本語](docs/ja/README.md) · [Command details](docs/usage.md) · [Agent workflow guide](.agents/skills/skillctrl/references/ci.md) · [Development](docs/requirements.md)

skillctrl lets you install agent skills and customize them for your project.
Shorten instructions, add your own checks, or combine skills into one workflow.

It keeps upstream originals and the intent behind your edits so your existing
agent can preserve those customizations when adapting upstream updates.

## QuickStart

Give your agent this prompt in the target repository:

```text
Read https://raw.githubusercontent.com/wwwyo/skillctrl/main/docs/start.md and install the skillctrl CLI and skill in this repository.
```

### Manual

Install the CLI globally using one method:

- Go: `go install github.com/wwwyo/skillctrl@latest`
- Homebrew: `brew install wwwyo/tap/skillctrl`
- mise: `mise use --global github:wwwyo/skillctrl@latest`

With a compatible `skills` CLI or Node.js/npm available, import the skill from
the target Git repository:

```sh
skillctrl add wwwyo/skillctrl:skillctrl
```

## Features

- **Your rules ✏️**: Use skills that fit your project's conventions and checks.
- **Customizations that last 🔄**: Get upstream improvements while keeping your local requirements.
- **One entrypoint 🧩**: Use workflows combined from several skills through a single skill.
- **Visible changes 👀**: See which customized skills have changed since you last accepted them.
- **Automated upkeep 🤖**: Keep skills maintained through [CI review and scheduled updates](.agents/skills/skillctrl/references/ci.md), without tracking every upstream change by hand.

## License

Licensed under [MIT](LICENSE).
