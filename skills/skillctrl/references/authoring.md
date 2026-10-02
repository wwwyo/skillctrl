# Create or improve a skill

Use this workflow for a new reusable skill, an intentional adaptation of an
imported skill, or a revision whose behavior needs checking. It is self-contained;
no separately installed `find-skills` or `skill-creator` skill is required.

## Capture a concrete workflow

Extract the task, triggers, constraints, tools, expected output, and completion
criteria from the conversation and repository. Ask only for details that affect
the result and cannot be inferred. Prefer a workflow that has already been used
and corrected over generic advice invented for a hypothetical task.

Search for an existing skill if the user is choosing between reuse and creation.
If creation is explicitly requested, do not block it on a public search. Reuse an
existing local skill when it owns the same task; preserve its name unless a
rename is requested. Keep full benchmarking or description optimization in a
separate evaluation task when that is requested, rather than requiring it for
every small edit.

## Write the package

For a skill used in the target repository, write
`.agents/skills/<name>/SKILL.md`. For a package intended for distribution, use the
source repository's distribution layout (for example `skills/<name>/SKILL.md`)
and test a separate installation. Do not confuse a published source directory
with the active installation or run `record` against an uninstalled package.

Use the Agent Skills format:

```markdown
---
name: example-workflow
description: "Create and check an example artifact. Use when the user requests the artifact, names this workflow, or asks to revise its output."
---

# Example workflow

Describe the intended result, the steps that produce it, and how to verify it.
```

Keep `name` identical to the enclosing directory. Use 1–64 lowercase letters,
digits, and hyphens, without leading, trailing, or repeated hyphens. Keep the
description within 1,024 characters and include both the task and its triggers.
Quote YAML strings containing `: ` or other YAML-sensitive punctuation. Add a
license and tool compatibility information when relevant to distribution.

Write a concise procedure with concrete defaults, actual tool commands, decision
points, and observable completion criteria. Explain non-obvious constraints.
Avoid restating everything an agent already knows or requiring approval for
routine actions the user already authorized. Keep credentials and personal
configuration out of a public package.

Put substantial task-specific details in `references/`, reusable deterministic
helpers in `scripts/`, and templates in `assets/`. Link each resource from
`SKILL.md` with a condition for reading or running it. Keep the entrypoint under
500 lines and ensure the package works when copied away from its source repo;
do not depend on private paths or a separately installed helper skill. Use
https://agentskills.io/specification to resolve format questions.

For an imported skill, also write its customization requirements in
`.agents/skillctrl/intents/<name>.md`. A handwritten skill does not need a fake
upstream registration.

## Verify and improve

Choose two or three realistic prompts that cover the main workflow, a likely
edge case, and a nearby request that should not activate the skill. Define what
success would look like before running them. Store reusable cases in
`evals/evals.json` when the package benefits from repeatable evaluation.

Validate frontmatter, relative links, command examples, required tools, and
bundled scripts. Then run representative tasks in a disposable or authorized
workspace. Use fixtures or read-only plans for operations that would otherwise
install software, publish content, or modify live configuration. Label those
limits in the results rather than claiming a real installation was tested.

If an independent runner is available and the task calls for evaluation, compare
the same cases with and without the new skill (or with the prior version for an
edit). Grade observable outcomes and inspect the instructions followed, not just
the final prose. Do not infer a measured improvement from static validation or a
single unpaired run. Check false triggers separately from execution correctness.

Revise concrete failures, remove instructions that did not help, and repeat the
affected cases. Scale evaluation to the change: a command correction needs less
work than a new workflow. Report what was exercised and what remains untested.

After verifying a skill installed in `.agents/skills/`, inspect its whole
directory and run `skillctrl --repo /actual/working-copy record <name>` to accept
the intentional content. For a distribution source package, review the source
diff instead; acceptance belongs to the separate installed copy. Follow the
user's existing commit and publication instructions.
