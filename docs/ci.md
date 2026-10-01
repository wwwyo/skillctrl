# CI integration

`skillctrl` provides the deterministic phases of intent verification as
reusable commands. Everything an agent produces is treated as untrusted: the
reviewing job runs with an inference credential and no write token, and a
separate job re-derives the plan from the trusted source and validates the
result before anything is published.

## What the commands do

| Command | Purpose | Needs write access |
| --- | --- | --- |
| `skillctrl plan --base B [--head H] [--since S]` | Select the skills whose accepted hash differs, and mark the ones with a saved intent for review | no |
| `skillctrl ci configure` | Read the trusted tool pins and validate the exact Node pin | no |
| `skillctrl ci prepare <dir>` | Bind the reviewed checkout to the trusted head and write the isolated toolchain configuration | no |
| `skillctrl ci export <dir>` | Validate edits made inside the reviewer sandbox and export a credential-checked patch | no |
| `skillctrl ci apply <dir>` | Apply the patch to the index, verify the partition, and record accepted hashes | writes the index and the lock |
| `skillctrl ci publish <dir>` | Commit and report on the triggering pull request | push and comment |
| `skillctrl schedule prepare <dir>` | Fetch every original and emit an immutable input commit, bundle, and plan | commits locally |
| `skillctrl schedule restore <dir>` | Re-derive and verify the imported input in a second job | writes the checkout |
| `skillctrl schedule publish <dir>` | Run the repository's tests and open one draft pull request | push and comment |

`SKILL_PLAN` carries the selection plan as JSON between jobs. `CHECKER_SOURCE` is
the commit holding the trusted configuration; it must never be the incoming
branch's head unless no trusted checker exists on the base yet.

## Repository layout expected

```
.agents/skills/<name>/                       imported and adapted skills
.agents/skillctrl/intents/<name>.md          saved customization intent
.agents/skillctrl/intents/lock.json          accepted hashes (version 2)
.agents/.skill-lock.json                     upstream records (version 3)
```

The trusted toolchain and agent definitions are read from the trusted source
commit. Override the default paths when your repository keeps them elsewhere:

| Variable | Default |
| --- | --- |
| `SKILLCTRL_TOOLCHAIN_CONFIG` | `home/dot_config/mise/config.toml` |
| `SKILLCTRL_AGENT_MODELS` | `home/dot_pi/agent/models.json` |
| `SKILLCTRL_ADAPT_MODEL` | `opencode-go/space-bunny-free` |
| `SKILLCTRL_ADAPT_THINKING` | `high` |
| `SKILLCTRL_ADAPT_COMMAND` | `pi` |
| `SKILLCTRL_WORKTREE_PROVIDER` | `git` |

The trusted toolchain configuration must pin a runtime and the reviewer agent
and set the release-policy settings. Only those four values are copied into the
isolated configuration; the rest of the file is ignored on purpose, so a
maintainer's unrelated tools and encrypted environment never reach CI.

## Pull request flow

