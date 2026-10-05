package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wwwyo/skillctrl/internal/lock"
)

func unmarshal(data []byte, target any) error { return json.Unmarshal(data, target) }

func TestRemoveRefusesLinkedIntentPathsBeforeChangingSkills(t *testing.T) {
	for _, kind := range []string{"file", "directory"} {
		t.Run(kind, func(t *testing.T) {
			h := newHarness(t)
			h.run(0, "add", "fixture/source", "--skill", "new-skill")
			h.write(".agents/skillctrl/intents/new-skill.md", "keep the new skill intent\n")
			external := t.TempDir()
			intent := filepath.Join(h.root, ".agents/skillctrl/intents/manual.md")
			target := filepath.Join(external, "manual.md")
			if err := os.WriteFile(target, []byte("external intent\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if kind == "directory" {
				intent = filepath.Dir(intent)
				target = external
			}
			if err := os.RemoveAll(intent); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, intent); err != nil {
				t.Fatal(err)
			}
			h.commitAll()
			before := h.git("status", "--porcelain")
			h.run(1, "remove", "manual", "new-skill")
			if h.git("status", "--porcelain") != before {
				t.Fatal("rejected removal changed project files")
			}
			if readAll(t, filepath.Join(external, "manual.md")) != "external intent\n" {
				t.Fatal("removal changed an external intent")
			}
		})
	}
}

func TestUpstreamOperationsIgnoreHandwrittenSkills(t *testing.T) {
	for _, withIntent := range []bool{false, true} {
		t.Run(map[bool]string{false: "without intent", true: "with intent"}[withIntent], func(t *testing.T) {
			h := newHarness(t)
			h.write(".agents/skills/other/SKILL.md", "intentional handwritten content\n")
			if withIntent {
				h.write(".agents/skillctrl/intents/other.md", "deliberate local requirements\n")
			}
			h.commitAll()
			before := h.read(".agents/skills/other/SKILL.md")
			h.run(0, "add", "fixture/source", "--skill", "new-skill")
			if _, ok := h.lockedSkills()["other"]; ok {
				t.Fatal("adding an upstream skill recorded a handwritten skill")
			}
			if string(h.read(".agents/skills/other/SKILL.md")) != string(before) {
				t.Fatal("upstream operation changed handwritten content")
			}
			if h.log() != "" {
				t.Fatal("handwritten intent triggered a reviewer")
			}
			got := h.run(0, "check")
			equal(t, list(got["local"].(map[string]any)["skills"]), []string{}, "handwritten drift is ignored")
			h.run(1, "record", "other")
			if _, ok := h.lockedSkills()["other"]; ok {
				t.Fatal("record enrolled a handwritten skill")
			}
		})
	}
}

func TestRecordPrunesLegacyHandwrittenHashesWithoutReview(t *testing.T) {
	h := newHarness(t)
	h.write(".agents/skillctrl/intents/other.md", "local intent\n")
	h.write(lock.Lock, lockBytes(t, h.root))
	h.commitAll()
	h.write(".agents/skills/other/SKILL.md", "intentional handwritten edit\n")
	h.commitAll()
	before := h.read(".agents/skills/other/SKILL.md")
	got := h.run(0, "check")
	if got["local"].(map[string]any)["lock_changed"] != true {
		t.Fatal("legacy handwritten hash did not request cleanup")
	}
	if h.git("status", "--porcelain") != "" {
		t.Fatal("check wrote the lock")
	}
	h.run(0, "update")
	h.run(0, "record", "manual")
	if _, ok := h.lockedSkills()["other"]; ok {
		t.Fatal("record retained a legacy handwritten hash")
	}
	if string(h.read(".agents/skills/other/SKILL.md")) != string(before) || h.log() != "" {
		t.Fatal("cleanup changed or reviewed handwritten content")
	}
	for _, name := range strings.Fields(h.git("diff", "--name-only")) {
		if name != lock.Lock && name != "skills-lock.json" {
			t.Fatalf("cleanup changed an unrelated file: %s", name)
		}
	}
}

