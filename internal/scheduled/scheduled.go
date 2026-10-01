// Package scheduled prepares immutable upstream inputs for a periodic run and
// publishes a verified update.
//
// The update flow is deliberately two-phase. The first phase fetches every
// original and commits the result, producing an immutable input commit plus a
// Git bundle that carries only that delta. The second phase runs in a different
// job: it re-derives the plan from the trusted base, verifies that the bundle
// really is a direct child of that base and that the imported trees match their
// recorded original hashes, and only then adapts and publishes. A runner that
// cannot reproduce the input cannot publish an update built on it.
package scheduled

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/wwwyo/skillctrl/internal/adapt"
	"github.com/wwwyo/skillctrl/internal/gitx"
	"github.com/wwwyo/skillctrl/internal/install"
	"github.com/wwwyo/skillctrl/internal/lock"
	"github.com/wwwyo/skillctrl/internal/upstream"
)

// Ref is the local reference that carries the fixed import delta between jobs.
const Ref = "refs/heads/skillctrl-input"

// BranchPrefix marks update branches so a rerun finds the existing one instead
// of opening a parallel update.
const BranchPrefix = "automation/skillctrl-"

// WorktreeProvider is re-exported so a publication test can describe the run
// environment without importing the installer package.

// Install is the importer used by Prepare. It is a variable so a test can
// supply a fixture without cloning a real upstream.
var Install = upstream.Install

// OpenUpdate finds an existing update pull request.
func OpenUpdate(gh adapt.GH, repo string) (string, error) {
	out, err := gh.JSON([]string{"pr", "list", "--repo", repo, "--state", "open",
		"--limit", "100", "--json", "headRefName,url"}, nil)
	if err != nil {
		return "", err
	}
	var pulls []struct {
		HeadRefName string `json:"headRefName"`
		URL         string `json:"url"`
	}
	if err := json.Unmarshal(out, &pulls); err != nil {
		return "", fmt.Errorf("unexpected pull request list response")
	}
	for _, pull := range pulls {
		if strings.HasPrefix(pull.HeadRefName, BranchPrefix) {
			return pull.URL, nil
		}
	}
	return "", nil
}

// ValidateImport restricts the import to registered skills, their source
// records, and the relative Claude links. Anything else in the diff means the
// update did more than refresh originals.
func ValidateImport(dir, base, tree string) error {
	before, err := lockEntries(dir, base)
	if err != nil {
		return err
	}
	after, err := lockEntries(dir, tree)
	if err != nil {
		return err
	}
	if !sameKeys(before.Skills, after.Skills) {
		return fmt.Errorf("scheduled updates cannot add or remove upstream registrations")
	}
	registered := slices.Sorted(maps.Keys(before.Skills))
	installed, err := lock.Snapshot(dir, base)
	if err != nil {
		return err
	}
	prefixes := make([]string, 0, len(registered))
	for _, name := range registered {
		prefixes = append(prefixes, lock.Skills+name+"/")
	}
	out, err := gitx.Output(dir, "diff", "--name-only", "-z", "--no-renames", base, tree)
	if err != nil {
		return err
	}
	for _, raw := range strings.Split(string(out), "\x00") {
		if raw == "" {
			continue
		}
		if raw == upstream.Lock || hasAnyPrefix(raw, prefixes) {
			continue
		}
		if name, ok := strings.CutPrefix(raw, ".claude/skills/"); ok {
			if _, present := installed.Skills[name]; present {
				entry, err := gitx.Output(dir, "ls-tree", tree, "--", raw)
				if err == nil && strings.TrimSpace(string(entry)) != "" {
					fields := strings.Fields(string(entry))
					target, showErr := gitx.Output(dir, "show", tree+":"+raw)
					if fields[0] == "120000" && showErr == nil &&
						strings.TrimSpace(string(target)) == "../../.agents/skills/"+name {
						continue
					}
				}
			}
		}
		return fmt.Errorf("upstream import changed an unauthorized path: %s", raw)
	}
	actual, err := lock.Snapshot(dir, tree)
	if err != nil {
		return err
	}
	for _, name := range registered {
		entry, _ := after.Skills[name].(map[string]any)
		original, _ := before.Skills[name].(map[string]any)
		if field(entry, "source") != field(original, "source") || field(entry, "sourceType") != "github" {
			return fmt.Errorf("scheduled update changed upstream identity: %s", name)
		}
		// An unchanged original must still be absent from the diff; a changed
		// original must appear with exactly the hash recorded in the lock.
		expected := field(entry, "skillFolderHash")
		if sameEntry(entry, original) {
			expected = installed.Skills[name]
		}
		if expected == "" || actual.Skills[name] != expected {
			return fmt.Errorf("imported skill differs from its original or unchanged tree hash: %s", name)
		}
	}
	return nil
}

