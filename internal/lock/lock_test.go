package lock_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/wwwyo/skillctrl/internal/lock"
	"github.com/wwwyo/skillctrl/internal/upstream"
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

func (f *fixture) selection(t *testing.T) lock.Selection {
	t.Helper()
	current, err := lock.Snapshot(f.dir, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	recorded, err := lock.Read(f.dir, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	registered, err := upstream.Read(f.dir, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	return lock.Select(current, recorded, mustIntents(t, f.dir), registered.ManagedSkills())
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
	registered, err := upstream.Read(f.dir, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lock.Record(f.dir, current, recorded, names, lock.IntentRegistered(registered.ManagedSkills(), mustIntents(t, f.dir))); err != nil {
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
	f.write(upstream.Lock, `{"version":3,"skills":{"manual":{"source":"fixture/source","sourceType":"github"}}}`)
	f.write("AGENTS.md", "environment policy")
	f.commit()

	if got := f.selection(t).Skills; len(got) != 1 || got[0] != "manual" {
		t.Fatalf("first plan: %v", got)
	}
	f.record(t, "manual")
	f.commit()
	if f.selection(t).NeedsReview {
		t.Fatal("accepted hashes must not trigger review")
	}

	f.write("AGENTS.md", "changed environment policy")
	f.commit()
	if got := f.selection(t).Skills; len(got) != 0 {
		t.Fatalf("policy change must not select a skill: %v", got)
	}
	if f.selection(t).NeedsReview {
		t.Fatal("policy change must not trigger review")
	}

	f.write(".agents/skills/manual/references/usage.md", "updated by another installer")
	f.commit()
	if got := f.selection(t).Skills; len(got) != 1 || got[0] != "manual" {
		t.Fatalf("unrecorded edit: %v", got)
	}
	f.record(t, "manual")
	f.commit()
	if f.selection(t).NeedsReview {
		t.Fatal("accepted reference edit must not trigger review")
	}

	f.write(".agents/skillctrl/intents/manual.md", "prefer another browser")
	f.commit()
	if got := f.selection(t).Skills; len(got) != 0 {
		t.Fatalf("intent-only change must not select a skill: %v", got)
	}
	if err := os.Remove(filepath.Join(f.dir, ".agents/skillctrl/intents/manual.md")); err != nil {
		t.Fatal(err)
	}
	f.commit()
	if got := f.selection(t).Skills; len(got) != 0 {
		t.Fatalf("intent deletion must not select a skill: %v", got)
	}

	f.write(".agents/skillctrl/intents/manual.md", "intent still exists after skill removal")
	if err := os.Remove(filepath.Join(f.dir, ".agents/skills/manual/SKILL.md")); err != nil {
		t.Fatal(err)
	}
	f.commit()
	if got := f.selection(t).ReviewSkills; len(got) != 1 || got[0] != "manual" {
		t.Fatalf("removed skill with surviving intent: %v", got)
	}
	if err := os.Remove(filepath.Join(f.dir, ".agents/skillctrl/intents/manual.md")); err != nil {
		t.Fatal(err)
	}
	f.commit()
	selection := f.selection(t)
	equal(t, selection.Skills, []string{}, "removed skill without intent is excluded")
	if selection.NeedsReview {
		t.Fatal("removed skill without intent needs no review")
	}
	f.record(t, "manual")
	f.commit()
	if f.selection(t).NeedsReview {
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
	registered := map[string]any{"a": nil, "b": nil}
	if _, err := lock.Record(f.dir, current, recorded, []string{"a"}, registered); err != nil {
		t.Fatal(err)
	}
	stored, err := lock.Local(f.dir)
	if err != nil {
		t.Fatal(err)
	}
	equal(t, sortedKeys(stored.Skills), []string{"a"}, "recorded entries")
	if _, err := lock.Record(f.dir, lock.Empty(), stored, []string{"a"}, registered); err != nil {
		t.Fatal(err)
	}
	after, err := lock.Local(f.dir)
	if err != nil {
		t.Fatal(err)
	}
	equal(t, sortedKeys(after.Skills), []string{}, "recording a skill that no longer exists clears its entry")
}

func TestSelectionIgnoresHandwrittenSkillsAndPrunesLegacyHashes(t *testing.T) {
	f := newFixture(t)
	f.write(".agents/skills/imported/SKILL.md", "accepted upstream content")
	f.write(".agents/skillctrl/intents/imported.md", "keep imported behavior")
	f.write(".agents/skills/local/SKILL.md", "intentional local edit")
	f.write(".agents/skillctrl/intents/local.md", "a local intent does not opt into upstream management")
	f.write(upstream.Lock, `{"version":3,"skills":{"imported":{"source":"fixture/source","sourceType":"github"}}}`)
	f.commit()
	current, err := lock.Snapshot(f.dir, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	current.Skills["local"] = "previous-local-hash"
	f.write(lock.Lock, mustStateJSON(t, current.Skills))
	f.commit()
	plan := f.selection(t)
	equal(t, plan.Skills, []string{}, "handwritten edits are not selected")
	equal(t, plan.ReviewSkills, []string{}, "handwritten intents are not reviewed")
	if !plan.LockChanged {
		t.Fatal("legacy handwritten hashes must request lock cleanup")
	}
	f.record(t)
	stored, err := lock.Local(f.dir)
	if err != nil {
		t.Fatal(err)
	}
	equal(t, sortedKeys(stored.Skills), []string{"imported"}, "cleanup preserves registered hashes only")
	if stored.Skills["imported"] != current.Skills["imported"] {
		t.Fatal("cleanup advanced an unrelated registered hash")
	}
	if got := f.git("diff", "--name-only"); got != lock.Lock {
		t.Fatalf("cleanup changed files outside the accepted lock: %s", got)
	}
	f.commit()
	if f.selection(t).LockChanged {
		t.Fatal("cleaned lock still reports drift")
	}

	f.write(upstream.Lock, `{"version":3,"skills":{}}`)
	if f.selection(t).LockChanged {
		t.Fatal("fixed-tree selection used an uncommitted upstream registration change")
	}
	f.commit()
	plan = f.selection(t)
	if !plan.LockChanged || plan.NeedsReview || len(plan.Skills) != 0 {
		t.Fatalf("removing an upstream registration must only clean its hash: %+v", plan)
	}
	f.record(t)
	stored, err = lock.Local(f.dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored.Skills) != 0 {
		t.Fatal("unregistered skill kept its accepted hash")
	}
}

func TestHandwrittenRepositoryNeedsNoLockOrReview(t *testing.T) {
	f := newFixture(t)
	f.write(".agents/skills/local/SKILL.md", "intentional content")
	f.write(".agents/skillctrl/intents/local.md", "intentional requirements")
	f.commit()
	plan := f.selection(t)
	if plan.LockChanged || plan.NeedsReview || len(plan.Skills) != 0 {
		t.Fatalf("handwritten-only repository entered upstream management: %+v", plan)
	}
}

// TestWorkingTreePreservesStaging is the safety property that makes `record`
// usable with pending edits: the caller's index must be untouched.
func TestWorkingTreePreserveStaging(t *testing.T) {
	f := newFixture(t)
	f.write(".agents/skills/a/SKILL.md", "original")
	f.write(upstream.Lock, `{"version":3,"skills":{"a":{"source":"fixture/source","sourceType":"github"}}}`)
	f.commit()
	f.write(".agents/skills/a/SKILL.md", "staged edit")
	f.git("add", "--", ".agents/skills/a/SKILL.md")
	f.write(".agents/skills/b/SKILL.md", "untracked edit")
	f.write(upstream.Lock, `{"version":3,"skills":{"b":{"source":"fixture/source","sourceType":"github"}}}`)
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
	registered, err := upstream.Read(f.dir, tree)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := registered.Skills["b"]; !ok || len(registered.Skills) != 1 {
		t.Fatal("working tree selection ignored uncommitted upstream registrations")
	}
	if err := os.Remove(filepath.Join(f.dir, upstream.Lock)); err != nil {
		t.Fatal(err)
	}
	tree, err = lock.WorkingTree(f.dir)
	if err != nil {
		t.Fatal(err)
	}
	registered, err = upstream.Read(f.dir, tree)
	if err != nil {
		t.Fatal(err)
	}
	if len(registered.Skills) != 0 {
		t.Fatal("working tree selection ignored upstream lock removal")
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
	slices.Sort(keys)
	return keys
}

func mustIntents(t *testing.T, dir string) map[string]bool {
	t.Helper()
	names, err := lock.IntentsAt(dir, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	return names
}

func mustStateJSON(t *testing.T, hashes map[string]string) string {
	t.Helper()
	data, err := json.Marshal(map[string]any{"version": 1, "upstreams": map[string]any{}, "acceptedHashes": hashes})
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