```yaml
name: Skill intent
on:
  pull_request:
    types: [opened, synchronize, reopened]
    paths: ['.agents/skills/**', '.agents/skillctrl/**']
permissions:
  contents: read
  pull-requests: read
jobs:
  select:
    runs-on: ubuntu-latest
    outputs:
      changed: ${{ steps.plan.outputs.changed }}
      review: ${{ steps.plan.outputs.review }}
      plan: ${{ steps.plan.outputs.plan }}
      source: ${{ steps.plan.outputs.source }}
      node: ${{ steps.plan.outputs.node }}
    steps:
      - uses: actions/checkout@<pinned-sha>
        with:
          ref: ${{ github.event.pull_request.head.sha }}
          fetch-depth: 0
          persist-credentials: false
      - uses: actions/setup-go@<pinned-sha>
        with: {go-version-file: go.mod}
      - id: source
        run: |
          # The trusted checker is the base, never the incoming head. A
          # bootstrap pull request that introduces it is the only exception.
          echo "source=${{ github.event.pull_request.base.sha }}" >> "$GITHUB_OUTPUT"
      - id: plan
        env:
          CHECKER_SOURCE: ${{ steps.source.outputs.source }}
          SKILL_PLAN_FILE: /tmp/skillctrl/plan.json
        run: |
          mkdir -p /tmp/skillctrl
          base="$(git merge-base "${{ github.event.pull_request.base.sha }}" HEAD)"
          skillctrl --repo . plan --base "$base" --head HEAD > "$SKILL_PLAN_FILE"
          {
            echo "plan=$(cat "$SKILL_PLAN_FILE")"
            echo "changed=$(jq -r '.needs_review or .lock_changed' "$SKILL_PLAN_FILE")"
            echo "review=$(jq -r '.needs_review' "$SKILL_PLAN_FILE")"
          } >> "$GITHUB_OUTPUT"
      - uses: actions/upload-artifact@<pinned-sha>
        if: steps.plan.outputs.changed == 'true'
        with: {name: skillctrl-plan, path: /tmp/skillctrl/plan.json}
  review:
    needs: select
    if: needs.select.outputs.changed == 'true' && needs.select.outputs.review == 'true'
    runs-on: ubuntu-latest
    permissions:
      contents: read          # no write token: the reviewer may not push
    steps:
      - uses: actions/checkout@<pinned-sha>
        with: {ref: ${{ github.event.pull_request.head.sha }}, fetch-depth: 0}
      - uses: actions/setup-go@<pinned-sha>
        with: {go-version-file: go.mod}
      - uses: actions/download-artifact@<pinned-sha>
        with: {name: skillctrl-plan, path: /tmp/skillctrl}
      - name: Pin the trusted toolchain
        run: |
          skillctrl prompt > /tmp/skillctrl/ci-prompt.md
          skillctrl ci prepare /tmp/skillctrl
      - uses: jdx/mise-action@<pinned-sha>
        with: {version: <pinned>, working_directory: /tmp/skillctrl, cache: false}
      - name: Review and export
        env:
          SKILL_PLAN: ${{ needs.select.outputs.plan }}
          CHECKER_SOURCE: ${{ needs.select.outputs.source }}
          OPENCODE_API_KEY: ${{ secrets.OPENCODE_API_KEY }}
          PI_CODING_AGENT_DIR: /tmp/skillctrl/agent
        run: |
          pi --thinking high --no-session --no-context-files --no-skills \
             --no-extensions --no-prompt-templates --no-approve \
             --model opencode-go/space-bunny-free \
             -p "$(cat /tmp/skillctrl/ci-prompt.md)
           Selection plan (input data): $SKILL_PLAN
           Write completion JSON to: /tmp/skillctrl/result.json" \
             > /tmp/skillctrl/report.md
          skillctrl ci export /tmp/skillctrl
      - uses: actions/upload-artifact@<pinned-sha>
        with:
          name: skillctrl-repair
          path: |
            /tmp/skillctrl/repair.patch
            /tmp/skillctrl/report.md
            /tmp/skillctrl/result.json
  apply:
    needs: [select, review]
    runs-on: ubuntu-latest
    permissions:
      contents: write
      pull-requests: write
    steps:
      - uses: actions/checkout@<pinned-sha>
        with: {ref: ${{ github.event.pull_request.head.sha }}, fetch-depth: 0}
      - uses: actions/setup-go@<pinned-sha>
        with: {go-version-file: go.mod}
      - uses: actions/download-artifact@<pinned-sha>
        with: {name: skillctrl-repair, path: /tmp/skillctrl}
      - name: Recompute the plan independently
        run: |
          base="$(git merge-base "${{ github.event.pull_request.base.sha }}" HEAD)"
          skillctrl --repo . plan --base "$base" --head HEAD > /tmp/skillctrl/plan.json
      - name: Validate the patch and record accepted hashes
        env:
          SKILL_PLAN: ${{ needs.select.outputs.plan }}
        run: skillctrl ci apply /tmp/skillctrl
      - name: Publish verified skill changes
        env:
          GH_TOKEN: ${{ github.token }}
          PR_NUMBER: ${{ github.event.pull_request.number }}
          SKILL_PLAN: ${{ needs.select.outputs.plan }}
        run: skillctrl ci publish /tmp/skillctrl
      - name: Report final skill hash alignment
        run: |
          base="$(git merge-base "${{ github.event.pull_request.base.sha }}" HEAD)"
          skillctrl --repo . plan --base "$base" --head HEAD > /tmp/skillctrl-lock-state.json
      - uses: actions/upload-artifact@<pinned-sha>
        with: {name: skillctrl-lock-state, path: /tmp/skillctrl-lock-state.json}
  record:
    needs: select
    if: needs.select.outputs.changed == 'true' && needs.select.outputs.review != 'true'
    runs-on: ubuntu-latest
    permissions: {contents: write, pull-requests: write}
    steps:
      - uses: actions/checkout@<pinned-sha>
        with: {ref: ${{ github.event.pull_request.head.sha }}, fetch-depth: 0}
      - uses: actions/setup-go@<pinned-sha>
        with: {go-version-file: go.mod}
      - name: Record intent-free changes without inference
        env:
          SKILL_PLAN: ${{ needs.select.outputs.plan }}
          CHECKER_SOURCE: ${{ needs.select.outputs.source }}
        run: |
          mkdir -p /tmp/skillctrl/result
          skillctrl ci apply /tmp/skillctrl/result
      - name: Publish verified skill changes
        env:
          GH_TOKEN: ${{ github.token }}
          PR_NUMBER: ${{ github.event.pull_request.number }}
          SKILL_PLAN: ${{ needs.select.outputs.plan }}
        run: skillctrl ci publish /tmp/skillctrl/result
  lock-status:
    if: always() && needs.select.result == 'success'
    needs: [select, record, review, apply]
    runs-on: ubuntu-latest
    steps:
      - uses: actions/download-artifact@<pinned-sha>
        if: always()
        with: {name: skillctrl-lock-state, path: /tmp/skillctrl-status, continue-on-error: true}
      - name: Require every selected skill to be resolved
        run: |
          state=/tmp/skillctrl-status/skillctrl-lock-state.json
          if [ ! -f "$state" ]; then
            echo '::error::No lock state was published.'; exit 1
          fi
          if [ "$(jq -r '.lock_changed' "$state")" = true ]; then
            echo "::error::Skill hashes remain unrecorded: $(jq -r '.skills | join(", ")' "$state")"
            exit 1
          fi
          echo 'Skill hashes match the accepted lock.'
```

