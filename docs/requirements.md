# Go rewrite and public release

## User request

Publish skillctrl as broadly usable open source, keeping its current requirements while rewriting it in Go. Use the MIT license and English by default. Deliver working installation through mise, Homebrew, and go install.

## Authoritative source

The existing implementation is in the dotfiles checkout used to start this task: `.agents/skillctrl/{skillctrl,upstream,lock,ci,update}.py`, its four `*.test.sh` suites, `ci-prompt.md`, and `README.md`. Its gh-aw integration is in `.github/workflows/skillctrl.md`, its compiled workflow and shared engine. Read the implementation and fixtures, not only this overview. Source paths are passed privately to the implementation agent; never commit machine-specific paths.

## Compatibility and safety

- Preserve find, add, update, remove, status, record, schema, --repo and --dry-run; support discoverable help and version output. Emit JSON on stdout and diagnostics on stderr. Exit 2 means unresolved adaptation; exit 1 means failure.
- Preserve GitHub owner/repo and HTTPS source validation, explicit skill selection, frontmatter/directory name resolution, relocated skills, source commit and original tree hashes, same-original skip behavior, and version-3 upstream lock compatibility.
- Clone tracked upstream content without running upstream installers, hooks, or scripts. Reject symlinks, submodules, Git control files, destination-ignored content, escapes, malformed names, ambiguous matches, and unsupported sources. Prepare all requested results before importing any; remove requires no network. Keep intent documents when removing a skill.
- Preserve `.agents/skills`, relative Claude links, `.agents/.skill-lock.json`, and accepted version-2 `.agents/skillctrl/intents/lock.json`. Keep whole-directory Git tree hashing including executable mode; use a private index and never disturb caller staging when recording hashes.
- Intent changes alone do not trigger adaptation. Compare current skill hashes to accepted hashes. Adapt only differing skills with intent; record differing skills without intent directly. Advance only explicitly accepted hashes. Leave unresolved content unaccepted and report it.
- Preserve clean-checkout requirements, isolated worktree mutation, atomic preparation, and no implicit commit/push/PR by ordinary installer commands. Preserve the Orca worktree integration for current users. Remove hardcoded dotfiles ownership/toolchain assumptions so other Git repositories can use the tool; document and test the chosen portable isolation path instead of silently weakening safety.
- Preserve pi/OpenCode Go adaptation compatibility and its existing model exception. External adaptation dependencies are optional for find/status/record and skills without intent. Isolate the agent, enforce target-skill-only changes, reject secret leakage in exported artifacts, and retain unresolved work/report on failure. Do not implement a Go wrapper which merely invokes the old Python code.
- Preserve CI plan, trusted checker/input separation, accepted/unresolved partition checks, head-drift rejection, upstream fixed-input verification, scheduled update preparation, final lock status, and safe draft publication behavior. Provide reusable Go commands and documented CI integration. Do not publish a trimmed installer as a full requirements-preserving rewrite.
- Keep prompts, help, errors, public docs and release notes English by default. An accompanying concise Japanese README may be provided.

## Distribution

- Public repository `wwwyo/skillctrl`, module `github.com/wwwyo/skillctrl`, MIT copyright 2026 wwwyo.
- Root main package so `go install github.com/wwwyo/skillctrl@v0.1.0` works. Use `github.com/spf13/cobra` for the CLI command tree, flags, help, and argument validation (explicit user request). Pin the newest stable release published at least seven days before installation; v1.10.2 currently meets that condition. Prefer the standard library for other functionality; external dependencies must be exact and older than seven days at installation.
- Release assets and checksums for macOS/Linux amd64/arm64, with truthful platform documentation. Use a tagged reproducible release workflow and meaningful CI.
- Verify `mise` GitHub backend can install the published archive and run its binary. Research actual archive selection and checksums against official docs.
- Publish a functional custom Homebrew tap (`wwwyo/homebrew-tap` if available, or another explicit project-specific tap), checksummed formula and a smoke test. Do not claim Homebrew core inclusion or bare `brew install skillctrl` unless actually achieved. No plaintext cross-repository publishing token; manual tap updates are acceptable for the initial release.
- Publish an initial release only after compatibility tests and CI pass. Verify the three documented installation paths in temporary locations without replacing the existing deployed dotfiles skillctrl command.

## Verification and project setup

Port meaningful existing fixtures, including adversarial inputs, stage-preservation, no-write dry runs, adaptation rejection and independent CI validation. Run formatting, tests, race tests where appropriate, vet and cross-build checks. Record a source-behavior-to-Go-test parity table in docs; name actual gaps rather than hiding them.

Follow project-setup: pinned mise tools, English AGENTS/README and linked Japanese README, CLAUDE redirect, public MIT repo, auto-delete merged branches. Enable personal Langfuse opt-in for all four agents and configure Pullfrog for this public repository (explicit user request). Keep telemetry opt-in and credentials in ignored local files. No unapproved skill installation. Create the mandatory personal project wiki entry through the wiki/delegate workflow, including index, log, project-focus and lint registration; keep personal project records outside this public repository.

Do not modify or replace dotfiles' deployed implementation in this task. The source checkout is read-only. Commit/push/create public repo, release/tag and custom Homebrew tap are authorized for this new project, as requested by the user. If using a PR, complete the pr skill workflow before considering it ready; publishing the initial release requires verified code on the default branch.

Report public repo/release/tap links, tested install commands and their observed versions, validation evidence, any real blocker or unverified live integration, and the wiki project record. Setup or prompt dispatch alone is not the implementation result.
