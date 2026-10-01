package adapt_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wwwyo/skillctrl/internal/adapt"
	"github.com/wwwyo/skillctrl/internal/lock"
	"github.com/wwwyo/skillctrl/internal/toolchain"
)

// reviewFixture separates the three directories a review must never confuse:
// the directory the caller happens to stand in, the checkout under review, and
// the prepared directory holding the trusted pins.
type reviewFixture struct {
	repo      *repo
	caller    string
	artifacts string
	trusted   string
	tools     string
	log       string
}

func newReviewFixture(t *testing.T) *reviewFixture {
	t.Helper()
	r := newRepo(t)
	f := &reviewFixture{
		repo:      r,
		caller:    t.TempDir(),
		artifacts: t.TempDir(),
		trusted:   t.TempDir(),
		tools:     t.TempDir(),
		log:       filepath.Join(t.TempDir(), "calls.log"),
	}
	// The caller's configuration would put a hostile tool on PATH if it were
	// ever evaluated, and so would the configuration in the checkout.
	hostile := "[tools]\n\"npm:hostile-package\" = \"9.9.9\"\n"
	for _, directory := range []string{f.caller, r.dir} {
		if err := os.WriteFile(filepath.Join(directory, "mise.toml"), []byte(hostile), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", f.tools+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FIXTURE_CALL_LOG", f.log)
	// Temporary directories live behind a symlink on macOS; the shell reports the
	// physical path, so the expectations must too.
	f.artifacts = physical(t, f.artifacts)
	f.caller = physical(t, f.caller)
	r.dir = physical(t, r.dir)
	t.Setenv("FIXTURE_TRUSTED_DIR", f.artifacts)
	t.Setenv("FIXTURE_TARGET_DIR", r.dir)
	t.Setenv("FIXTURE_TOOLS_DIR", f.tools)
	return f
}

// mise writes the trusted environment: a PATH that contains only the trusted
// tools directory, plus a credential the reviewer never inherits from the
// caller. It refuses to run anywhere but the prepared directory.
func (f *reviewFixture) mise(t *testing.T) {
	t.Helper()
	writeScript(t, filepath.Join(f.tools, "mise"), `#!/usr/bin/env bash
set -euo pipefail
pwd >> "$FIXTURE_CALL_LOG"
[ "$PWD" = "$FIXTURE_TRUSTED_DIR" ] || { echo "mise resolved in $PWD" >&2; exit 1; }
printf '{"PATH":"%s","OPENCODE_API_KEY":"%s","TRUSTED_MARKER":"trusted"}' "$FIXTURE_TOOLS_DIR" "$FIXTURE_SECRET"
`)
}

func (f *reviewFixture) reviewer(t *testing.T, body string) {
	t.Helper()
	writeScript(t, filepath.Join(f.tools, "pi"), `#!/usr/bin/env bash
set -euo pipefail
pwd >> "$FIXTURE_CALL_LOG"
[ "$PWD" = "$FIXTURE_TARGET_DIR" ] || { echo "reviewer ran in $PWD" >&2; exit 1; }
[ "${PATH:-}" = "$FIXTURE_TOOLS_DIR" ] || { echo "inherited PATH leaked into the reviewer: $PATH" >&2; exit 1; }
[ "${TRUSTED_MARKER:-}" = trusted ] || { echo "trusted toolchain was not applied" >&2; exit 1; }
case ":${PATH}:" in
  *hostile-package*|*incoming-package*) echo "untrusted tools reached the reviewer" >&2; exit 1;;
esac
case " $* " in
  *" --no-context-files "*) ;;
  *) echo 'reviewer ran with repository context files' >&2; exit 1;;
esac
case "$*" in
  *"--model opencode-go/space-bunny-free"*) ;;
  *) echo 'reviewer ran with an unexpected model' >&2; exit 1;;
esac
printf '%s' "$PWD" > "$FIXTURE_CALL_LOG.reviewer"
`+body)
}

// run performs the review from the caller's directory, which is where a user
// would realistically be standing when they invoke the tool.
func (f *reviewFixture) run(t *testing.T, plan lock.Plan) {
	t.Helper()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(f.caller); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(previous)
	if err := adapt.ReviewLocal(adapt.Options{
		Dir: f.repo.dir, Plan: plan, Directory: f.artifacts, Source: plan.Head,
		Prompt: adapt.Prompt, ConfigPath: toolchain.DefaultConfig,
		ModelsPath: toolchain.DefaultModels,
	}); err != nil {
		t.Fatalf("review failed: %v\n%s", err, readFile(t, f.log))
	}
}

func (f *reviewFixture) calls(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile(f.log)
	if err != nil {
		t.Fatalf("no tool invocations were recorded: %v", err)
	}
	return strings.Split(strings.TrimSpace(string(data)), "\n")
}

// TestReviewLocalUsesOnlyTheTrustedToolchain pins the three boundaries with a
// caller directory, a checkout directory, and a prepared directory that each
// hold different configuration.
func TestReviewLocalUsesOnlyTheTrustedToolchain(t *testing.T) {
	f := newReviewFixture(t)
	f.mise(t)
	f.reviewer(t, `
body=".agents/skills/manual/SKILL.md"
printf 'repaired body\n' > "$body"
result="${@: -1}"; result="${result##*Write completion JSON to: }"
printf '{"accepted":["manual"],"unresolved":[]}' > "$result"
`)
	t.Setenv("FIXTURE_SECRET", "fixture-trusted-credential")

	plan := f.repo.plan()
	f.run(t, plan)

	calls := f.calls(t)
	if len(calls) != 2 {
		t.Fatalf("unexpected tool invocations: %v", calls)
	}
	if calls[0] != f.artifacts {
		t.Fatalf("the toolchain was resolved in %s, not in the prepared directory", calls[0])
	}
	if calls[1] != f.repo.dir {
		t.Fatalf("the reviewer ran in %s, not in the checkout under review", calls[1])
	}
	if got := physical(t, readFile(t, f.log+".reviewer")); got != f.repo.dir {
		t.Fatalf("the reviewer edited %s", got)
	}
	if body := readFile(t, filepath.Join(f.repo.dir, ".agents/skills/manual/SKILL.md")); body != "repaired body\n" {
		t.Fatalf("the reviewer did not edit the checkout under review: %q", body)
	}
	if _, err := os.Stat(filepath.Join(f.artifacts, adapt.PatchFile)); err != nil {
		t.Fatalf("no artifact was exported: %v", err)
	}
}

// TestReviewerIsNotInheritedFromTheCallerPath proves the executable comes from
// the trusted PATH: the reviewer exists only in the trusted tools directory,
// which is deliberately absent from the environment the test runs in.
func TestReviewerIsNotInheritedFromTheCallerPath(t *testing.T) {
	f := newReviewFixture(t)
	f.mise(t)
	f.reviewer(t, `
printf 'repaired body\n' > .agents/skills/manual/SKILL.md
result="${@: -1}"; result="${result##*Write completion JSON to: }"
printf '{"accepted":["manual"],"unresolved":[]}' > "$result"
`)
	t.Setenv("FIXTURE_SECRET", "fixture-trusted-credential")
	// The caller's PATH contains the tools this test itself needs, but no
	// reviewer: only the trusted configuration can supply one.
	callerTools := t.TempDir()
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(git, filepath.Join(callerTools, "git")); err != nil {
		t.Fatal(err)
	}
	// The resolver is reachable, the reviewer is not.
	resolver, err := os.ReadFile(filepath.Join(f.tools, "mise"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(callerTools, "mise"), resolver, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", callerTools)
	f.run(t, f.repo.plan())
}

// TestCredentialFromTheTrustedEnvironmentIsScreened keeps a credential that the
// caller never had from reaching an exported artifact: the secret to check is
// the one the reviewer actually received.
func TestCredentialFromTheTrustedEnvironmentIsScreened(t *testing.T) {
	for _, leak := range []string{"body", "binary"} {
		t.Run(leak, func(t *testing.T) {
			f := newReviewFixture(t)
			f.mise(t)
			body := `
printf '%s\n' "$OPENCODE_API_KEY" > .agents/skills/manual/SKILL.md
result="${@: -1}"; result="${result##*Write completion JSON to: }"
printf '{"accepted":["manual"],"unresolved":[]}' > "$result"
`
			if leak == "binary" {
				body = `
printf '\0%s' "$OPENCODE_API_KEY" > .agents/skills/manual/blob.bin
printf 'repaired\n' > .agents/skills/manual/SKILL.md
result="${@: -1}"; result="${result##*Write completion JSON to: }"
printf '{"accepted":["manual"],"unresolved":[]}' > "$result"
`
			}
			f.reviewer(t, body)
			const secret = "fixture-trusted-credential"
			t.Setenv("FIXTURE_SECRET", secret)
			// The caller holds no credential of its own.
			t.Setenv("OPENCODE_API_KEY", "")
			plan := f.repo.plan()
			err := adapt.ReviewLocal(adapt.Options{
				Dir: f.repo.dir, Plan: plan, Directory: f.artifacts, Source: plan.Head,
				Prompt: adapt.Prompt, ConfigPath: toolchain.DefaultConfig,
				ModelsPath: toolchain.DefaultModels,
			})
			if err == nil {
				t.Fatal("a credential supplied by the trusted toolchain was exported")
			}
			if _, statErr := os.Stat(filepath.Join(f.artifacts, adapt.PatchFile)); statErr == nil {
				t.Fatal("a refused export left a patch behind")
			}
		})
	}
}

// TestReviewLocalRequiresATrustedToolchain refuses to review at all when the
// trusted configuration cannot be resolved, rather than falling back to
// whatever the caller's environment provides.
func TestReviewLocalRequiresATrustedToolchain(t *testing.T) {
	r := newRepo(t)
	tools := t.TempDir()
	writeScript(t, filepath.Join(tools, "mise"), "#!/usr/bin/env bash\nexit 1\n")
	writeScript(t, filepath.Join(tools, "pi"), "#!/usr/bin/env bash\nexit 1\n")
	t.Setenv("OPENCODE_API_KEY", "")
	t.Setenv("PATH", tools+string(os.PathListSeparator)+os.Getenv("PATH"))

	plan := r.plan()
	err := adapt.ReviewLocal(adapt.Options{
		Dir: r.dir, Plan: plan, Directory: t.TempDir(), Source: plan.Head,
		Prompt: adapt.Prompt, ConfigPath: toolchain.DefaultConfig,
		ModelsPath: toolchain.DefaultModels,
	})
	if err == nil {
		t.Fatal("a review ran without a trusted toolchain")
	}
	if !strings.Contains(err.Error(), "trusted toolchain") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// writeScript writes an executable stub with an absolute interpreter path,
// because these tests deliberately replace PATH and a script that resolves its
// interpreter through PATH would fail for an unrelated reason.
func physical(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return path
	}
	return resolved
}

func writeScript(t *testing.T, path, body string) {
	t.Helper()
	shell, err := exec.LookPath("bash")
	if err != nil {
		shell = "/bin/sh"
	}
	body = strings.Replace(body, "#!/usr/bin/env bash", "#!"+shell, 1)
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}