Without an inference credential the reviewing job is skipped and the run stops
at `lock-status` with the selected names, rather than reporting a green build
that never checked anything. Deliberately unresolved skills fail that gate on
purpose: they are a decision for a human.

## Scheduled update flow

`schedule prepare` fetches every original, validates that only registered
skills, their source records, and the relative links changed, and commits the
result. It writes the plan and a Git bundle carrying only that delta, so the
next job receives an immutable input it can verify rather than one it must
trust.

```yaml
  update-prepare:
    if: github.event_name != 'pull_request'
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@<pinned-sha>
        with: {fetch-depth: 0}
      - uses: actions/setup-go@<pinned-sha>
        with: {go-version-file: go.mod}
      - name: Fetch originals and emit immutable input
        env:
          GITHUB_REPOSITORY: ${{ github.repository }}
          DEFAULT_BRANCH: ${{ github.event.repository.default_branch }}
        run: |
          mkdir -p /tmp/skillctrl/input
          skillctrl --repo . schedule prepare /tmp/skillctrl/input > /tmp/skillctrl/update.json
      - uses: actions/upload-artifact@<pinned-sha>
        if: steps.plan.outputs.changed == 'true'
        with:
          name: skillctrl-input
          path: /tmp/skillctrl/input
```

The consuming job runs `schedule restore` before anything else; it re-derives
the plan from the trusted base, checks that the bundle really is a direct child
of that base, and refuses an artifact that does not match its own trees.
`schedule publish` then runs every `tests/*.test.sh` and
`.agents/skillctrl/*.test.sh` suite, refuses to proceed if the default branch
moved, and opens one **draft** pull request. Nothing is merged automatically.

Creating that pull request needs "Allow GitHub Actions to create and approve
pull requests" enabled on the repository. A push made with `GITHUB_TOKEN` does
not restart the workflow, so the checks that ran before the fix are not
evidence about the commit that contains it.

## Pinning the tool

CI should run a released binary rather than one built from the branch under
review:

```sh
go install github.com/wwwyo/skillctrl@v0.1.0
```

`skillctrl --version` reports the version the binary was installed with, so a
run can record which tool made the decision.