func hasAnyPrefix(value string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(value, prefix) {
			return true
		}
	}
	return false
}

func field(object map[string]any, key string) string {
	value, _ := object[key].(string)
	return value
}

func sameEntry(a, b map[string]any) bool {
	left, _ := json.Marshal(a)
	right, _ := json.Marshal(b)
	return string(left) == string(right)
}

func sameKeys(a, b map[string]any) bool {
	if len(a) != len(b) {
		return false
	}
	for name := range a {
		if _, ok := b[name]; !ok {
			return false
		}
	}
	return true
}

type upstreamLock struct {
	Skills map[string]any `json:"skills"`
}

func lockEntries(dir, tree string) (upstreamLock, error) {
	before, err := gitx.Output(dir, "show", tree+":"+upstream.Lock)
	if err != nil {
		return upstreamLock{}, fmt.Errorf("upstream lock is unavailable at %s: %w", tree, err)
	}
	var value upstreamLock
	if err := json.Unmarshal(before, &value); err != nil {
		return upstreamLock{}, fmt.Errorf("unreadable upstream lock at %s: %w", tree, err)
	}
	if value.Skills == nil {
		return upstreamLock{}, fmt.Errorf("upstream lock at %s has no skills", tree)
	}
	return value, nil
}

// Result is the prepared input plus whether anything changed.
type Result struct {
	Plan       lock.Plan `json:"plan"`
	Changed    bool      `json:"changed"`
	ExistingPR string    `json:"existing_pr,omitempty"`
}

// Prepare fetches all originals before emitting a fixed input commit, bundle,
// and hash plan.
func Prepare(dir, directory, ghRepo string, gh adapt.GH) (Result, error) {
	if existing, err := OpenUpdate(gh, ghRepo); err != nil {
		return Result{}, err
	} else if existing != "" {
		head, err := gitx.Output(dir, "rev-parse", "HEAD")
		if err != nil {
			return Result{}, err
		}
		base := gitx.Trimmed(head)
		plan, err := lock.Compare(dir, base, base, "")
		if err != nil {
			return Result{}, err
		}
		if err := writePlan(directory, plan); err != nil {
			return Result{}, err
		}
		return Result{Plan: plan, Changed: false, ExistingPR: existing}, nil
	}
	status, err := gitx.Output(dir, "status", "--porcelain")
	if err != nil {
		return Result{}, err
	}
	if strings.TrimSpace(string(status)) != "" {
		return Result{}, fmt.Errorf("scheduled update requires a clean checkout")
	}
	head, err := gitx.Output(dir, "rev-parse", "HEAD")
	if err != nil {
		return Result{}, err
	}
	base := gitx.Trimmed(head)
	skills := filepath.Join(dir, filepath.FromSlash(lock.Skills))
	if err := install.CheckSkills(skills); err != nil {
		return Result{}, err
	}
	temporary, err := os.MkdirTemp("", "skillctrl-original-")
	if err != nil {
		return Result{}, err
	}
	defer os.RemoveAll(temporary)
	target, upstreamLockPath, err := Install(dir, "update", nil, "", temporary)
	if err != nil {
		return Result{}, err
	}
	if err := install.CheckSkills(target); err != nil {
		return Result{}, err
	}
	if err := install.Import(dir, target, upstreamLockPath); err != nil {
		return Result{}, err
	}
	if err := gitx.Run(dir, "add", "-A", "--", lock.Skills, upstream.Lock, ".claude/skills"); err != nil {
		return Result{}, err
	}
	tree, err := gitx.Output(dir, "write-tree")
	if err != nil {
		return Result{}, err
	}
	treeID := gitx.Trimmed(tree)
	if err := ValidateImport(dir, base, treeID); err != nil {
		return Result{}, err
	}
	staged, err := gitx.Output(dir, "diff", "--cached", "--name-only")
	if err != nil {
		return Result{}, err
	}
	if strings.TrimSpace(string(staged)) != "" {
		if err := adapt.Commit(dir, "chore(skills): refresh upstream source trees"); err != nil {
			return Result{}, err
		}
	}
	prepared, err := gitx.Output(dir, "rev-parse", "HEAD")
	if err != nil {
		return Result{}, err
	}
	preparedID := gitx.Trimmed(prepared)
	plan, err := lock.Compare(dir, base, preparedID, "")
	if err != nil {
		return Result{}, err
	}
	if err := writePlan(directory, plan); err != nil {
		return Result{}, err
	}
	changed := base != preparedID || plan.LockChanged
	if base != preparedID {
		if err := gitx.Run(dir, "update-ref", Ref, preparedID); err != nil {
			return Result{}, err
		}
		// The writer already has base, so the bundle carries only the delta.
		if err := gitx.Run(dir, "bundle", "create", filepath.Join(directory, "input.bundle"), Ref, "^"+base); err != nil {
			return Result{}, err
		}
	}
	return Result{Plan: plan, Changed: changed}, nil
}

