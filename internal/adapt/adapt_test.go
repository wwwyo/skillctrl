package adapt_test

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wwwyo/skillctrl/internal/adapt"
	"github.com/wwwyo/skillctrl/internal/lock"
	"github.com/wwwyo/skillctrl/internal/toolchain"
)

type repo struct {
	t         *testing.T
	dir       string
	bin       string
	artifacts string
}

func newRepo(t *testing.T) *repo {
	t.Helper()
	dir := t.TempDir()
	r := &repo{t: t, dir: dir, bin: filepath.Join(dir, "bin")}
	if err := os.MkdirAll(filepath.Join(dir, ".agents/skills/manual"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".agents/skills/other"), 0o755); err != nil {
		t.Fatal(err)
	}
	r.write(".agents/skills/manual/SKILL.md", "old body\n")
	r.write(".agents/skills/other/SKILL.md", "old body\n")
	r.write(".agents/skillctrl/intents/manual.md", "use the default browser\n")
	r.write("home/dot_pi/agent/models.json", `{"trusted": true}`)
	r.write("home/dot_config/mise/config.toml", `[tools]
node = "1.2.3"
"npm:@earendil-works/pi-coding-agent" = "4.5.6"
unrelated = "7.8.9"
[settings]
pin = true
minimum_release_age = "7d"
[env]
UNRELATED_SECRET = "fixture-only"
`)
	r.git("init", "-q")
	r.git("config", "user.name", "Fixture")
	r.git("config", "user.email", "fixture@example.invalid")
	r.git("config", "commit.gpgsign", "false")
	r.git("add", "-A")
	r.git("commit", "-qm", "fixture")
	return r
}

func (r *repo) git(args ...string) string {
	r.t.Helper()
	command := exec.Command("git", args...)
	command.Dir = r.dir
	command.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1")
	out, err := command.CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func (r *repo) write(path, content string) {
	r.t.Helper()
	full := filepath.Join(r.dir, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		r.t.Fatal(err)
	}
}

func (r *repo) read(path string) string {
	r.t.Helper()
	data, err := os.ReadFile(filepath.Join(r.dir, filepath.FromSlash(path)))
	if err != nil {
		r.t.Fatal(err)
	}
	return string(data)
}

func (r *repo) reset() {
	r.t.Helper()
	r.git("reset", "--hard", r.head())
}

func (r *repo) head() string { return r.git("rev-parse", "HEAD") }

// staged writes content to a path and stages it, returning the patch.
func (r *repo) stage(paths ...string) []byte {
	r.t.Helper()
	for _, path := range paths {
		r.write(path, "changed\n")
	}
	arguments := append([]string{"add", "--"}, paths...)
	r.git(arguments...)
	command := exec.Command("git", "diff", "--cached", "--binary")
	command.Dir = r.dir
	out, err := command.Output()
	if err != nil {
		r.t.Fatal(err)
	}
	return out
}

func (r *repo) patchFromIndex() []byte {
	r.t.Helper()
	command := exec.Command("git", "diff", "--cached", "--binary")
	command.Dir = r.dir
	out, err := command.Output()
	if err != nil {
		r.t.Fatal(err)
	}
	return out
}

func (r *repo) plan() lock.Plan {
	r.t.Helper()
	head := r.head()
	value, err := lock.Snapshot(r.dir, head)
	if err != nil {
		r.t.Fatal(err)
	}
	plan := lock.Select(value, lock.Empty(), map[string]bool{"manual": true})
	plan.Base = head
	plan.Head = head
	plan.Comparison = head
	return plan
}

// results writes the review artifacts a reviewer would leave behind.
func (r *repo) results(directory string, accepted, unresolved []string, report string) {
	r.t.Helper()
	body, _ := json.Marshal(map[string]any{"accepted": accepted, "unresolved": unresolved})
	if err := os.WriteFile(filepath.Join(directory, adapt.ResultFile), body, 0o644); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, adapt.ReportFile), []byte(report), 0o644); err != nil {
		r.t.Fatal(err)
	}
}

