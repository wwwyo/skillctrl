package lock_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wwwyo/skillctrl/internal/lock"
)

// fixture is a throwaway Git repository with a fixed identity so commits work
// on a machine whose global signing or identity configuration would otherwise
// make the test fail for the wrong reason.
type fixture struct {
	t   *testing.T
	dir string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	dir := t.TempDir()
	f := &fixture{t: t, dir: dir}
	f.git("init", "-q")
	f.git("config", "user.name", "Fixture")
	f.git("config", "user.email", "fixture@example.invalid")
	f.git("config", "commit.gpgsign", "false")
	return f
}

func (f *fixture) git(args ...string) string {
	f.t.Helper()
	command := exec.Command("git", args...)
	command.Dir = f.dir
	command.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1")
	out, err := command.CombinedOutput()
	if err != nil {
		f.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return trim(string(out))
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

func (f *fixture) commit() string {
	f.t.Helper()
	f.git("add", "-A")
	f.git("commit", "-qm", "fixture")
	return f.git("rev-parse", "HEAD")
}

func (f *fixture) plan(t *testing.T, base, since string) lock.Plan {
	t.Helper()
	plan, err := lock.Compare(f.dir, base, "HEAD", since)
	if err != nil {
		t.Fatalf("compare: %v", err)
	}
	return plan
}

func (f *fixture) record(t *testing.T, names ...string) {
	t.Helper()
	current, err := lock.Snapshot(f.dir, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	recorded, err := lock.Local(f.dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lock.Record(f.dir, current, recorded, names); err != nil {
		t.Fatal(err)
	}
}

func trim(value string) string { return strings.TrimSpace(value) }

func equal(t *testing.T, got, want []string, label string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: got %v want %v", label, got, want)
	}
	for index := range got {
		if got[index] != want[index] {
			t.Fatalf("%s: got %v want %v", label, got, want)
		}
	}
}

// TestHashesDetectUnrecordedEdits is the port of lock.test.sh: a hash covers the
// whole skill directory, an accepted hash stops review, and neither intent
// files nor repository policy participate in the hash.
func TestHashesDetectUnrecordedEdits(t *testing.T) {
	f := newFixture(t)
	f.write(".agents/skills/manual/SKILL.md", "manual skill")
	f.write(".agents/skills/manual/references/usage.md", "old reference")
	f.write(".agents/skillctrl/intents/manual.md", "prefer the default browser")
	f.write("AGENTS.md", "environment policy")
	first := f.commit()

	if got := f.plan(t, first, "").Skills; len(got) != 1 || got[0] != "manual" {
		t.Fatalf("first plan: %v", got)
	}
	f.record(t, "manual")
	base := f.commit()
	if f.plan(t, base, "").NeedsReview {
		t.Fatal("accepted hashes must not trigger review")
	}

	f.write("AGENTS.md", "changed environment policy")
	policy := f.commit()
	if got := f.plan(t, base, "").Skills; len(got) != 0 {
		t.Fatalf("policy change must not select a skill: %v", got)
	}
	if f.plan(t, policy, "").NeedsReview {
		t.Fatal("policy change must not trigger review")
	}

	f.write(".agents/skills/manual/references/usage.md", "updated by another installer")
	changed := f.commit()
	if got := f.plan(t, policy, "").Skills; len(got) != 1 || got[0] != "manual" {
		t.Fatalf("unrecorded edit: %v", got)
	}
	f.record(t, "manual")
	f.commit()
	if f.plan(t, base, changed).NeedsReview {
		t.Fatal("accepted reference edit must not trigger review")
	}

	f.write(".agents/skillctrl/intents/manual.md", "prefer another browser")
	reviewed := f.commit()
	if got := f.plan(t, reviewed, "").Skills; len(got) != 0 {
		t.Fatalf("intent-only change must not select a skill: %v", got)
	}
	if err := os.Remove(filepath.Join(f.dir, ".agents/skillctrl/intents/manual.md")); err != nil {
		t.Fatal(err)
	}
	noIntent := f.commit()
	if got := f.plan(t, reviewed, "").Skills; len(got) != 0 {
		t.Fatalf("intent deletion must not select a skill: %v", got)
	}

	f.write(".agents/skillctrl/intents/manual.md", "intent still exists after skill removal")
	if err := os.Remove(filepath.Join(f.dir, ".agents/skills/manual/SKILL.md")); err != nil {
		t.Fatal(err)
	}
	removed := f.commit()
	if got := f.plan(t, noIntent, "").ReviewSkills; len(got) != 1 || got[0] != "manual" {
		t.Fatalf("removed skill with surviving intent: %v", got)
	}
	if err := os.Remove(filepath.Join(f.dir, ".agents/skillctrl/intents/manual.md")); err != nil {
		t.Fatal(err)
	}
	f.commit()
	selection := f.plan(t, removed, "")
	equal(t, selection.Skills, []string{"manual"}, "removed skill selection")
	if selection.NeedsReview {
		t.Fatal("removed skill without intent needs no review")
	}
	f.record(t, "manual")
	empty := f.commit()
	if f.plan(t, empty, "").NeedsReview {
		t.Fatal("empty lock must not trigger review")
	}
}

// TestRecordPreservesUnrelatedEntries pins the rule that only named skills move.
func TestRecordPreservesUnrelatedEntries(t *testing.T) {
	f := newFixture(t)
	f.write(".agents/skills/a/SKILL.md", "a")
	f.write(".agents/skills/b/SKILL.md", "b")
	f.commit()
	current, err := lock.Snapshot(f.dir, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	recorded := lock.Empty()
	if _, err := lock.Record(f.dir, current, recorded, []string{"a"}); err != nil {
		t.Fatal(err)
	}
	stored, err := lock.Local(f.dir)
	if err != nil {
		t.Fatal(err)
	}
	equal(t, sortedKeys(stored.Skills), []string{"a"}, "recorded entries")
	if _, err := lock.Record(f.dir, lock.Empty(), stored, []string{"a"}); err != nil {
		t.Fatal(err)
	}
	after, err := lock.Local(f.dir)
	if err != nil {
		t.Fatal(err)
	}
	equal(t, sortedKeys(after.Skills), []string{}, "recording a skill that no longer exists clears its entry")
}

// TestWorkingTreePreservesStaging is the safety property that makes `record`
// usable with pending edits: the caller's index must be untouched.
func TestWorkingTreePreserveStaging(t *testing.T) {
	f := newFixture(t)
	f.write(".agents/skills/a/SKILL.md", "original")
	f.commit()
	f.write(".agents/skills/a/SKILL.md", "staged edit")
	f.git("add", "--", ".agents/skills/a/SKILL.md")
	f.write(".agents/skills/b/SKILL.md", "untracked edit")
	before := f.git("diff", "--cached", "--binary")

	tree, err := lock.WorkingTree(f.dir)
	if err != nil {
		t.Fatal(err)
	}
	current, err := lock.Snapshot(f.dir, tree)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := current.Skills["b"]; !ok {
		t.Fatal("working tree hashing must see uncommitted skills")
	}
	if after := f.git("diff", "--cached", "--binary"); after != before {
		t.Fatalf("staging changed:\n%s\n%s", before, after)
	}
	if len(f.git("status", "--porcelain")) == 0 {
		t.Fatal("expected pending changes to remain")
	}
}

// TestExecutableBitIsCovered guards the claim that a hash covers the whole skill
// directory: making a script executable must change the hash.
func TestExecutableBitIsCovered(t *testing.T) {
	f := newFixture(t)
	// Mode changes are invisible where core.fileMode is off, which some shared
	// configurations do by default.
	f.git("config", "core.fileMode", "true")
	f.write(".agents/skills/a/SKILL.md", "body")
	f.write(".agents/skills/a/run.sh", "#!/bin/sh\n")
	f.commit()
	first, err := lock.Snapshot(f.dir, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(f.dir, ".agents/skills/a/run.sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	f.git("add", "-A")
	f.git("commit", "-qm", "executable")
	second, err := lock.Snapshot(f.dir, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if first.Skills["a"] == second.Skills["a"] {
		t.Fatal("executable mode must be part of the skill hash")
	}
}

func TestParseRejectsUnsupportedLocks(t *testing.T) {
	cases := map[string]string{
		"version":    `{"version":1,"skills":{}}`,
		"shape":      `{"version":2,"skills":[]}`,
		"unreadable": `not json`,
	}
	for label, content := range cases {
		if _, err := lock.Parse([]byte(content)); err == nil {
			t.Fatalf("%s lock accepted: %s", label, content)
		}
	}
	value, err := lock.Parse([]byte(`{"version":2,"skills":{"b":"x","a":"y"}}`))
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != `{"version":2,"skills":{"a":"y","b":"x"}}` {
		t.Fatalf("lock round-trip changed: %s", encoded)
	}
}

func sortedKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	for i := range keys {
		for j := i + 1; j < len(keys); j++ {
			if keys[j] < keys[i] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
	return keys
}
