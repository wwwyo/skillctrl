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

// TestInstallerLifecycle is the port of the original installer suite. It walks
// the behaviors that make the tool safe to run on a real checkout: a dry run
// writes nothing, an unchanged original is not re-imported, a changed original
// keeps binary bytes and executable modes, every rejected import is atomic, and
// unresolved or failed adaptation retains the previously accepted hash.
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
		if body := readAll(t, adapted); !strings.Contains(body, "upstream v2") ||
			!strings.Contains(body, "default browser") {
			// The imported original must be adapted to the saved intent, not
			// dropped in favour of either the original or the old edit.
			t.Fatalf("unexpected imported body: %q", body)
		}
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
		if locked["other"] != previous.Skills["other"] {
			t.Fatal("recording one skill moved another")
		}
		entry, _ := h.upstreamSkills()["manual"].(map[string]any)
		if entry["skillFolderHash"] != updatedTree {
			t.Fatalf("upstream lock kept the wrong original hash: %v", entry["skillFolderHash"])
		}
		if entry["pluginName"] != "fixture-plugin" {
			t.Fatal("an unrelated upstream field was dropped")
		}
		if acceptedBefore == string(h.read(lock.Lock)) {
			t.Fatal("accepted lock did not move")
		}
		if got := h.run(0, "status"); len(list(got["skills"])) != 0 {
			t.Fatalf("status still reports drift: %v", got["skills"])
		}
		if staged := h.git("diff", "--cached", "--name-only"); staged != "" {
			t.Fatalf("installer staged changes: %s", staged)
		}
		h.commitAll()
	})

	t.Run("unrelated upstream change is ignored", func(t *testing.T) {
		acceptedBytes := readAll(t, filepath.Join(h.root, lock.Lock))
		upstreamBytes := readAll(t, filepath.Join(h.root, ".agents/.skill-lock.json"))
		stamp := stampOf(t, adapted)
		h.writeOrigin("README.md", "unrelated upstream change\n")
		h.originGit("add", "--", "README.md")
		h.originGit("-c", "commit.gpgsign=false", "commit", "-qm", "unrelated change")
		result := h.run(0, "update")
		if len(list(result["skills"])) != 0 {
			t.Fatalf("unrelated change selected skills: %v", result["skills"])
		}
		if readAll(t, filepath.Join(h.root, ".agents/.skill-lock.json")) != upstreamBytes {
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
				upstreamBefore := readAll(t, filepath.Join(h.root, ".agents/.skill-lock.json"))
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
				if readAll(t, filepath.Join(h.root, ".agents/.skill-lock.json")) != upstreamBefore {
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

	t.Run("unresolved adaptation exits 2 and keeps the old hash", func(t *testing.T) {
		h.reset()
		restore := h.withEnv("FIXTURE_UNRESOLVED", "1")
		defer restore()
		result := h.run(2, "update")
		equal(t, list(result["unresolved"]), []string{"manual"}, "unresolved skills")
		if readAll(t, filepath.Join(h.root, lock.Lock)) != string(h.originalLock) {
			t.Fatal("unresolved adaptation advanced the accepted lock")
		}
		if body := readAll(t, adapted); !strings.Contains(body, "generic browser") {
			t.Fatal("unresolved adaptation changed the skill body")
		}
		got := h.run(0, "status")
		equal(t, list(got["review_skills"]), []string{"manual"}, "status review skills")
		if staged := h.git("diff", "--cached", "--name-only"); staged != "" {
			t.Fatalf("unresolved adaptation staged changes: %s", staged)
		}
	})

	t.Run("failed adaptation exits 1 and keeps the old hash", func(t *testing.T) {
		h.reset()
		restore := h.withEnv("FIXTURE_REVIEW_FAIL", "1")
		defer restore()
		h.run(1, "update")
		if readAll(t, filepath.Join(h.root, lock.Lock)) != string(h.originalLock) {
			t.Fatal("failed adaptation advanced the accepted lock")
		}
		if body := readAll(t, adapted); !strings.Contains(body, "generic browser") {
			t.Fatal("failed adaptation changed the skill body")
		}
		if staged := h.git("diff", "--cached", "--name-only"); staged != "" {
			t.Fatalf("failed adaptation staged changes: %s", staged)
		}
	})

	t.Run("reviewer scope violation is refused", func(t *testing.T) {
		h.reset()
		restore := h.withEnv("FIXTURE_SCOPE", "1")
		defer restore()
		h.run(1, "update")
		if readAll(t, filepath.Join(h.root, lock.Lock)) != string(h.originalLock) {
			t.Fatal("scope violation advanced the accepted lock")
		}
		// The reviewer's edits are deliberately left in the worktree so the
		// maintainer can inspect them; what matters is that nothing was accepted.
		if _, err := os.Stat(filepath.Join(h.root, "scratch-notes.md")); err != nil {
			t.Fatal("expected the refused review to remain visible for inspection")
		}
		if staged := h.git("diff", "--cached", "--name-only"); staged != "" {
			t.Fatalf("scope violation staged changes: %s", staged)
		}
	})

	t.Run("reviewer is isolated and its output is exported", func(t *testing.T) {
		h.reset()
		result := h.run(0, "update")
		if str(result["report"]) == "" || str(result["report"]) == "<nil>" {
			t.Fatalf("expected a report path: %v", result["report"])
		}
		log := h.log()
		for _, want := range []string{"--no-context-files", "--no-skills", "--no-extensions",
			"--no-prompt-templates", "--no-session", "--no-approve", "--thinking",
			"opencode-go/space-bunny-free"} {
			if !strings.Contains(log, want) {
				t.Fatalf("reviewer was not isolated: %q missing from %q", want, log)
			}
		}
		if !strings.Contains(log, "PI_CODING_AGENT_DIR=") {
			t.Fatal("reviewer ran without an isolated configuration directory")
		}
		if !strings.Contains(log, "--thinking\nhigh") {
			t.Fatal("reviewer did not run at the documented thinking level")
		}
	})

	t.Run("intent-free import needs no reviewer", func(t *testing.T) {
		h.reset()
		restore := h.withEnv("FIXTURE_REVIEW_FAIL", "1")
		defer restore()
		saved := h.withEnv("OPENCODE_API_KEY", "")
		defer saved()
		os.Remove(filepath.Join(h.base, "pi.log"))
		h.run(0, "add", "fixture/source", "--skill", "new-skill")
		if _, ok := h.lockedSkills()["new-skill"]; !ok {
			t.Fatal("intent-free skill was not recorded")
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

	t.Run("remove keeps the intent file", func(t *testing.T) {
		h.commitAll()
		h.run(0, "remove", "new-skill")
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
			t.Fatal("remove deleted the intent document")
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
		got := h.run(0, "status")
		equal(t, list(got["skills"]), []string{"other"}, "status after record")
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
