// Package integration drives the documented CI phase sequence against the
// built binary.
//
// The unit suites check each command in isolation. This one runs them in the
// order docs/ci.md tells an adopter to run them, through the real executable, so
// the documented contract cannot drift away from what the tool actually does -
// including the refusal that stops a second job from applying a plan it did not
// recompute.
package integration

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

var (
	buildOnce sync.Once
	binary    string
)

const (
	baseCommit = "fixture"
	credential = "fixture-inference-credential-never-publish"
)

func binaryPath(t *testing.T) string {
	t.Helper()
	buildOnce.Do(func() {
		directory, err := os.MkdirTemp("", "skillctrl-integration-")
		if err != nil {
			t.Fatal(err)
		}
		binary = filepath.Join(directory, "skillctrl")
		command := exec.Command("go", "build", "-o", binary, "github.com/wwwyo/skillctrl")
		if out, err := command.CombinedOutput(); err != nil {
			t.Fatalf("build: %v\n%s", err, out)
		}
	})
	return binary
}

type env struct {
	values map[string]string
}

// newEnv takes alternating key and value arguments, so a value containing an
// equals sign or a newline cannot be confused with the next key.
func newEnv(pairs ...string) *env {
	e := &env{values: map[string]string{}}
	if len(pairs)%2 != 0 {
		panic("env takes alternating keys and values")
	}
	for i := 0; i < len(pairs); i += 2 {
		e.values[pairs[i]] = pairs[i+1]
	}
	return e
}

func (e *env) with(pairs ...string) *env {
	next := newEnv()
	for key, value := range e.values {
		next.values[key] = value
	}
	for i := 0; i+1 < len(pairs); i += 2 {
		next.values[pairs[i]] = pairs[i+1]
	}
	return next
}

// list merges the overrides over the inherited environment. Replacing an
// inherited entry matters: a child that sees both reads the first one, so
// appending an override would silently do nothing.
func (e *env) list() []string {
	inherited := map[string]string{}
	for _, entry := range os.Environ() {
		if key, value, ok := strings.Cut(entry, "="); ok {
			inherited[key] = value
		}
	}
	for key, value := range e.values {
		inherited[key] = value
	}
	result := make([]string, 0, len(inherited))
	for key, value := range inherited {
		result = append(result, key+"="+value)
	}
	return result
}