func TestRecordWithoutNamesOnlyPrunesIneligibleHashes(t *testing.T) {
	h := newHarness(t)
	h.write(lock.Lock, lockBytes(t, h.root))
	accepted := h.lockedSkills()["manual"]
	h.write(".agents/skills/manual/SKILL.md", "unverified edit\n")
	h.git("add", "--", ".agents/skills/manual/SKILL.md")
	index := h.read(".fixture-git/index")
	result := h.run(0, "record")
	if len(list(result["recorded"])) != 0 || h.lockedSkills()["manual"] != accepted {
		t.Fatal("cleanup accepted unverified content")
	}
	if _, ok := h.lockedSkills()["other"]; ok {
		t.Fatal("cleanup retained an ineligible hash")
	}
	if err := os.Remove(filepath.Join(h.root, lock.Intents, "manual.md")); err != nil {
		t.Fatal(err)
	}
	h.run(0, "record")
	if len(h.lockedSkills()) != 0 {
		t.Fatal("cleanup retained hashes after every intent was removed")
	}
	if string(index) != string(h.read(".fixture-git/index")) || string(h.read(".agents/skills/manual/SKILL.md")) != "unverified edit\n" || h.log() != "" {
		t.Fatal("cleanup changed staging, skill content, or invoked a reviewer")
	}
}