func writePlan(directory string, plan lock.Plan) error {
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(plan)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(directory, "plan.json"), data, 0o644)
}

// Restore validates the read-only selection artifact before using its imported
// checkout.
func Restore(dir, directory, base string) (lock.Plan, error) {
	data, err := os.ReadFile(filepath.Join(directory, "plan.json"))
	if err != nil {
		return lock.Plan{}, fmt.Errorf("update input is unavailable")
	}
	var supplied lock.Plan
	if err := json.Unmarshal(data, &supplied); err != nil {
		return lock.Plan{}, fmt.Errorf("update input is not bound to the selected base")
	}
	head := supplied.Head
	if supplied.Base != base || !commitPattern.MatchString(head) {
		return lock.Plan{}, fmt.Errorf("update input is not bound to the selected base")
	}
	if head != base {
		bundle := filepath.Join(directory, "input.bundle")
		heads, err := gitx.Output(dir, "bundle", "list-heads", bundle)
		if err != nil {
			return lock.Plan{}, err
		}
		if strings.TrimSpace(string(heads)) != head+" "+Ref {
			return lock.Plan{}, fmt.Errorf("unexpected update bundle reference")
		}
		if err := gitx.SafeRun(dir, "fetch", "--no-tags", bundle, Ref); err != nil {
			return lock.Plan{}, err
		}
		parents, err := gitx.Output(dir, "rev-list", "--parents", "-n", "1", head)
		if err != nil {
			return lock.Plan{}, err
		}
		fields := strings.Fields(string(parents))
		if len(fields) != 2 || fields[1] != base {
			return lock.Plan{}, fmt.Errorf("update input must be a direct child of the selected base")
		}
	}
	if err := ValidateImport(dir, base, head); err != nil {
		return lock.Plan{}, err
	}
	actual, err := lock.Compare(dir, base, head, "")
	if err != nil {
		return lock.Plan{}, err
	}
	if !samePlan(actual, supplied) {
		return lock.Plan{}, fmt.Errorf("update input plan differs from its Git trees")
	}
	if err := gitx.SafeRun(dir, "reset", "--hard", head); err != nil {
		return lock.Plan{}, err
	}
	if err := install.CheckSkills(filepath.Join(dir, filepath.FromSlash(lock.Skills))); err != nil {
		return lock.Plan{}, err
	}
	return actual, nil
}

var commitPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

func samePlan(a, b lock.Plan) bool {
	left, _ := json.Marshal(a)
	right, _ := json.Marshal(b)
	return string(left) == string(right)
}