func (r *repo) patch(directory string, content []byte) {
	r.t.Helper()
	if err := os.WriteFile(filepath.Join(directory, adapt.PatchFile), content, 0o644); err != nil {
		r.t.Fatal(err)
	}
}

// TestPrepareUsesOnlyTrustedPins is the trusted-checker rule: the reviewing job
// takes the Node and agent pins and the release policy from the trusted commit,
// and nothing else from the configuration - not the unrelated tools and not the
// encrypted environment.
func TestPrepareUsesOnlyTrustedPins(t *testing.T) {
	r := newRepo(t)
	directory := filepath.Join(r.dir, "review")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	plan := r.plan()
	// The incoming checkout carries an untrusted value for the same file.
	r.write("home/dot_config/mise/config.toml", "[tools]\nnode = \"incoming-untrusted-version\"\n")
	if err := adapt.Prepare(r.dir, plan, plan.Head, directory); err != nil {
		t.Fatal(err)
	}
	got := r.read("review/mise.toml")
	for _, want := range []string{`"node" = "1.2.3"`, `"npm:@earendil-works/pi-coding-agent" = "4.5.6"`,
		"pin = true", `minimum_release_age = "7d"`} {
		if !strings.Contains(got, want) {
			t.Fatalf("prepared configuration is missing %s:\n%s", want, got)
		}
	}
	for _, unwanted := range []string{"unrelated", "incoming-untrusted-version", "UNRELATED_SECRET", "[env]"} {
		if strings.Contains(got, unwanted) {
			t.Fatalf("prepared configuration leaked %s:\n%s", unwanted, got)
		}
	}
}

// TestPrepareRefusesHeadDrift keeps a review bound to the head that was selected
// before any inference ran.
func TestPrepareRefusesHeadDrift(t *testing.T) {
	r := newRepo(t)
	directory := t.TempDir()
	plan := r.plan()
	r.write(".agents/skills/other/SKILL.md", "drift\n")
	r.git("add", "-A")
	r.git("commit", "-qm", "drift")
	if err := adapt.Prepare(r.dir, plan, plan.Head, directory); err == nil {
		t.Fatal("a moved HEAD was accepted")
	}
}

// TestApplyRecordsFromTheIndexOnly is the core CI guarantee: the accepted hash
// comes from the validated index, and the caller's working tree keeps showing
// the reviewed edit as an unstaged change.
func TestApplyRecordsFromTheIndexOnly(t *testing.T) {
	r := newRepo(t)
	directory := t.TempDir()
	plan := r.plan()
	r.results(directory, []string{"manual"}, nil, "criterion checked")

	if err := adapt.Apply(r.dir, plan, directory); err == nil {
		t.Fatal("apply accepted a missing artifact")
	}

	r.patch(directory, r.stage(".agents/skills/manual/SKILL.md"))
	r.reset()
	if err := adapt.Apply(r.dir, plan, directory); err != nil {
		t.Fatal(err)
	}
	if got := r.git("show", ":.agents/skills/manual/SKILL.md"); got != "changed" {
		t.Fatalf("the patch was not applied to the index: %q", got)
	}
	if got := r.read(".agents/skills/manual/SKILL.md"); got != "old body\n" {
		t.Fatalf("the working tree was modified: %q", got)
	}
	tree := r.git("write-tree")
	stored, err := lock.Snapshot(r.dir, tree)
	if err != nil {
		t.Fatal(err)
	}
	var accepted struct {
		Skills map[string]string `json:"skills"`
	}
	if err := json.Unmarshal([]byte(r.read(lock.Lock)), &accepted); err != nil {
		t.Fatal(err)
	}
	if accepted.Skills["manual"] != stored.Skills["manual"] {
		t.Fatal("the accepted hash does not match the validated index")
	}
	r.reset()

	// An empty patch is valid and simply records the review as unresolved later.
	r.patch(directory, nil)
	if err := adapt.Apply(r.dir, plan, directory); err != nil {
		t.Fatal(err)
	}
	r.reset()

	// Head drift is refused even with a valid artifact.
	drifted := r.plan()
	drifted.Head = strings.Repeat("0", 40)
	if err := adapt.Apply(r.dir, drifted, directory); err == nil {
		t.Fatal("apply accepted a plan for a different head")
	}
}

