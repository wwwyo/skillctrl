package scheduled_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/wwwyo/skillctrl/internal/adapt"
	"github.com/wwwyo/skillctrl/internal/gitx"
	"github.com/wwwyo/skillctrl/internal/lock"
	"github.com/wwwyo/skillctrl/internal/scheduled"
	"github.com/wwwyo/skillctrl/internal/upstream"
)

// fixture mirrors the scheduled-update repository: one registered skill with a
// saved intent, a relative Claude link, an accepted lock, and a test suite that
// the publication phase must run.
type fixture struct {
	t        *testing.T
	dir      string
	remote   string
	head     string
	changed  bool
	original string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	base := t.TempDir()
	f := &fixture{t: t, dir: filepath.Join(base, "repo"), remote: filepath.Join(base, "remote.git")}
	if err := os.MkdirAll(filepath.Join(f.dir, ".agents/skills/manual"), 0o755); err != nil {
		t.Fatal(err)
	}
	f.write(".agents/skills/manual/SKILL.md", "Original v1 with default browser customization.\n")
	if err := os.MkdirAll(filepath.Join(f.dir, ".claude/skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../../.agents/skills/manual", filepath.Join(f.dir, ".claude/skills/manual")); err != nil {
		t.Fatal(err)
	}
	f.write(".agents/skillctrl/intents/manual.md", "Use the default browser.\n")
	// The lock is written the way the importer writes it, so an unchanged
	// original really does produce no diff.
	registration, err := json.Marshal(struct {
		Version int            `json:"version"`
		Skills  map[string]any `json:"skills"`
	}{Version: 3, Skills: map[string]any{
		"manual": map[string]any{"source": "fixture/skills", "sourceType": "github",
			"skillFolderHash": strings.Repeat("0", 40)},
	}})
	if err != nil {
		t.Fatal(err)
	}
	f.write(".agents/.skill-lock.json", string(registration))
	f.write("tests/fixture.test.sh", "test \"${FIXTURE_TEST_FAILURE:-0}\" = 0\n")
	f.git("init", "-q", "-b", "main")
	f.git("config", "user.name", "Fixture")
	f.git("config", "user.email", "fixture@example.invalid")
	f.git("config", "commit.gpgsign", "false")
	f.git("add", "-A")
	f.write(lock.Lock, f.lockBytes())
	f.git("add", lock.Lock)
	f.git("commit", "-qm", "baseline")
	f.head = f.git("rev-parse", "HEAD")
	f.original = f.head
	f.git("init", "--bare", "-q", f.remote)
	f.git("remote", "add", "origin", f.remote)
	return f
}

func (f *fixture) git(args ...string) string {
	f.t.Helper()
	out, err := gitx.Output(f.dir, args...)
	if err != nil {
		f.t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}

func (f *fixture) write(path, content string) {
	f.t.Helper()
	full := filepath.Join(f.dir, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) read(path string) string {
	f.t.Helper()
	data, err := os.ReadFile(filepath.Join(f.dir, filepath.FromSlash(path)))
	if err != nil {
		f.t.Fatal(err)
	}
	return string(data)
}

func (f *fixture) lockBytes() string {
	f.t.Helper()
	tree := f.git("write-tree")
	value, err := lock.Snapshot(f.dir, tree)
	if err != nil {
		f.t.Fatal(err)
	}
	data, err := json.Marshal(value)
	if err != nil {
		f.t.Fatal(err)
	}
	return string(data)
}

func (f *fixture) accepted() map[string]string {
	f.t.Helper()
	var value struct {
		Skills map[string]string `json:"skills"`
	}
	if err := json.Unmarshal([]byte(f.read(lock.Lock)), &value); err != nil {
		f.t.Fatal(err)
	}
	return value.Skills
}

// installFakeImporter replaces the network fetch with a fixture that moves the
// original forward, so the scheduled flow is exercised without cloning anything.
// The override is undone when the test ends.
func (f *fixture) installFakeImporter() {
	f.t.Cleanup(func() { scheduled.Install = upstream.Install })
	{
		scheduled.Install = func(dir, command string, selected []string, identifier, directory string) (string, string, error) {
			if command != "update" {
				return "", "", fmt.Errorf("unexpected command %s", command)
			}
			target := filepath.Join(directory, "skills")
			if err := copyTree(filepath.Join(dir, ".agents/skills"), target); err != nil {
				return "", "", err
			}
			var metadata struct {
				Version int            `json:"version"`
				Skills  map[string]any `json:"skills"`
			}
			if err := json.Unmarshal([]byte(f.read(upstream.Lock)), &metadata); err != nil {
				return "", "", err
			}
			if f.changed {
				body := []byte("Original v2 generic browser.\n")
				if err := os.WriteFile(filepath.Join(target, "manual", "SKILL.md"), body, 0o644); err != nil {
					return "", "", err
				}
				blob := f.gitOut(dir, []string{"hash-object", "-w", "--stdin"}, body)
				tree := f.gitOut(dir, []string{"mktree"}, []byte("100644 blob "+blob+"\tSKILL.md\n"))
				entry, _ := metadata.Skills["manual"].(map[string]any)
				entry["skillFolderHash"] = tree
			}
			encoded, err := json.Marshal(metadata)
			if err != nil {
				return "", "", err
			}
			original := filepath.Join(directory, "original.json")
			if err := os.WriteFile(original, encoded, 0o644); err != nil {
				return "", "", err
			}
			return target, original, nil
		}
	}
}

func (f *fixture) gitOut(dir string, args []string, input []byte) string {
	f.t.Helper()
	command := exec.Command("git", args...)
	command.Dir = dir
	if input != nil {
		command.Stdin = bytes.NewReader(input)
	}
	out, err := command.CombinedOutput()
	if err != nil {
		f.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// ghFixture records the GitHub calls the update flow makes.
type ghFixture struct {
	existing  string
	branchSHA string
	created   string
	createdOK bool
	pushed    bool
}

func (g *ghFixture) JSON(args []string, input []byte) ([]byte, error) {
	switch {
	case args[0] == "pr" && args[1] == "list":
		if g.existing == "" {
			return []byte("[]"), nil
		}
		return []byte(fmt.Sprintf(`[{"headRefName":%q,"url":%q}]`,
			scheduled.BranchPrefix+"previous", g.existing)), nil
	case args[0] == "pr" && args[1] == "create":
		if !slices.Contains(args, "--draft") {
			return nil, fmt.Errorf("the update pull request was not a draft")
		}
		body := string(input)
		if !strings.Contains(body, "1 repository tests") && !strings.Contains(body, "all 1 repository tests") {
			return nil, fmt.Errorf("the pull request body does not report verification: %q", body)
		}
		g.createdOK = true
		g.created = g.created + "|created"
		return []byte("https://example.invalid/update\n"), nil
	case strings.Contains(args[1], "/git/ref/heads/"):
		return []byte(fmt.Sprintf(`{"object":{"sha":%q}}`, g.branchSHA)), nil
	}
	return nil, fmt.Errorf("unexpected gh invocation: %v", args)
}

func (g *ghFixture) Run(args []string, input []byte) ([]byte, error) { return g.JSON(args, input) }

func (g *ghFixture) SetupGit() error { return nil }

// TestPrepareAndRestoreProduceAVerifiedInput walks the whole scheduled flow:
// an unchanged original produces no work, a changed original produces an
// immutable input, and a tampered input is refused in the second job.
func TestPrepareAndRestoreProduceAVerifiedInput(t *testing.T) {
	f := newFixture(t)
	f.installFakeImporter()
	gh := &ghFixture{branchSHA: f.head}
	directory := filepath.Join(t.TempDir(), "input")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}

	t.Run("an unchanged original produces nothing to publish", func(t *testing.T) {
		result, err := scheduled.Prepare(f.dir, directory, "fixture/skills", gh)
		if err != nil {
			t.Fatal(err)
		}
		if result.Changed {
			t.Fatalf("an unchanged original reported work: %+v", result)
		}
		if result.Plan.Head != f.head {
			t.Fatalf("plan head moved: %s", result.Plan.Head)
		}
		if _, err := os.Stat(filepath.Join(directory, "input.bundle")); err == nil {
			t.Fatal("an unchanged original produced an input bundle")
		}
		if body := f.read(".agents/skills/manual/SKILL.md"); !strings.Contains(body, "customization") {
			t.Fatalf("the original import lost the customization: %q", body)
		}
	})

	f.changed = true
	t.Run("a changed original is validated before it becomes input", func(t *testing.T) {
		f.git("reset", "--hard", f.head)
		result, err := scheduled.Prepare(f.dir, directory, "fixture/skills", gh)
		if err != nil {
			t.Fatal(err)
		}
		if !result.Changed {
			t.Fatal("a changed original reported no work")
		}
		if len(result.Plan.ReviewSkills) != 1 || result.Plan.ReviewSkills[0] != "manual" {
			t.Fatalf("unexpected review selection: %v", result.Plan.ReviewSkills)
		}
		if result.Plan.Head == f.head || !result.Plan.LockChanged {
			t.Fatalf("unexpected plan: %+v", result.Plan)
		}
		if f.accepted()["manual"] == "" {
			t.Fatal("the accepted lock lost its entry")
		}
		if _, err := os.Stat(filepath.Join(directory, "input.bundle")); err != nil {
			t.Fatalf("no input bundle was produced: %v", err)
		}
		f.head = result.Plan.Head
	})

	t.Run("the input restores only when it matches its plan", func(t *testing.T) {
		f.git("reset", "--hard", f.original)
		restored, err := scheduled.Restore(f.dir, directory, f.original)
		if err != nil {
			t.Fatal(err)
		}
		if restored.Head != f.head {
			t.Fatalf("restored head %s want %s", restored.Head, f.head)
		}
		// A plan that does not match the trees must not restore, or a tampered
		// artifact could decide what gets reviewed.
		supplied := readPlan(t, filepath.Join(directory, "plan.json"))
		supplied.InputTrees["manual"] = strings.Repeat("f", 40)
		writePlan(t, filepath.Join(directory, "plan.json"), supplied)
		f.git("reset", "--hard", f.original)
		if _, err := scheduled.Restore(f.dir, directory, f.original); err == nil ||
			!strings.Contains(err.Error(), "differs from its Git trees") {
			t.Fatalf("a forged plan was accepted: %v", err)
		}
		if f.git("rev-parse", "HEAD") != f.original {
			t.Fatal("a refused restore moved the checkout")
		}
	})
}

// TestValidateImportRefusesAnythingButOriginals is the guard that keeps a
// scheduled update from smuggling unrelated changes into a pull request.
func TestValidateImportRefusesAnythingButOriginals(t *testing.T) {
	t.Run("an unregistered skill change", func(t *testing.T) {
		f := newFixture(t)
		f.write(".agents/skills/manual/SKILL.md", "Unregistered original change.\n")
		f.git("add", ".agents/skills")
		err := scheduled.ValidateImport(f.dir, f.head, f.git("write-tree"))
		if err == nil || !strings.Contains(err.Error(), "unchanged tree hash") {
			t.Fatalf("unregistered change was accepted: %v", err)
		}
	})
	t.Run("a changed registration", func(t *testing.T) {
		f := newFixture(t)
		f.write(".agents/.skill-lock.json",
			`{"version": 3, "skills": {"manual": {"source": "other/repo", "sourceType": "github",
			  "skillFolderHash": "`+strings.Repeat("0", 40)+`"}}}`)
		f.git("add", "-A")
		err := scheduled.ValidateImport(f.dir, f.head, f.git("write-tree"))
		if err == nil || !strings.Contains(err.Error(), "upstream identity") {
			t.Fatalf("changed registration was accepted: %v", err)
		}
	})
	t.Run("a broken Claude link", func(t *testing.T) {
		f := newFixture(t)
		if err := os.Remove(filepath.Join(f.dir, ".claude/skills/manual")); err != nil {
			t.Fatal(err)
		}
		f.git("add", "-A")
		err := scheduled.ValidateImport(f.dir, f.head, f.git("write-tree"))
		if err == nil || !strings.Contains(err.Error(), "unauthorized path") {
			t.Fatalf("a removed link was accepted: %v", err)
		}
	})
	t.Run("an added registration", func(t *testing.T) {
		f := newFixture(t)
		f.write(".agents/.skill-lock.json", `{"version": 3, "skills": {
			"manual": {"source": "fixture/skills", "sourceType": "github", "skillFolderHash": "`+strings.Repeat("0", 40)+`"},
			"extra": {"source": "fixture/skills", "sourceType": "github", "skillFolderHash": "`+strings.Repeat("0", 40)+`"}}}`)
		f.git("add", "-A")
		err := scheduled.ValidateImport(f.dir, f.head, f.git("write-tree"))
		if err == nil || !strings.Contains(err.Error(), "cannot add or remove") {
			t.Fatalf("an added registration was accepted: %v", err)
		}
	})
}

// TestPublishRunsVerificationBeforeOpeningAPullRequest is the publication gate:
// a failing repository test, a moved default branch, and an existing update must
// each stop the run before anything is pushed.
func TestPublishRunsVerificationBeforeOpeningAPullRequest(t *testing.T) {
	f := newFixture(t)
	f.installFakeImporter()
	directory := filepath.Join(t.TempDir(), "input")
	result, err := scheduled.Prepare(f.dir, directory, "fixture/skills", &ghFixture{branchSHA: f.head})
	if err != nil {
		t.Fatal(err)
	}
	f.changed = true
	result, err = scheduled.Prepare(f.dir, directory, "fixture/skills", &ghFixture{branchSHA: f.head})
	if err != nil {
		t.Fatal(err)
	}
	plan := result.Plan
	review := filepath.Join(t.TempDir(), "review")
	if err := os.MkdirAll(review, 0o755); err != nil {
		t.Fatal(err)
	}
	repair := func(t *testing.T) {
		f.git("reset", "--hard", plan.Head)
		body := filepath.Join(f.dir, ".agents/skills/manual/SKILL.md")
		if err := os.WriteFile(body, []byte("Original v2 with default browser customization.\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(review, adapt.ResultFile),
			[]byte(`{"accepted":["manual"],"unresolved":[]}`), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(review, adapt.ReportFile), []byte("re-adapted.\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := adapt.Export(f.dir, plan, review, "fixture-credential"); err != nil {
			t.Fatal(err)
		}
		f.git("reset", "--hard", plan.Head)
		if err := adapt.Apply(f.dir, plan, review); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("a failing repository test stops publication", func(t *testing.T) {
		repair(t)
		t.Setenv("FIXTURE_TEST_FAILURE", "1")
		gh := &ghFixture{branchSHA: plan.Base}
		if _, err := scheduled.Publish(f.dir, review, plan, f.environment(), gh); err == nil {
			t.Fatal("a failing test suite did not stop publication")
		}
		if gh.createdOK {
			t.Fatal("a failing test suite still opened a pull request")
		}
		if branches := f.git("ls-remote", "origin", "refs/heads/"+scheduled.BranchPrefix+"*"); branches != "" {
			t.Fatalf("a failing test suite still pushed: %s", branches)
		}
	})

	t.Run("a moved default branch stops publication", func(t *testing.T) {
		repair(t)
		gh := &ghFixture{branchSHA: strings.Repeat("f", 40)}
		if _, err := scheduled.Publish(f.dir, review, plan, f.environment(), gh); err == nil ||
			!strings.Contains(err.Error(), "default branch advanced") {
			t.Fatalf("a moved default branch was accepted: %v", err)
		}
		if branches := f.git("ls-remote", "origin", "refs/heads/"+scheduled.BranchPrefix+"*"); branches != "" {
			t.Fatalf("a moved default branch still pushed: %s", branches)
		}
	})

	t.Run("a verified update opens one draft pull request", func(t *testing.T) {
		repair(t)
		gh := &ghFixture{branchSHA: plan.Base}
		out, err := scheduled.Publish(f.dir, review, plan, f.environment(), gh)
		if err != nil {
			t.Fatal(err)
		}
		if out["tests"] != 1 {
			t.Fatalf("unexpected result: %v", out)
		}
		if !gh.createdOK {
			t.Fatal("no pull request was created")
		}
		if branches := f.git("ls-remote", "origin", "refs/heads/"+scheduled.BranchPrefix+"*"); branches == "" {
			t.Fatal("the update branch was not pushed")
		}
		if plan2, err := lock.Compare(f.dir, plan.Base, "HEAD", ""); err != nil {
			t.Fatal(err)
		} else if plan2.LockChanged {
			t.Fatal("a verified update left the lock unresolved")
		}
	})

	t.Run("an unresolved skill keeps the update unresolved", func(t *testing.T) {
		f.git("reset", "--hard", plan.Head)
		// The artifact from the previous repair describes a different review.
		if err := os.WriteFile(filepath.Join(review, adapt.PatchFile), nil, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(review, adapt.ResultFile),
			[]byte(`{"accepted":[],"unresolved":["manual"]}`), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := adapt.Apply(f.dir, plan, review); err != nil {
			t.Fatal(err)
		}
		gh := &ghFixture{branchSHA: plan.Base, existing: "https://example.invalid/existing"}
		out, err := scheduled.Publish(f.dir, review, plan, f.environment(), gh)
		if err != nil {
			t.Fatal(err)
		}
		if out["existing_pr"] == nil {
			t.Fatalf("an existing update was not reused: %v", out)
		}
		state, err := lock.Compare(f.dir, plan.Base, "HEAD", "")
		if err != nil {
			t.Fatal(err)
		}
		if !state.LockChanged {
			t.Fatal("an unresolved skill was accepted")
		}
	})
}

func (f *fixture) environment() map[string]string {
	return map[string]string{
		"GITHUB_REPOSITORY":  "fixture/skills",
		"DEFAULT_BRANCH":     "main",
		"GITHUB_RUN_ID":      "123",
		"GITHUB_RUN_ATTEMPT": "1",
	}
}

func readPlan(t *testing.T, path string) lock.Plan {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var plan lock.Plan
	if err := json.Unmarshal(data, &plan); err != nil {
		t.Fatal(err)
	}
	return plan
}

func writePlan(t *testing.T, path string, plan lock.Plan) {
	t.Helper()
	data, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func copyTree(source, destination string) error {
	return filepath.Walk(source, func(current string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, current)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(current)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode().Perm())
	})
}