// TestInstallerLifecycle is the port of the original installer suite. It walks
// the behaviors that make the tool safe to run on a real checkout: a dry run
// writes nothing, an unchanged original is not re-imported, a changed original
// keeps binary bytes and executable modes, every rejected import is atomic, and
// imports preserve accepted hashes until the caller explicitly records content.
func TestInstallerLifecycle(t *testing.T) {
	h := newHarness(t)
	status := func() string { return h.git("status", "--porcelain") }

	t.Run("dry run writes nothing", func(t *testing.T) {
		before := status()
		result := h.run(0, "--dry-run", "update")
		if result["dry_run"] != true {
			t.Fatalf("dry run: %v", result)
		}
		if status() != before {
			t.Fatal("dry run changed the working tree")
		}
	})

	t.Run("unchanged original is not re-imported", func(t *testing.T) {
		untouched := filepath.Join(h.root, ".agents/skills/other/SKILL.md")
		stamp := stampOf(t, untouched)
		result := h.run(0, "update")
		equal(t, list(result["skills"]), []string{}, "selected skills")
		if stampOf(t, untouched) != stamp {
			t.Fatal("an unchanged import rewrote an untouched skill")
		}
		if body := readAll(t, filepath.Join(h.root, ".agents/skills/manual/SKILL.md")); !strings.Contains(body, "default browser") {
			t.Fatal("adaptation was lost when nothing changed")
		}
	})

	h.reset()
	updatedTree := h.publishSecondVersion(t)
	adapted := filepath.Join(h.root, ".agents/skills/manual/SKILL.md")
	other := filepath.Join(h.root, ".agents/skills/other/SKILL.md")
	otherStamp := stampOf(t, other)
	acceptedBefore := string(h.originalLock)

	t.Run("changed original imports bytes and modes", func(t *testing.T) {
		result := h.run(0, "update", "manual")
		equal(t, list(result["skills"]), []string{"manual"}, "selected skills")
		if stampOf(t, other) != otherStamp {
			t.Fatal("updating one skill rewrote another")
		}
		if h.log() != "" || acceptedBefore != string(h.acceptedBytes()) {
			t.Fatal("pure update reviewed or accepted content")
		}
		if body := readAll(t, adapted); !strings.Contains(body, "generic browser") {
			t.Fatal("pure update did not preserve the original")
		}
		h.write(".agents/skills/manual/SKILL.md", manifest("manual", "upstream v2; default browser"))
		h.run(0, "record", "manual")
		binary := filepath.Join(h.root, ".agents/skills/manual/reference.bin")
		if readAll(t, binary) != "\x00\xff\nraw\n" {
			t.Fatal("binary content was not preserved")
		}
		info, err := os.Stat(filepath.Join(h.root, ".agents/skills/manual/run.sh"))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm()&0o111 == 0 {
			t.Fatal("executable mode was not preserved")
		}
		locked := h.lockedSkills()
		if locked["manual"] == "" {
			t.Fatal("manual hash was not recorded")
		}
		var previous struct {
			Skills map[string]string `json:"skills"`
		}
		if err := unmarshal(h.originalLock, &previous); err != nil {
			t.Fatal(err)
		}
		if locked["manual"] == previous.Skills["manual"] {
			t.Fatal("changed original kept its old accepted hash")
		}
		if _, ok := locked["other"]; ok {
			t.Fatal("handwritten skill was recorded")
		}
		entry, _ := h.upstreamSkills()["manual"].(map[string]any)
		if entry["skillFolderHash"] != updatedTree {
			t.Fatalf("upstream lock kept the wrong original hash: %v", entry["skillFolderHash"])
		}
		if entry["pluginName"] != "fixture-plugin" {
			t.Fatal("an unrelated upstream field was dropped")
		}
		if acceptedBefore == string(h.acceptedBytes()) {
			t.Fatal("accepted lock did not move")
		}
		if got := h.run(0, "check"); len(list(got["local"].(map[string]any)["skills"])) != 0 {
			t.Fatalf("check still reports drift: %v", got["local"].(map[string]any)["skills"])
		}
		if staged := h.git("diff", "--cached", "--name-only"); staged != "" {
			t.Fatalf("installer staged changes: %s", staged)
		}
		h.commitAll()
	})

	t.Run("unrelated upstream change is ignored", func(t *testing.T) {
		acceptedBytes := readAll(t, filepath.Join(h.root, lock.Lock))
		upstreamBytes := readAll(t, filepath.Join(h.root, "skills-lock.json"))
		stamp := stampOf(t, adapted)
		h.writeOrigin("README.md", "unrelated upstream change\n")
		h.originGit("add", "--", "README.md")
		h.originGit("-c", "commit.gpgsign=false", "commit", "-qm", "unrelated change")
		result := h.run(0, "update")
		if len(list(result["skills"])) != 0 {
			t.Fatalf("unrelated change selected skills: %v", result["skills"])
		}
		if readAll(t, filepath.Join(h.root, "skills-lock.json")) != upstreamBytes {
			t.Fatal("upstream lock was rewritten for an unrelated change")
		}
		if readAll(t, filepath.Join(h.root, lock.Lock)) != acceptedBytes {
			t.Fatal("accepted lock was rewritten for an unrelated change")
		}
		if stampOf(t, adapted) != stamp {
			t.Fatal("an unrelated change re-imported the skill")
		}
		h.reset()
	})

	t.Run("rejected imports are atomic", func(t *testing.T) {
		cases := []struct {
			name string
			args []string
		}{
			{"unknown skill", []string{"add", "fixture/source", "--skill", "new-skill", "--skill", "zzz-missing"}},
			{"symlink", []string{"add", "fixture/source", "--skill", "linked"}},
			{"gitignore", []string{"add", "fixture/source", "--skill", "rules"}},
			{"gitattributes", []string{"add", "fixture/source", "--skill", "attributes"}},
			{"destination ignored file", []string{"add", "fixture/source", "--skill", "ignored"}},
			{"handwritten skill", []string{"add", "fixture/source", "--skill", "other"}},
			{"credential in source", []string{"add", "https://github.com/fixture/source?token=bad", "--skill", "new-skill"}},
			{"unregistered update", []string{"update", "other"}},
			{"traversal name", []string{"record", "../other"}},
		}
		for _, test := range cases {
			t.Run(test.name, func(t *testing.T) {
				h.reset()
				lockBefore := readAll(t, filepath.Join(h.root, lock.Lock))
				upstreamBefore := readAll(t, filepath.Join(h.root, "skills-lock.json"))
				stdout, stderr, code := h.try(test.args...)
				if code != 1 {
					t.Fatalf("exit %d want 1 (stdout %s stderr %s)", code, stdout, stderr)
				}
				if !strings.Contains(stderr, "\"ok\":false") {
					t.Fatalf("failure was not reported as JSON on stderr: %s", stderr)
				}
				if strings.TrimSpace(stdout) != "" {
					t.Fatalf("failure wrote to stdout: %s", stdout)
				}
				if readAll(t, filepath.Join(h.root, lock.Lock)) != lockBefore {
					t.Fatal("rejected import moved the accepted lock")
				}
				if readAll(t, filepath.Join(h.root, "skills-lock.json")) != upstreamBefore {
					t.Fatal("rejected import moved the upstream lock")
				}
				if diff := h.git("diff", "--name-only"); diff != "" {
					t.Fatalf("rejected import left changes: %s", diff)
				}
				if _, err := os.Stat(filepath.Join(h.root, ".agents/skills/new-skill")); err == nil {
					t.Fatal("rejected import created the skill")
				}
			})
		}
	})

	t.Run("intent-free import needs no reviewer", func(t *testing.T) {
		h.reset()
		previous := h.env
		h.env = append(h.env, "OPENCODE_API_KEY=")
		defer func() { h.env = previous }()
		os.Remove(filepath.Join(h.base, "pi.log"))
		h.run(0, "add", "fixture/source", "--skill", "new-skill")
		if _, ok := h.lockedSkills()["new-skill"]; ok {
			t.Fatal("intent-free skill was recorded in the accepted lock")
		}
		if _, ok := h.upstreamSkills()["new-skill"]; !ok {
			t.Fatal("intent-free skill was not registered upstream")
		}
		link := filepath.Join(h.root, ".claude/skills/new-skill")
		target, err := os.Readlink(link)
		if err != nil {
			t.Fatal(err)
		}
		if target != filepath.Join("../../.agents/skills/new-skill") {
			t.Fatalf("unexpected Claude link target: %s", target)
		}
		if h.log() != "" {
			t.Fatal("a skill without intent must not invoke the reviewer")
		}
	})

	t.Run("remove deletes the selected intent", func(t *testing.T) {
		h.write(".agents/skillctrl/intents/new-skill.md", "local requirements for the new skill\n")
		h.commitAll()
		before := status()
		h.run(0, "--dry-run", "remove", "new-skill")
		if status() != before {
			t.Fatal("dry-run remove changed project files")
		}
		h.run(0, "remove", "new-skill")
		if _, err := os.Lstat(filepath.Join(h.root, ".agents/skillctrl/intents/new-skill.md")); !os.IsNotExist(err) {
			t.Fatalf("remove retained the selected intent: %v", err)
		}
		if _, ok := h.lockedSkills()["new-skill"]; ok {
			t.Fatal("removed skill stayed in the accepted lock")
		}
		if _, ok := h.upstreamSkills()["new-skill"]; ok {
			t.Fatal("removed skill stayed in the upstream lock")
		}
		if _, err := os.Stat(filepath.Join(h.root, ".agents/skills/manual/SKILL.md")); err != nil {
			t.Fatal("remove deleted an unrelated skill")
		}
		if _, err := os.Stat(filepath.Join(h.root, ".agents/skillctrl/intents/manual.md")); err != nil {
			t.Fatal("remove deleted an unrelated intent document")
		}
	})

	h.reset()
	t.Run("record preserves staging in the main checkout", func(t *testing.T) {
		// A record run must work in a plain checkout, not only in a worktree.
		if err := os.Remove(filepath.Join(h.root, ".git")); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(filepath.Join(h.root, ".fixture-git"), filepath.Join(h.root, ".git")); err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := os.Rename(filepath.Join(h.root, ".git"), filepath.Join(h.root, ".fixture-git")); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(h.root, ".git"), []byte("gitdir: .fixture-git\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}()
		h.write(".agents/skills/manual/SKILL.md", "intentional manual edit\n")
		h.write(".agents/skills/other/SKILL.md", "unrecorded other edit\n")
		h.git("add", "--", ".agents/skills/manual/SKILL.md")
		staged := h.git("diff", "--cached", "--binary")
		h.run(0, "record", "manual")
		if h.git("diff", "--cached", "--binary") != staged {
			t.Fatal("record disturbed the caller's staging")
		}
		got := h.run(0, "check")
		equal(t, list(got["local"].(map[string]any)["skills"]), []string{}, "check ignores handwritten edits after record")
	})
}

// publishSecondVersion adds new content to the upstream original and returns the
// new tree hash.
func (h *harness) publishSecondVersion(t *testing.T) string {
	h.t.Helper()
	full := filepath.Join(h.origin, "skills", "manual")
	h.writeOrigin("skills/manual/SKILL.md", manifest("manual", "upstream v2; generic browser"))
	h.writeOrigin("skills/manual/reference.bin", "\x00\xff\nraw\n")
	h.writeOrigin("skills/manual/run.sh", "#!/bin/sh\nexit 0\n")
	if err := os.Chmod(filepath.Join(full, "run.sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	h.originGit("add", "--", "skills")
	h.originGit("-c", "commit.gpgsign=false", "commit", "-qm", "original v2")
	return h.originGit("rev-parse", "HEAD:skills/manual")
}

func (h *harness) writeOrigin(path, content string) {
	h.t.Helper()
	full := filepath.Join(h.origin, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		h.t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		h.t.Fatal(err)
	}
}

func stampOf(t *testing.T, path string) int64 {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.ModTime().UnixNano()
}

func readAll(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