// TestApplyRefusesEditsOutsideTheSelection covers the scope rule from both
// sides: an intent file and an unrelated skill are refused, because a reviewer
// that can edit them can change what it is judged against.
func TestApplyRefusesEditsOutsideTheSelection(t *testing.T) {
	forbidden := []string{
		".agents/skillctrl/intents/manual.md",
		".agents/skills/other/SKILL.md",
		"AGENTS.md",
	}
	for _, path := range forbidden {
		t.Run(path, func(t *testing.T) {
			r := newRepo(t)
			directory := t.TempDir()
			plan := r.plan()
			r.results(directory, []string{"manual"}, nil, "criterion checked")
			r.patch(directory, r.stage(".agents/skills/manual/SKILL.md", path))
			r.reset()
			if err := adapt.Apply(r.dir, plan, directory); err == nil {
				t.Fatalf("apply accepted an edit to %s", path)
			}
		})
	}
}

// TestAcceptedRequiresACompletePartition rejects every answer that would let an
// unverified skill inherit a hash.
func TestAcceptedRequiresACompletePartition(t *testing.T) {
	invalid := []struct {
		name       string
		accepted   []string
		unresolved []string
	}{
		{"names another skill", []string{"other"}, nil},
		{"double counts a skill", []string{"manual"}, []string{"manual"}},
		{"covers nothing", nil, nil},
		{"omits a reviewed skill", nil, nil},
	}
	for _, test := range invalid {
		t.Run(test.name, func(t *testing.T) {
			r := newRepo(t)
			directory := t.TempDir()
			plan := r.plan()
			r.results(directory, test.accepted, test.unresolved, "report")
			r.patch(directory, r.stage(".agents/skills/manual/SKILL.md"))
			r.reset()
			if err := adapt.Apply(r.dir, plan, directory); err == nil {
				t.Fatal("an incomplete partition was accepted")
			}
		})
	}

	t.Run("unresolved skill must be untouched", func(t *testing.T) {
		r := newRepo(t)
		directory := t.TempDir()
		plan := r.plan()
		r.results(directory, nil, []string{"manual"}, "report")
		r.patch(directory, r.stage(".agents/skills/manual/SKILL.md"))
		r.reset()
		if err := adapt.Apply(r.dir, plan, directory); err == nil {
			t.Fatal("a changed unresolved skill was accepted")
		}
	})

	t.Run("removed skill with surviving intent cannot be accepted", func(t *testing.T) {
		r := newRepo(t)
		directory := t.TempDir()
		// The selection is made while the skill exists; the review then runs
		// against a head where it has been deleted.
		plan := r.plan()
		r.git("rm", "-q", ".agents/skills/manual/SKILL.md")
		r.git("commit", "-qm", "remove skill but keep intent")
		plan.Head = r.head()
		plan.Base = plan.Head
		plan.Comparison = plan.Head
		trees, err := lock.Snapshot(r.dir, plan.Head)
		if err != nil {
			t.Fatal(err)
		}
		plan.InputTrees = trees.Skills
		r.results(directory, []string{"manual"}, nil, "report")
		r.write(".agents/skills/manual/SKILL.md", "changed\n")
		r.git("add", "--", ".agents/skills/manual/SKILL.md")
		patch := r.patchFromIndex()
		r.reset()
		r.patch(directory, patch)
		err = adapt.Apply(r.dir, plan, directory)
		if err == nil {
			t.Fatal("a removed skill was accepted")
		}
		if !strings.Contains(err.Error(), "removed skill") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

// TestWriteRepairArtifactRefusesCredentials is the export guard: the inference
// credential must not reach a report, a result, a patch, or a staged blob.
func TestWriteRepairArtifactRefusesCredentials(t *testing.T) {
	const secret = "fixture-inference-credential-never-publish"
	leaks := map[string]func(r *repo){
		"skill body": func(r *repo) {
			r.write(".agents/skills/manual/SKILL.md", "repaired body\n"+secret+"\n")
		},
		"report": func(r *repo) {
			r.results(r.artifacts, []string{"manual"}, nil, "report mentioning "+secret)
		},
		"completion result": func(r *repo) {
			r.results(r.artifacts, []string{"manual"}, nil, "report")
			body := fmt.Sprintf(`{"accepted":["manual"],"unresolved":[],"note":%q}`, secret)
			if err := os.WriteFile(filepath.Join(r.artifacts, adapt.ResultFile), []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
		},
		"binary resource": func(r *repo) {
			path := filepath.Join(r.dir, ".agents/skills/manual/blob.bin")
			if err := os.WriteFile(path, append([]byte{0}, []byte(secret)...), 0o644); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, setup := range leaks {
		t.Run(name, func(t *testing.T) {
			r := newRepo(t)
			r.artifacts = t.TempDir()
			plan := r.plan()
			r.results(r.artifacts, []string{"manual"}, nil, "report")
			setup(r)
			if err := adapt.WriteRepairArtifact(r.dir, plan, r.artifacts, secret); err == nil {
				t.Fatalf("a leaked credential in %s was exported", name)
			}
			if _, err := os.Stat(filepath.Join(r.artifacts, adapt.PatchFile)); err == nil {
				t.Fatalf("a refused export left a patch behind for %s", name)
			}
		})
	}

	t.Run("a missing credential is refused", func(t *testing.T) {
		r := newRepo(t)
		directory := t.TempDir()
		plan := r.plan()
		r.results(directory, []string{"manual"}, nil, "report")
		r.write(".agents/skills/manual/SKILL.md", "repaired body\n")
		if err := adapt.WriteRepairArtifact(r.dir, plan, directory, ""); err == nil {
			t.Fatal("export proceeded without a credential to check against")
		}
	})

	t.Run("a clean artifact is exported", func(t *testing.T) {
		r := newRepo(t)
		directory := t.TempDir()
		plan := r.plan()
		r.results(directory, []string{"manual"}, nil, "report")
		r.write(".agents/skills/manual/SKILL.md", "repaired body\n")
		if err := adapt.WriteRepairArtifact(r.dir, plan, directory, secret); err != nil {
			t.Fatal(err)
		}
		patch, err := os.ReadFile(filepath.Join(directory, adapt.PatchFile))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(patch), "repaired body") {
			t.Fatalf("exported patch is empty: %q", patch)
		}
		if strings.Contains(string(patch), secret) {
			t.Fatal("exported patch contains the credential")
		}
	})
}

// TestTrustedSourceMustBeACommit closes a hole where a missing configuration
// variable would resolve the revision to the index - which the pull request
// under review controls - and turn "no trusted source" into "trust the
// incoming code".
func TestTrustedSourceMustBeACommit(t *testing.T) {
	for _, source := range []string{"", "HEAD", "main", "HEAD~1", "refs/heads/main", strings.Repeat("0", 39), strings.Repeat("0", 41)} {
		t.Run("source "+source, func(t *testing.T) {
			r := newRepo(t)
			if _, err := toolchain.Trusted(r.dir, source, toolchain.DefaultConfig); err == nil {
				t.Fatalf("source %q was accepted", source)
			}
			if _, err := toolchain.Models(r.dir, source, toolchain.DefaultModels); err == nil {
				t.Fatalf("source %q was accepted for models", source)
			}
		})
	}
	t.Run("plan without a commit", func(t *testing.T) {
		r := newRepo(t)
		plan := r.plan()
		plan.Head = "HEAD"
		if err := adapt.ValidateHead(r.dir, plan); err == nil {
			t.Fatal("a plan naming a ref instead of a commit was accepted")
		}
	})
}

// TestApplyLeavesNoHalfValidatedRepair keeps a refusal from looking like
// accepted work: once a patch has been applied to the index and the validation
// fails, the index must be back where it started.
func TestApplyLeavesNoHalfValidatedRepair(t *testing.T) {
	r := newRepo(t)
	directory := t.TempDir()
	plan := r.plan()
	r.results(directory, nil, []string{"manual"}, "report")
	r.patch(directory, r.stage(".agents/skills/manual/SKILL.md"))
	r.reset()
	before := r.git("diff", "--cached", "--name-only")
	if err := adapt.Apply(r.dir, plan, directory); err == nil {
		t.Fatal("an unresolved skill that changed was accepted")
	}
	if after := r.git("diff", "--cached", "--name-only"); after != before {
		t.Fatalf("a refused validation left the repair staged:\nbefore %q\nafter %q", before, after)
	}
	if _, err := os.Stat(filepath.Join(r.dir, lock.Lock)); err == nil {
		t.Fatal("a refused validation recorded a lock")
	}
}

// TestReadReportBounds keeps model output from becoming an unbounded GitHub
// comment and supplies an explicit note when no review happened.
func TestReadReportBounds(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, adapt.ReportFile), make([]byte, 40_001), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := adapt.ReadReport(directory); err == nil {
		t.Fatal("an oversized report was accepted")
	}
	if err := os.WriteFile(filepath.Join(directory, adapt.ReportFile), []byte("  \n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := adapt.ReadReport(directory); err == nil {
		t.Fatal("an empty report was accepted")
	}
	empty := t.TempDir()
	text, err := adapt.ReadReport(empty)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(text) == "" {
		t.Fatal("a missing report must still say what happened")
	}
}

// BranchPrefix mirrors the scheduled-update branch convention so publication
// tests can describe an existing update branch without importing the
// scheduled-update package, which imports this one.
const BranchPrefix = "automation/skillctrl-"

// fakeGH records gh invocations so publication can be exercised offline.
type fakeGH struct {
	pull        string
	comments    []map[string]string
	posted      map[string]string
	refSHA      string
	branchSHA   string
	createdPR   string
	existingPR  string
	setupFailed bool
}

func (g *fakeGH) JSON(args []string, input []byte) ([]byte, error) {
	switch {
	case len(args) > 1 && args[0] == "pr" && args[1] == "list":
		if g.existingPR == "" {
			return []byte("[]"), nil
		}
		return []byte(fmt.Sprintf(`[{"headRefName":"%s","url":"%s"}]`, BranchPrefix+"run", g.existingPR)), nil
	case len(args) > 1 && args[0] == "api" && strings.Contains(args[1], "/pulls/"):
		return []byte(g.pull), nil
	case len(args) > 1 && args[0] == "api" && strings.Contains(args[1], "/git/ref/heads/"):
		return []byte(fmt.Sprintf(`{"object":{"sha":%q}}`, g.branchSHA)), nil
	case len(args) > 1 && args[0] == "api" && strings.Contains(args[1], "/comments"):
		if input != nil {
			var body map[string]string
			if err := json.Unmarshal(input, &body); err != nil {
				return nil, err
			}
			g.posted = body
		}
		data, _ := json.Marshal(g.comments)
		return data, nil
	case len(args) > 2 && args[0] == "pr" && args[1] == "create":
		return []byte(g.createdPR), nil
	}
	return nil, fmt.Errorf("unexpected gh invocation: %v", args)
}

func (g *fakeGH) Run(args []string, input []byte) ([]byte, error) { return g.JSON(args, input) }

func (g *fakeGH) SetupGit() error {
	if g.setupFailed {
		return fmt.Errorf("gh auth setup-git failed")
	}
	return nil
}

// TestPublishRefusesAnythingButTheCurrentPullRequest covers the publication
// guard: a pull request that closed, moved, came from a fork, or now targets
// its own base branch is refused before anything is written.
func TestPublishRefusesAnythingButTheCurrentPullRequest(t *testing.T) {
	head := func(r *repo) string { return r.head() }
	cases := []struct {
		name string
		pull func(head string) string
	}{
		{"stale head", func(string) string {
			return `{"state":"open","head":{"ref":"feature","sha":"stale","repo":{"full_name":"fixture/repo"}},"base":{"ref":"main"}}`
		}},
		{"closed", func(head string) string {
			return `{"state":"closed","head":{"ref":"feature","sha":"` + head + `","repo":{"full_name":"fixture/repo"}},"base":{"ref":"main"}}`
		}},
		{"fork", func(head string) string {
			return `{"state":"open","head":{"ref":"feature","sha":"` + head + `","repo":{"full_name":"other/repo"}},"base":{"ref":"main"}}`
		}},
		{"targets its own base", func(head string) string {
			return `{"state":"open","head":{"ref":"main","sha":"` + head + `","repo":{"full_name":"fixture/repo"}},"base":{"ref":"main"}}`
		}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			r := newRepo(t)
			directory := t.TempDir()
			plan := r.plan()
			r.results(directory, []string{"manual"}, nil, "unresolved ambiguity")
			gh := &fakeGH{pull: test.pull(head(r))}
			if _, err := adapt.Publish(adapt.PublishOptions{
				Dir: r.dir, Plan: plan, Directory: directory,
				Repo: "fixture/repo", Number: "1", GH: gh,
			}); err == nil {
				t.Fatal("publication was not refused")
			}
			if gh.posted != nil {
				t.Fatal("a refused publication posted a comment")
			}
			if r.head() != head(r) {
				t.Fatal("a refused publication created a commit")
			}
		})
	}
}

// TestPublishReportsOnce is the rerun case: the same review must not produce a
// second comment, and an oversized report must be refused before any push.
func TestPublishReportsOnce(t *testing.T) {
	r := newRepo(t)
	directory := t.TempDir()
	plan := r.plan()
	r.results(directory, []string{"manual"}, nil, "unresolved ambiguity")
	pull := func() string {
		return `{"state":"open","head":{"ref":"feature","sha":"` + plan.Head +
			`","repo":{"full_name":"fixture/repo"}},"base":{"ref":"main"}}`
	}
	gh := &fakeGH{pull: pull(), createdPR: "https://example.invalid/pr"}
	changed, err := adapt.Publish(adapt.PublishOptions{
		Dir: r.dir, Plan: plan, Directory: directory,
		Repo: "fixture/repo", Number: "1", GH: gh,
	})
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("nothing was staged, so nothing should have been committed")
	}
	first := gh.posted["body"]
	if first == "" {
		t.Fatal("no report was posted")
	}
	gh.comments = append(gh.comments, map[string]string{"body": first})
	if _, err := adapt.Publish(adapt.PublishOptions{
		Dir: r.dir, Plan: plan, Directory: directory,
		Repo: "fixture/repo", Number: "1", GH: gh,
	}); err != nil {
		t.Fatal(err)
	}
	if len(gh.comments) != 1 {
		t.Fatal("a rerun posted a duplicate comment")
	}
}

// TestToolchainRequiresExactPins keeps a moving runtime out of CI.
func TestToolchainRequiresExactPins(t *testing.T) {
	r := newRepo(t)
	head := r.head()
	configuration, err := toolchain.Trusted(r.dir, head, toolchain.DefaultConfig)
	if err != nil {
		t.Fatal(err)
	}
	node, err := toolchain.NodeVersion(configuration)
	if err != nil || node != "1.2.3" {
		t.Fatalf("unexpected node pin %q (%v)", node, err)
	}
	r.write("home/dot_config/mise/config.toml", "[tools]\nnode = \"^1.2.3\"\n"+
		"\"npm:@earendil-works/pi-coding-agent\" = \"4.5.6\"\n[settings]\npin = true\nminimum_release_age = \"7d\"\n")
	r.git("add", "-A")
	r.git("commit", "-qm", "range pin")
	configuration, err = toolchain.Trusted(r.dir, r.head(), toolchain.DefaultConfig)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := toolchain.NodeVersion(configuration); err == nil {
		t.Fatal("a version range was accepted as a pin")
	}
}

func TestToolchainRefusesAnIncompleteConfiguration(t *testing.T) {
	r := newRepo(t)
	r.write("home/dot_config/mise/config.toml", "[tools]\nnode = \"1.2.3\"\n[settings]\npin = true\n")
	r.git("add", "-A")
	r.git("commit", "-qm", "partial")
	if _, err := toolchain.Trusted(r.dir, r.head(), toolchain.DefaultConfig); err == nil {
		t.Fatal("a configuration without the agent pin was accepted")
	}
	if _, err := toolchain.Trusted(r.dir, r.head(), "does/not/exist.toml"); err == nil {
		t.Fatal("a missing configuration was accepted")
	}
}
