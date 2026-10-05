# Use the same commands locally and in automation

skillctrl manages acquisition and explicit hash recording. It does not launch
AI, run a timer, commit, push, or create a PR. Local work, a scheduled job, and
an agent in CI use the same commands in their current checkout.

## Fail CI on unrecorded content

Install the pinned CLI and `jq` through your repository's tool configuration,
then run this from the repository:

```bash
set -euo pipefail
skillctrl check > skillctrl-check.json
jq -e '.local.lock_changed == false' skillctrl-check.json
```

`check` compares current whole-directory skill hashes with the accepted lock.
Only skills with both upstream registration and saved intent are eligible.
It also reports stale accepted entries that need pruning. It never contacts
upstreams or assesses whether instructions satisfy intent. Reported differences
alone exit successfully; the `jq` predicate makes them fail CI. Invalid inputs
or an unreadable lock already make `check` fail.

Intent edits are free and do not change a skill hash. If intent changes, review
and edit the skill to match before explicitly recording its content. A passing
hash check proves the content matches the recorded hash, not the intent.

## Update originals on a schedule

A scheduler invokes the same flow as a person or agent. Replace `chosen-skill`
with the local name; omit it from `update` to fetch every registered skill.

```bash
set -euo pipefail
skillctrl check chosen-skill
skillctrl update chosen-skill
git diff -- .agents/skills/chosen-skill skills-lock.json .agents/skillctrl/upstreams.json
# Review the saved intent and the whole changed skill, then edit and verify it.
skillctrl record chosen-skill
skillctrl check > skillctrl-check.json
jq -e '.local.lock_changed == false' skillctrl-check.json
```

The initial check shows existing local hash drift. Inspect it before importing;
imports that would replace pending skill edits are refused. An unchanged
original preserves local customization. A changed single-input original replaces
its skill content; a merged update refreshes the registered reference directories
and preserves the routing body. The diff compares the current installation before
and after import, not a separately reconstructed pair of upstream originals.
Include references, scripts, and executable modes in review.

Read `.agents/skillctrl/intents/chosen-skill.md` when it exists. After verifying
the final content against that intent, `record chosen-skill` computes and saves
its hash. Do not record an unverified result. Intent-free skills need no accepted
hash; skip `record` for them. `record` without names only prunes ineligible hashes
and accepts no skill content. `update` never advances accepted hashes.

The scheduler, agent configuration, and review instructions belong to the caller.
An agent can use the installed skillctrl skill; the CLI does not select or start
a model. No additional phase protocol or environment plan is required.

## Publish through a pull request

Use the same repository checks locally and in CI. A fixed script or lint can
check permitted paths and snapshot integrity; required checks should come from
trusted repository configuration and should not rely on an agent's report.
A hash check alone does not restrict changed paths or validate intent.

Let Git or GitHub CLI commit, push the review branch, and open a draft PR after
verification. The agent may push that branch if authorized; job separation is
optional. Protect the default branch, require the checks and review, and merge
through a PR. skillctrl does not manage these permissions or publish changes.

The former `ci` and `schedule` command families are removed. Existing automation
must switch to the basic commands above; internal review artifacts and phase
variables are no longer an interface.
