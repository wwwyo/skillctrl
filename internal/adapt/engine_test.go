package adapt_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/wwwyo/skillctrl/internal/adapt"
)

var (
	buildOnce sync.Once
	binary    string
)

func binaryPath(t *testing.T) string {
	t.Helper()
	buildOnce.Do(func() {
		directory, err := os.MkdirTemp("", "skillctrl-bin-")
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

// reviewer is a stand-in for the reviewing agent. It asserts how it was invoked,
// repairs the selected skill, and optionally leaks a credential so the export
// guard can be exercised end to end.
const reviewer = `#!/usr/bin/env bash
set -euo pipefail
[ "${PI_CODING_AGENT_DIR:-}" != "" ] || { echo 'reviewer ran without an isolated configuration directory' >&2; exit 1; }
case " $* " in
  *" --no-context-files "*) ;;
  *) echo 'reviewer ran with repository context files' >&2; exit 1;;
esac
case " $* " in
  *" --no-extensions "*) ;;
  *) echo 'reviewer ran with extensions enabled' >&2; exit 1;;
esac
case "$*" in
  *"--model opencode-go/space-bunny-free"*) ;;
  *) echo 'reviewer ran with an unexpected model' >&2; exit 1;;
esac
body=".agents/skills/manual/SKILL.md"
leak="@LEAK@"
case "$leak" in
  body) printf '%s\n' "$OPENCODE_API_KEY" > "$body";;
  binary) printf '\0%s' "$OPENCODE_API_KEY" > .agents/skills/manual/blob.bin;;
  report) printf 'leaked %s\n' "$OPENCODE_API_KEY";;
  result) result="${@: -1}"; result="${result##*Write completion JSON to: }"; printf '%s' "$OPENCODE_API_KEY" > "$result"; exit 0;;
  *) printf 'repaired body\n' > "$body";;
esac
result="${@: -1}"
result="${result##*Write completion JSON to: }"
printf '{"accepted":["manual"],"unresolved":[]}' > "$result"
echo 'default browser criterion repaired'
`

// TestEngineHandoffExportsOnlySelectedSkills runs the sandbox contract end to
// end: restored merge context must not become a repair input, the reviewer must
// be isolated, and a credential that reaches any artifact must stop the export.
func TestEngineHandoffExportsOnlySelectedSkills(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed; the sandbox handoff cannot be exercised")
	}
	r := newRepo(t)
	directory := t.TempDir()
	plan := r.plan()
	r.write(".agents/skills/manual/SKILL.md", "repaired body\n")

	// The sandbox restored merge-only content and another skill's change.
	r.write(".agents/skills/other/SKILL.md", "independent base change\n")
	restored := filepath.Join(r.dir, ".agents/restored-context.md")
	if err := os.WriteFile(restored, []byte("framework merge-only context\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The reviewer lives outside the checkout: an extra file inside it would be
	// a genuine working-tree change and the export would (correctly) refuse it.
	reviewerPath := filepath.Join(t.TempDir(), "reviewer")
	if err := os.WriteFile(reviewerPath, []byte(reviewer), 0o755); err != nil {
		t.Fatal(err)
	}
	prompt := filepath.Join(t.TempDir(), "prompt.md")
	if err := os.WriteFile(prompt, []byte("Review the current customization criteria.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(directory, "agent")
	if err := os.MkdirAll(state, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, "models.json"), []byte(`{"trusted": true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	engine := filepath.Join(directory, "engine.cjs")
	script, err := os.ReadFile("testdata/engine.cjs")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(engine, script, 0o644); err != nil {
		t.Fatal(err)
	}
	run := func(directory string, leak string) (string, error) {
		reviewerWithLeak := strings.ReplaceAll(reviewer, "@LEAK@", leak)
		planPath := filepath.Join(directory, "plan.json")
		if err := os.WriteFile(planPath, mustMarshal(plan), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(directory, "agent"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, "agent", "models.json"),
			[]byte(`{"trusted": true}`), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(reviewerPath, []byte(reviewerWithLeak), 0o755); err != nil {
			t.Fatal(err)
		}
		command := exec.Command(node, engine, directory, binaryPath(t), reviewerPath)
		command.Dir = r.dir
		command.Env = append(os.Environ(),
			"PATH="+filepath.Dir(reviewerPath)+string(os.PathListSeparator)+os.Getenv("PATH"),
			"SKILL_MODEL=opencode-go/space-bunny-free",
			"REVIEW_PROMPT="+prompt,
			"REVIEW_DIR="+r.dir,
			"PI_CODING_AGENT_DIR="+filepath.Join(directory, "agent"),
			"OPENCODE_API_KEY=fixture-inference-credential-never-publish",
			"SKILL_PLAN="+string(mustMarshal(plan)),
		)
		out, err := command.CombinedOutput()
		return string(out), err
	}

	out, err := run(directory, "")
	if err != nil {
		t.Fatalf("engine handoff failed: %v\n%s", err, out)
	}
	if got := readFile(t, filepath.Join(r.dir, ".agents/skills/other/SKILL.md")); got != "old body\n" {
		t.Fatalf("restored merge content was used as a repair input: %q", got)
	}
	if _, err := os.Stat(restored); err == nil {
		t.Fatal("restored merge-only content survived the handoff")
	}
	patch, err := os.ReadFile(filepath.Join(directory, adapt.PatchFile))
	if err != nil {
		t.Fatalf("no patch was exported: %v\n%s", err, out)
	}
	if !strings.Contains(string(patch), "repaired body") {
		t.Fatalf("exported patch is empty: %q", patch)
	}
	report, err := os.ReadFile(filepath.Join(directory, adapt.ReportFile))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(report), "criterion repaired") {
		t.Fatalf("reviewer output was not captured: %q", report)
	}

	for _, leak := range []string{"body", "report", "binary", "result"} {
		t.Run("leak via "+leak, func(t *testing.T) {
			failed := t.TempDir()
			out, err := run(failed, leak)
			if err == nil {
				t.Fatalf("a leaked credential was exported: %s", out)
			}
			if _, statErr := os.Stat(filepath.Join(failed, adapt.PatchFile)); statErr == nil {
				t.Fatal("a refused export left a patch behind")
			}
			if strings.Contains(out, "fixture-inference-credential-never-publish") {
				t.Fatal("the credential appeared in the output")
			}
			r.reset()
		})
	}
}

func mustMarshal(value any) []byte {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return data
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