// Publish tests the complete repaired update before pushing one draft pull
// request. Nothing is pushed until the repository's own tests pass and the
// default branch is confirmed unchanged.
func Publish(dir, directory string, plan lock.Plan, environment map[string]string, gh adapt.GH) (map[string]any, error) {
	if err := adapt.ValidateHead(dir, plan); err != nil {
		return nil, err
	}
	repo := environment["GITHUB_REPOSITORY"]
	branch := environment["DEFAULT_BRANCH"]
	run := environment["GITHUB_RUN_ID"]
	attempt := environment["GITHUB_RUN_ATTEMPT"]
	if repo == "" || branch == "" || run == "" || attempt == "" {
		return nil, fmt.Errorf("publish requires the scheduled run environment")
	}
	if err := gitx.Run(dir, "check-ref-format", "refs/heads/"+branch); err != nil {
		return nil, err
	}
	existing, err := OpenUpdate(gh, repo)
	if err != nil {
		return nil, err
	}
	if existing != "" {
		return map[string]any{"existing_pr": existing}, nil
	}
	report, err := adapt.ReadReport(directory)
	if err != nil {
		return nil, err
	}
	// The import commit is runner-local; the final pull request carries one
	// commit based on the default branch.
	if err := gitx.Run(dir, "reset", "--soft", plan.Base); err != nil {
		return nil, err
	}
	allowed := make([]string, 0, len(plan.Skills))
	for _, name := range plan.Skills {
		allowed = append(allowed, lock.Skills+name+"/")
	}
	originalDiff, err := gitx.Output(dir, "diff", "--name-only", "-z", plan.Base, plan.Head)
	if err != nil {
		return nil, err
	}
	original := map[string]bool{}
	for _, raw := range strings.Split(string(originalDiff), "\x00") {
		if raw != "" {
			original[raw] = true
		}
	}
	staged, err := gitx.Output(dir, "diff", "--cached", "--name-only", "-z", "--no-renames")
	if err != nil {
		return nil, err
	}
	for _, raw := range strings.Split(string(staged), "\x00") {
		if raw == "" || original[raw] || raw == lock.Lock || hasAnyPrefix(raw, allowed) {
			continue
		}
		return nil, fmt.Errorf("scheduled publication changed an unauthorized path: %s", raw)
	}
	stagedNames, err := gitx.Output(dir, "diff", "--cached", "--name-only")
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(string(stagedNames)) == "" {
		return map[string]any{"changed": false}, nil
	}
	if err := adapt.Commit(dir, "chore(skills): update originals and preserve customization"); err != nil {
		return nil, err
	}
	if err := gitx.SafeRun(dir, "reset", "--hard", "HEAD"); err != nil {
		return nil, err
	}
	// Index-only repair deletions would otherwise remain as untracked files.
	if err := gitx.Run(dir, "clean", "-fdx", "--", lock.Skills); err != nil {
		return nil, err
	}
	tests, err := verificationTests(dir)
	if err != nil {
		return nil, err
	}
	if len(tests) == 0 {
		return nil, fmt.Errorf("no update verification tests found")
	}
	for _, test := range tests {
		command := exec.Command("bash", test)
		command.Env = append(os.Environ(), "PYTHONDONTWRITEBYTECODE=1")
		command.Stdout = os.Stderr
		command.Stderr = os.Stderr
		if err := command.Run(); err != nil {
			return nil, err
		}
	}
	status, err := gitx.Output(dir, "status", "--porcelain")
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(string(status)) != "" {
		return nil, fmt.Errorf("verification changed the prepared update")
	}
	current, err := gh.JSON([]string{"api", fmt.Sprintf("repos/%s/git/ref/heads/%s", repo, branch)}, nil)
	if err != nil {
		return nil, err
	}
	var reference struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	if err := json.Unmarshal(current, &reference); err != nil {
		return nil, err
	}
	if reference.Object.SHA != plan.Base {
		return nil, fmt.Errorf("default branch advanced during update; rerun on its new head")
	}
	name := fmt.Sprintf("%s%s-%s", BranchPrefix, run, attempt)
	if err := gitx.Run(dir, "check-ref-format", "refs/heads/"+name); err != nil {
		return nil, err
	}
	if err := gh.SetupGit(); err != nil {
		return nil, err
	}
	if err := gitx.Run(dir, "push", "origin", "HEAD:refs/heads/"+name); err != nil {
		return nil, err
	}
	link := fmt.Sprintf("https://github.com/%s/actions/runs/%s", repo, run)
	body := fmt.Sprintf("%s\n\nUpdated originals, re-adapted them to the saved intent, and ran all %d repository tests in this workflow.\n\nVerification: %s\nBase used for the fetch: `%s`. This update is not merged automatically.",
		report, len(tests), link, plan.Base)
	created, err := gh.JSON([]string{"pr", "create", "--repo", repo, "--base", branch,
		"--head", name, "--draft", "--title", "chore(skills): update upstream skills", "--body-file", "-"},
		[]byte(body))
	if err != nil {
		return nil, err
	}
	return map[string]any{"pr": strings.TrimSpace(string(created)), "tests": len(tests)}, nil
}

// verificationTests finds the repository's shell test suites. Running the
// repository's own tests before publishing is the point: an update that breaks
// an unrelated check must not reach a pull request.
func verificationTests(dir string) ([]string, error) {
	var tests []string
	for _, pattern := range []string{"tests/*.test.sh", ".agents/skillctrl/*.test.sh"} {
		matches, err := filepath.Glob(filepath.Join(dir, filepath.FromSlash(pattern)))
		if err != nil {
			return nil, err
		}
		slices.Sort(matches)
		tests = append(tests, matches...)
	}
	return tests, nil
}
