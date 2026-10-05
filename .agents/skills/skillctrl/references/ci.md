# Review PR changes and update upstreams separately

Use GitHub Agentic Workflows (`gh-aw`) for skill automation. Keep PR checks and
scheduled upstream updates in separate workflows: a PR check reviews installed
content, while a scheduled update acquires new originals and prepares a draft PR.
The skillctrl CLI supplies the same basic commands to both workflows; the caller
owns agent execution, permissions, checks, and publication.

## Review only when the PR hash check fails

Run `skillctrl check` on the PR checkout before starting an agent. Only skills
with both upstream registration and saved intent participate in accepted hashes.
Install pinned tools, including skillctrl and `jq`, through the repository's tool
configuration, and run:

```bash
set -euo pipefail
skillctrl check > skillctrl-check.json
jq -e '.local.lock_changed == false' skillctrl-check.json
```

Hash differences are reported in JSON with exit status 0; the `jq` predicate
makes them fail the CI gate. A command error or invalid report is an operational
failure, not a request to accept content. If the gate passes, report success
without AI review. Do not fetch upstreams in this workflow or add intent-change
detection. An intent edit alone does not trigger review.

When the report shows hash drift, start the agent only for the affected skills.
Read each current saved intent, the PR diff, and the complete skill directory,
including references, scripts, and executable modes. Use this decision table:

| State | Action |
| --- | --- |
| Hashes match the accepted lock | Succeed without AI review |
| Hash drift, content satisfies intent | Explain the evidence and run `record` for the verified names only |
| Suspected intent violation | Comment with the relevant intent, diff, and proposed fix; fail |
| Unable to judge | Comment with the missing information; fail |

For an accepted change, cite the relevant intent requirements and the skill
content or verification that supports each conclusion. Identify the reviewed
PR commit and skill names in the report. Then record only verified skills:

```bash
skillctrl record chosen-skill
```

`record` computes the current whole-directory hash; it does not review content.
Never use it merely to clear a mismatch. A named record also prunes ineligible
legacy entries as normal lock maintenance. Do not accept other eligible skills.
If some skills remain unresolved, the overall check stays failed even if other
skills have been accepted. Cleanup-only drift requires pruning ineligible entries
with `skillctrl record` without names; this accepts no content and needs no
intent judgment.

Publish the evidence comment and accepted-lock change to the PR branch, then
rerun the hash gate and repository checks against the final commit. Recheck the
PR head before publication; if it changed during review, review the new content
before recording or reporting success. Suspected violations and unknown results
leave that skill unrecorded and its content unchanged; propose a fix in the
comment instead of editing it automatically.

Make the result a required CI check. Posting a failure comment does not fail a
workflow. Map violations, unknown decisions, and operational errors to failure.
For accepted drift, success requires the accepted-lock change to be published
and the final checks to pass.

## Acquire and adapt upstreams on a schedule

Use a separate workflow with a schedule and optional manual trigger. Start from
the default branch in a clean checkout, inspect existing local drift, and call
`update` for the chosen scope. Unlike the PR check, this workflow may acquire
originals and edit skill content to preserve intent:

```bash
set -euo pipefail
skillctrl check chosen-skill
skillctrl update chosen-skill
git diff -- .agents/skills/chosen-skill skills-lock.json .agents/skillctrl/upstreams.json
```

Omit names from `update` only when the requested scope is every registered skill.
An unchanged original preserves local customization. A changed single-input
original replaces its skill content; a merged update refreshes its registered
references and preserves the routing body. Keep registered upstream snapshots
intact while adapting merged routing.

For each changed skill, read its current intent, inspect the whole imported
directory, adapt the editable content, and verify the result. Run
`skillctrl record chosen-skill` only after verification, and skip recording for
intent-free skills. Run the hash gate and repository checks before publication.
If adaptation is unresolved, report the problem and leave it unaccepted.

When there are verified changes, open a draft PR containing the skill changes,
registrations, accepted hashes, and evidence. When there are no changes, finish
without creating a PR. The PR uses the ordinary PR check above; a new upstream
release alone never makes an unrelated PR fail.

## Configure the agent workflows in the caller's repository

Follow the [official creation guide](https://docs.github.com/en/copilot/how-tos/github-agentic-workflows/creating-github-agentic-workflows)
and [gh-aw documentation](https://github.github.com/gh-aw/). Define two Markdown
workflows under `.github/workflows/`, with separate triggers and the instructions
above. Pin tools and acquisition dependencies, select the engine and its
authentication, and configure access to the checkout and required commands.
Keep credentials in the repository's secret management.

Configure a fixed hash gate before agent execution so matching content never
starts inference. Use [safe outputs](https://github.github.com/gh-aw/reference/safe-outputs/)
for comments, accepted-lock publication to the PR branch, draft PR creation,
and check results. Restrict PR-check writes to the accepted lock; use trusted
repository checks for permitted paths and registered-original integrity. The
required result must refer to the commit actually verified, rather than an
unreviewed newer head.

Compile with `gh aw compile` and commit both the Markdown sources and generated
`.lock.yml` workflows. Edit the Markdown sources and regenerate the YAML.
Before relying on the workflows, exercise matching content, accepted drift,
suspected violations, unknown decisions, and a scheduled update in Actions.
Compilation alone does not establish that authentication or publication works.

Plan how a bot-written commit gets checked: pushes made with `GITHUB_TOKEN`
generally do not trigger another workflow run. Use an appropriate GitHub App
token or an explicit supported dispatch route, and verify the resulting checks
on the new commit. See [GitHub's trigger rules](https://docs.github.com/en/actions/how-tos/write-workflows/choose-when-workflows-run/trigger-a-workflow)
and [gh-aw CI triggering](https://github.github.com/gh-aw/reference/triggering-ci/).