// run invokes the binary and fails the test unless the exit code matches.
func run(t *testing.T, dir string, e *env, code int, args ...string) string {
	t.Helper()
	command := exec.Command(binaryPath(t), args...)
	command.Dir = dir
	command.Env = e.list()
	var stdout, stderr strings.Builder
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	status := 0
	if err != nil {
		exit, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("run %v: %v", args, err)
		}
		status = exit.ExitCode()
	}
	if status != code {
		t.Fatalf("%v exited %d want %d\nstdout: %s\nstderr: %s", args, status, code, stdout.String(), stderr.String())
	}
	return stdout.String()
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = dir
	out, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeScript(t *testing.T, path, body string) {
	t.Helper()
	shell, err := exec.LookPath("bash")
	if err != nil {
		shell = "/bin/sh"
	}
	write(t, path, strings.Replace(body, "#!/usr/bin/env bash", "#!"+shell, 1))
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

// phaseRepository builds the repository an adopter would have: one skill with an
// intent, one without, and a trusted toolchain on the base commit.
func phaseRepository(t *testing.T) (string, string, string, *env) {
	t.Helper()
	base := t.TempDir()
	repo := filepath.Join(base, "repo")
	tools := filepath.Join(base, "trusted-tools")
	caller := filepath.Join(base, "caller")
	for _, directory := range []string{repo, tools, caller} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// Temporary directories sit behind a symlink on macOS and the shell reports
	// the physical path, so every expectation uses it too.
	repo = physical(t, repo)
	caller = physical(t, caller)
	tools = physical(t, tools)
	write(t, filepath.Join(repo, ".agents/skills/manual/SKILL.md"), "original v1\n")
	write(t, filepath.Join(repo, ".agents/skills/plain/SKILL.md"), "original v1\n")
	write(t, filepath.Join(repo, ".agents/skillctrl/intents/manual.md"), "keep the default browser\n")
	write(t, filepath.Join(repo, "skills-lock.json"), `{"version":3,"skills":{"manual":{"source":"fixture/source","sourceType":"github"},"plain":{"source":"fixture/source","sourceType":"github"}}}`)
	write(t, filepath.Join(repo, "home/dot_config/mise/config.toml"),
		"[tools]\nnode = \"22.11.0\"\n\"npm:@earendil-works/pi-coding-agent\" = \"0.55.1\"\n"+
			"[settings]\npin = true\nminimum_release_age = \"7d\"\n")
	write(t, filepath.Join(repo, "home/dot_pi/agent/models.json"), `{"trusted": true}`)
	// An upstream change that only the intent-free skill made.
	write(t, filepath.Join(repo, ".agents/skills/plain/SKILL.md"), "original v2\n")

	git(t, repo, "init", "-q")
	git(t, repo, "config", "user.name", "Fixture")
	git(t, repo, "config", "user.email", "fixture@example.invalid")
	git(t, repo, "config", "commit.gpgsign", "false")
	git(t, repo, "add", "-A")
	git(t, repo, "commit", "-qm", baseCommit)

	// The trusted resolver refuses to run anywhere except the prepared
	// directory, which is how the reviewed job proves it did not evaluate the
	// caller's or the reviewed repository's configuration.
	// The stubs carry their expectations inline: the reviewer inherits a reduced
	// environment by design, so passing them as variables would test nothing.
	writeScript(t, filepath.Join(tools, "mise"), `#!/usr/bin/env bash
set -euo pipefail
[ "$PWD" = "`+artifactsFor(tools)+`" ] || { echo "mise resolved in $PWD" >&2; exit 1; }
printf '{"PATH":"`+tools+`","OPENCODE_API_KEY":"`+credential+`"}'
`)
	writeScript(t, filepath.Join(tools, "pi"), `#!/usr/bin/env bash
set -euo pipefail
[ "$PWD" = "`+repo+`" ] || { echo "reviewer ran in $PWD" >&2; exit 1; }
[ "${PATH:-}" = "`+tools+`" ] || { echo "reviewer PATH is not the trusted one" >&2; exit 1; }
[ "${OPENCODE_API_KEY:-}" = "`+credential+`" ] || { echo "reviewer credential is not the resolved one" >&2; exit 1; }
[ -f "${PI_CODING_AGENT_DIR}/models.json" ] || { echo "trusted models are missing" >&2; exit 1; }
case " $* " in
  *" --no-context-files "*) ;;
  *) echo 'reviewer ran without isolation flags' >&2; exit 1;;
esac
printf 'original v2; default browser\n' > .agents/skills/manual/SKILL.md
result="${@: -1}"; result="${result##*Write completion JSON to: }"
printf '{"accepted":["manual"],"unresolved":[]}' > "$result"
echo 'the default browser criterion is preserved'
`)
	environment := newEnv(
		"PATH", tools+string(os.PathListSeparator)+os.Getenv("PATH"),
		// The caller holds no credential of its own.
		"OPENCODE_API_KEY", "",
	)
	return repo, caller, tools, environment
}

// TestDocumentedPhaseSequence runs docs/ci.md end to end: select, review in a
// job with no write token, validate in a job that recomputes the plan, then
// gate on the recomputed state.
func TestDocumentedPhaseSequence(t *testing.T) {
	repo, caller, tools, environment := phaseRepository(t)
	head := git(t, repo, "rev-parse", "HEAD")
	artifacts := filepath.Join(caller, "artifacts")
	if err := os.MkdirAll(artifacts, 0o755); err != nil {
		t.Fatal(err)
	}

	t.Run("select", func(t *testing.T) {
		plan := run(t, repo, environment, 0,
			"ci", "plan", "--base", head, "--head", head)
		var selection struct {
			Skills       []string `json:"skills"`
			ReviewSkills []string `json:"review_skills"`
			NeedsReview  bool     `json:"needs_review"`
			LockChanged  bool     `json:"lock_changed"`
		}
		if err := json.Unmarshal([]byte(plan), &selection); err != nil {
			t.Fatalf("plan is not JSON: %v\n%s", err, plan)
		}
		if len(selection.Skills) != 1 || selection.Skills[0] != "manual" {
			t.Fatalf("unexpected selection: %v", selection.Skills)
		}
		if len(selection.ReviewSkills) != 1 || selection.ReviewSkills[0] != "manual" {
			t.Fatalf("unexpected review selection: %v", selection.ReviewSkills)
		}
		if !selection.NeedsReview || !selection.LockChanged {
			t.Fatalf("unexpected flags: %+v", selection)
		}
		write(t, filepath.Join(caller, "plan.json"), plan)
	})

	plan := readFile(t, filepath.Join(caller, "plan.json"))
	job := environment.with(
		"SKILL_PLAN", plan,
		"CHECKER_SOURCE", head,
		// The reviewing job holds the inference credential, and ci export
		// screens artifacts against the credential that job was given.
		"OPENCODE_API_KEY", credential,
	)

	t.Run("prepare refuses without a trusted source", func(t *testing.T) {
		without := environment.with("SKILL_PLAN", plan)
		run(t, repo, without, 1, "ci", "prepare", artifacts)
	})

	t.Run("review", func(t *testing.T) {
		run(t, repo, job, 0, "ci", "prepare", artifacts)
		config := readFile(t, filepath.Join(artifacts, "mise.toml"))
		for _, want := range []string{`node = "22.11.0"`, `"npm:@earendil-works/pi-coding-agent" = "0.55.1"`,
			"pin = true", `minimum_release_age = "7d"`} {
			if !strings.Contains(config, want) {
				t.Fatalf("prepared toolchain is missing %s:\n%s", want, config)
			}
		}
		// The reviewing job runs the agent under the prepared toolchain with the
		// contract this binary embeds, then exports what it produced.
		runReviewer(t, repo, artifacts, tools, job, plan)
		run(t, repo, job, 0, "ci", "export", artifacts)
		patch, err := os.ReadFile(filepath.Join(artifacts, "repair.patch"))
		if err != nil {
			t.Fatalf("no patch was exported: %v", err)
		}
		if !strings.Contains(string(patch), "default browser") {
			t.Fatalf("exported patch is empty: %q", patch)
		}
		if strings.Contains(string(patch), credential) {
			t.Fatal("the exported patch contains the inference credential")
		}
	})

	// The validating job is a separate checkout, exactly as a CI job would be:
	// the reviewer's edits never reach it, and the patch applies to an index
	// that still holds the original.
	validator := cloneAt(t, repo, head, filepath.Join(caller, "validator"))
	validatorJob := job.with("SKILL_PLAN", plan)

	t.Run("validate refuses a review set the agent did not answer", func(t *testing.T) {
		// A plan that claims nothing needs review, while the artifact answers for
		// one skill, is exactly the mismatch the partition check exists for.
		forged := strings.Replace(plan, `"review_skills":["manual"]`, `"review_skills":[]`, 1)
		if forged == plan {
			t.Fatal("could not build a mismatched plan")
		}
		other := cloneAt(t, repo, head, filepath.Join(caller, "forged"))
		run(t, other, validatorJob.with("SKILL_PLAN", forged), 1, "ci", "apply", artifacts)
		if git(t, other, "diff", "--cached", "--name-only") != "" {
			t.Fatal("a refused validation staged changes")
		}
	})

	t.Run("validate", func(t *testing.T) {
		recomputed := run(t, validator, validatorJob, 0,
			"ci", "plan", "--base", head, "--head", head)
		if normalise(recomputed) != normalise(plan) {
			t.Fatalf("the recomputed plan differs:\n%s\n%s", plan, recomputed)
		}
		run(t, validator, validatorJob, 0, "ci", "apply", artifacts)
		if body := readFile(t, filepath.Join(validator, ".agents/skills/manual/SKILL.md")); body != "original v1\n" {
			t.Fatalf("apply modified the working tree: %q", body)
		}
		if got := git(t, validator, "show", ":.agents/skills/manual/SKILL.md"); got != "original v2; default browser" {
			t.Fatalf("the patch was not applied to the index: %q", got)
		}
		staged := git(t, validator, "diff", "--cached", "--name-only")
		for _, path := range strings.Split(staged, "\n") {
			if path == "" {
				continue
			}
			if !strings.HasPrefix(path, ".agents/skills/") && path != ".agents/skillctrl/intents/lock.json" {
				t.Fatalf("validation staged an unrelated path: %s", path)
			}
		}
	})

	t.Run("gate before publication still reports unresolved work", func(t *testing.T) {
		// ci apply is index-only, so until the repair is committed the hashes on
		// disk still differ from the accepted lock. The gate must say so.
		state := run(t, validator, validatorJob, 0, "ci", "plan", "--base", head, "--head", "HEAD")
		var selection struct {
			LockChanged bool `json:"lock_changed"`
		}
		if err := json.Unmarshal([]byte(state), &selection); err != nil {
			t.Fatal(err)
		}
		if !selection.LockChanged {
			t.Fatal("an uncommitted repair was reported as resolved")
		}
	})

	t.Run("publish", func(t *testing.T) {
		remote := filepath.Join(caller, "remote.git")
		git(t, caller, "init", "--bare", "-q", remote)
		git(t, validator, "remote", "set-url", "origin", remote)
		ghDir := filepath.Join(caller, "gh")
		if err := os.MkdirAll(ghDir, 0o755); err != nil {
			t.Fatal(err)
		}
		log := filepath.Join(caller, "gh.log")
		installFakeGH(t, ghDir, log)
		pull := `{"state":"open","head":{"ref":"feature","sha":"` + head +
			`","repo":{"full_name":"fixture/repo"}},"base":{"ref":"main"}}`
		publishJob := validatorJob.with(
			"PATH", ghDir+string(os.PathListSeparator)+os.Getenv("PATH"),
			"GITHUB_REPOSITORY", "fixture/repo",
			"PR_NUMBER", "7",
			"GH_TOKEN", "fixture-token",
			"FIXTURE_GH_LOG", log,
			"FIXTURE_PULL_JSON", pull,
		)
		// Nothing is staged yet, so a first run has nothing to commit. Running it
		// twice must still post one comment, not two.
		git(t, validator, "reset", "--hard", head)
		run(t, validator, publishJob, 0, "ci", "publish", artifacts)
		if git(t, validator, "rev-parse", "HEAD") != head {
			t.Fatal("publication committed without a staged repair")
		}
		first := strings.Count(readFile(t, log), "POST")
		if first != 1 {
			t.Fatalf("expected one report, got %d", first)
		}
		run(t, validator, publishJob, 0, "ci", "publish", artifacts)
		if second := strings.Count(readFile(t, log), "POST"); second != first {
			t.Fatalf("a rerun posted a duplicate comment: %d then %d", first, second)
		}

		// Now publish the repair itself.
		run(t, validator, validatorJob, 0, "ci", "apply", artifacts)
		run(t, validator, publishJob, 0, "ci", "publish", artifacts)
		if branch := git(t, validator, "ls-remote", "origin", "refs/heads/feature"); branch == "" {
			t.Fatal("the repair was not pushed to the pull request branch")
		}
		if published := git(t, validator, "rev-parse", "HEAD"); published == head {
			t.Fatal("publication did not create a commit")
		}
	})

	t.Run("gate", func(t *testing.T) {
		state := run(t, validator, validatorJob, 0, "ci", "plan", "--base", head, "--head", "HEAD")
		var selection struct {
			LockChanged bool     `json:"lock_changed"`
			Skills      []string `json:"skills"`
		}
		if err := json.Unmarshal([]byte(state), &selection); err != nil {
			t.Fatal(err)
		}
		if selection.LockChanged {
			t.Fatalf("the gate would fail: %v", selection.Skills)
		}
	})
}

// runReviewer executes the reviewing agent the way docs/ci.md specifies and
// fails the test if the agent could have been run without the trusted setup.
func runReviewer(t *testing.T, repo, artifacts, trustedTools string, environment *env, plan string) {
	t.Helper()
	prompt := filepath.Join(artifacts, "ci-prompt.md")
	write(t, prompt, run(t, repo, environment, 0, "ci", "prompt"))
	report, err := os.Create(filepath.Join(artifacts, "report.md"))
	if err != nil {
		t.Fatal(err)
	}
	defer report.Close()
	resolver := exec.Command(filepath.Join(trustedTools, "mise"), "env", "--json")
	resolver.Dir = artifacts
	resolver.Env = environment.list()
	resolved, err := resolver.Output()
	if err != nil {
		t.Fatalf("resolve the prepared toolchain: %v", err)
	}
	var toolchain map[string]string
	if err := json.Unmarshal(resolved, &toolchain); err != nil {
		t.Fatal(err)
	}
	agent := exec.Command(filepath.Join(toolchain["PATH"], "pi"),
		"--thinking", "high", "--no-session", "--no-context-files", "--no-skills",
		"--no-extensions", "--no-prompt-templates", "--no-approve",
		"--model", "opencode-go/space-bunny-free", "-p",
		readFile(t, prompt)+
			"\nSelection plan (input data):\n"+plan+
			"\nWrite completion JSON to: "+filepath.Join(artifacts, "result.json"))
	agent.Dir = repo
	// The reviewing job installs the toolchain that ci prepare pinned and runs
	// the agent under it, so the agent sees that PATH and nothing else.
	agent.Env = environment.with(
		"PATH", toolchain["PATH"],
		"OPENCODE_API_KEY", toolchain["OPENCODE_API_KEY"],
		"PI_CODING_AGENT_DIR", filepath.Join(artifacts, "agent"),
	).list()
	if err := os.MkdirAll(filepath.Join(artifacts, "agent"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(artifacts, "agent", "models.json"),
		git(t, repo, "show", environment.values["CHECKER_SOURCE"]+":home/dot_pi/agent/models.json"))
	agent.Stdout = report
	agent.Stderr = os.Stderr
	if err := agent.Run(); err != nil {
		t.Fatalf("the reviewer failed: %v", err)
	}
}

// cloneAt makes a fresh checkout of one commit, standing in for the separate
// job that validates what the reviewing job produced.
func cloneAt(t *testing.T, repo, commit, target string) string {
	t.Helper()
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, target, "init", "-q")
	git(t, target, "config", "user.name", "Fixture")
	git(t, target, "config", "user.email", "fixture@example.invalid")
	git(t, target, "config", "commit.gpgsign", "false")
	git(t, target, "remote", "add", "origin", repo)
	git(t, target, "fetch", "--quiet", repo, commit)
	git(t, target, "checkout", "--quiet", "--detach", commit)
	return physical(t, target)
}

// artifactsFor names the directory the reviewing job prepares its toolchain in.
func artifactsFor(tools string) string {
	return filepath.Join(filepath.Dir(tools), "caller", "artifacts")
}

func physical(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return path
	}
	return resolved
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func normalise(document string) string {
	var value any
	if err := json.Unmarshal([]byte(document), &value); err != nil {
		return strings.TrimSpace(document)
	}
	compact, err := json.Marshal(value)
	if err != nil {
		return strings.TrimSpace(document)
	}
	return string(compact)
}

// installFakeGH stands in for the gh CLI: it records every invocation and keeps
// the comments it was asked to post, so a test can tell whether a phase reached
// the remote repository and whether a rerun posted twice.
func installFakeGH(t *testing.T, directory, log string) {
	t.Helper()
	write(t, filepath.Join(directory, "gh"), `#!/usr/bin/env bash
set -euo pipefail
store="$FIXTURE_GH_LOG.comments"
case "$1" in
  api)
    printf 'GET %s\n' "$2" >> "$FIXTURE_GH_LOG"
    case "$2" in
      *"/pulls/"*) printf '%s' "$FIXTURE_PULL_JSON";;
      *"/comments?"*)
        if [ -f "$store" ]; then
          printf '['
          separator=""
          while IFS= read -r line; do
            printf '%s%s' "$separator" "$line"
            separator=","
          done < "$store"
          printf ']'
        else
          printf '[]'
        fi;;
      *"/comments")
        cat > "$FIXTURE_GH_LOG.body"
        # The stored body arrives without a trailing newline; add one so the list
        # can read it back as a line.
        cat "$FIXTURE_GH_LOG.body" >> "$store"
        printf '\n' >> "$store"
        printf 'POST %s\n' "$2" >> "$FIXTURE_GH_LOG"
        printf '{}';;
      *) printf '{}';;
    esac;;
  auth) printf 'auth %s\n' "$*" >> "$FIXTURE_GH_LOG";;
  *) printf '%s\n' "$*" >> "$FIXTURE_GH_LOG"; printf '[]';;
esac
`)
	if err := os.Chmod(filepath.Join(directory, "gh"), 0o755); err != nil {
		t.Fatal(err)
	}
}

// TestPublishRefusesDryRun guards the one flag that would otherwise be
// misleading: --dry-run is accepted by the installer commands and must never be
// silently ignored by a phase that writes a remote repository.
func TestPublishRefusesDryRun(t *testing.T) {
	repo, caller, tools, environment := phaseRepository(t)
	head := git(t, repo, "rev-parse", "HEAD")
	artifacts := filepath.Join(caller, "artifacts")
	if err := os.MkdirAll(artifacts, 0o755); err != nil {
		t.Fatal(err)
	}
	_ = tools
	ghDir := filepath.Join(caller, "gh")
	if err := os.MkdirAll(ghDir, 0o755); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(caller, "gh.log")
	pull := `{"state":"open","head":{"ref":"feature","sha":"` + head +
		`","repo":{"full_name":"fixture/repo"}},"base":{"ref":"main"}}`
	installFakeGH(t, ghDir, log)
	write(t, filepath.Join(artifacts, "report.md"), "a review that must not be published\n")
	write(t, filepath.Join(artifacts, "result.json"), `{"accepted":[],"unresolved":[]}`)

	job := environment.with(
		"PATH", ghDir+string(os.PathListSeparator)+environment.values["PATH"],
		"SKILL_PLAN", run(t, repo, environment, 0, "ci", "plan", "--base", head, "--head", head),
		"CHECKER_SOURCE", head,
		"GITHUB_REPOSITORY", "fixture/repo",
		"PR_NUMBER", "7",
		"GH_TOKEN", "fixture-token",
		"FIXTURE_GH_LOG", log,
		"FIXTURE_PULL_JSON", pull,
	)
	for _, phase := range [][]string{
		{"ci", "publish", artifacts},
		{"ci", "apply", artifacts},
		{"ci", "export", artifacts},
		{"ci", "prepare", artifacts},
		{"schedule", "publish", artifacts},
		{"schedule", "restore", artifacts},
	} {
		command := []string{"--dry-run"}
		command = append(command, phase...)
		stdout := run(t, repo, job, 1, command...)
		if stdout != "" {
			t.Fatalf("%v wrote to stdout under --dry-run: %s", phase, stdout)
		}
		if _, err := os.Stat(log); err == nil {
			t.Fatalf("%v contacted the remote repository under --dry-run", phase)
		}
	}
	// The stub publisher works, so the refusals above are not vacuous.
	run(t, repo, job, 0, "ci", "publish", artifacts)
	if _, err := os.Stat(log); err != nil {
		t.Fatal("the stub publisher was never reached without --dry-run")
	}
}
