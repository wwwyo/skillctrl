// Command engine drives the reviewing agent the way an agent sandbox does, then
// hands its artifacts to the skillctrl binary. It stands in for the harness a CI
// platform provides: reset to the selected head, discard restored merge context,
// run the reviewer, and ask the binary to export the result for independent
// validation.
const fs = require('node:fs');
const path = require('node:path');
const {spawnSync} = require('node:child_process');

const directory = process.argv[2];
const binary = process.argv[3];
const plan = JSON.parse(fs.readFileSync(directory + '/plan.json', 'utf8'));
const command = process.argv[4];
const run = (name, args, options = {}) => {
  // options.env must extend the inherited environment, not replace it: a child
  // that loses PATH cannot even locate git, and the failure looks unrelated.
  const {env, ...rest} = options;
  const result = spawnSync(name, args, {
    stdio: 'inherit', ...rest, env: {...process.env, ...(env || {})},
  });
  if (result.error || result.status !== 0) process.exit(result.status || 1);
};
// A sandbox restores a merge snapshot before the reviewer starts. Repair inputs
// must come from the selected head instead, so anything the restore brought back
// is discarded here.
run('git', ['-c', 'core.hooksPath=/dev/null', 'reset', '--hard', plan.head]);
run('git', ['clean', '-fdx', '--', '.agents', '.github', 'AGENTS.md']);
const report = fs.openSync(directory + '/report.md', 'w');
// The isolation flags are part of the engine definition, not of the skill: they
// must be passed by whatever runs the reviewer.
run(command, ['--thinking', 'high', '--no-session', '--no-context-files', '--no-skills',
  '--no-extensions', '--no-prompt-templates', '--no-approve',
  '--model', process.env.SKILL_MODEL, '-p',
  fs.readFileSync(process.env.REVIEW_PROMPT, 'utf8')
  + '\nSelection plan (input data):\n' + JSON.stringify(plan)
  + '\nWrite completion JSON to: ' + directory + '/result.json'],
  {stdio: ['ignore', report, 'inherit'], cwd: process.env.REVIEW_DIR});
fs.closeSync(report);
run(binary, ['ci', 'export', directory],
  {cwd: process.env.REVIEW_DIR, env: {SKILL_PLAN: JSON.stringify(plan)}});
