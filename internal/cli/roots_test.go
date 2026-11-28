package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// useRoots moves the fixture skills into two configured roots, as a repository
// that publishes one plugin per directory would keep them.
func useRoots(t *testing.T, h *harness) {
	t.Helper()
	for _, dir := range []string{"plugins/core/skills", "plugins/local/skills"} {
		if err := os.MkdirAll(filepath.Join(h.root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	h.git("mv", ".agents/skills/manual", "plugins/core/skills/manual")
	h.git("mv", ".agents/skills/other", "plugins/local/skills/other")
	h.write(".agents/skillctrl/config.json", `{"version":1,"skillRoots":{"core":"plugins/core/skills","local":"plugins/local/skills"}}`+"\n")
	h.commitAll()
}

func TestConfiguredSkillRoots(t *testing.T) {
	h := newHarness(t)
	useRoots(t, h)

	t.Run("list reports every root", func(t *testing.T) {
		result := h.run(0, "list")
		equal(t, list(result["skills"]), []string{"manual", "other"}, "listed skills")
		roots, _ := result["roots"].(map[string]any)
		if str(roots["manual"]) != "core" || str(roots["other"]) != "local" {
			t.Fatalf("roots: %v", result["roots"])
		}
	})

	t.Run("check hashes skills outside the default root", func(t *testing.T) {
		result := h.run(0, "check")
		local, _ := result["local"].(map[string]any)
		equal(t, list(local["skills"]), []string{}, "drifted skills")
		h.write("plugins/core/skills/manual/SKILL.md", manifest("manual", "upstream v1; edited"))
		result = h.run(0, "check")
		local, _ = result["local"].(map[string]any)
		equal(t, list(local["skills"]), []string{"manual"}, "drifted skills")
		h.git("checkout", "--", "plugins")
	})

	t.Run("a new skill needs a root when several exist", func(t *testing.T) {
		_, stderr, code := h.try("add", "fixture/source:new-skill")
		if code != 1 || !strings.Contains(stderr, "--root") {
			t.Fatalf("exit %d stderr %s", code, stderr)
		}
		if _, err := os.Stat(filepath.Join(h.root, "plugins/core/skills/new-skill")); !os.IsNotExist(err) {
			t.Fatal("a refused add wrote a skill")
		}
		_, stderr, code = h.try("add", "--root", "missing", "fixture/source:new-skill")
		if code != 1 || !strings.Contains(stderr, "unknown skill root") {
			t.Fatalf("exit %d stderr %s", code, stderr)
		}
	})

	t.Run("add imports into the chosen root", func(t *testing.T) {
		h.run(0, "add", "--root", "local", "fixture/source:new-skill")
		if body := readAll(t, filepath.Join(h.root, "plugins/local/skills/new-skill/SKILL.md")); !strings.Contains(body, "upstream v1") {
			t.Fatalf("imported skill: %q", body)
		}
		for _, stray := range []string{".agents/skills/new-skill", "plugins/core/skills/new-skill"} {
			if _, err := os.Stat(filepath.Join(h.root, stray)); !os.IsNotExist(err) {
				t.Fatalf("skill also written to %s", stray)
			}
		}
		link, err := os.Readlink(filepath.Join(h.root, ".claude/skills/new-skill"))
		if err != nil || link != filepath.Join("..", "..", "plugins", "local", "skills", "new-skill") {
			t.Fatalf("Claude link %q: %v", link, err)
		}
		if _, err := os.Stat(filepath.Join(h.root, ".claude/skills/manual")); err != nil {
			t.Fatalf("existing skill lost its Claude link: %v", err)
		}
	})

	t.Run("update keeps an existing skill in its root", func(t *testing.T) {
		h.publishSecondVersion(t)
		result := h.run(0, "update", "manual")
		updates, _ := result["updates"].([]any)
		if len(updates) != 1 || str(updates[0].(map[string]any)["path"]) != "plugins/core/skills/manual" {
			t.Fatalf("updates: %v", result["updates"])
		}
		if _, err := os.Stat(filepath.Join(h.root, "plugins/core/skills/manual/run.sh")); err != nil {
			t.Fatalf("update did not land in the existing root: %v", err)
		}
		if _, err := os.Stat(filepath.Join(h.root, ".agents/skills/manual")); !os.IsNotExist(err) {
			t.Fatal("update created a copy in the default root")
		}
	})

	t.Run("remove deletes from the skill's root", func(t *testing.T) {
		// An uncommitted import counts as a pending edit and is protected.
		h.git("add", "--", "plugins")
		h.commitAll()
		h.run(0, "remove", "new-skill")
		if _, err := os.Stat(filepath.Join(h.root, "plugins/local/skills/new-skill")); !os.IsNotExist(err) {
			t.Fatal("remove left the skill in place")
		}
		if _, err := os.Lstat(filepath.Join(h.root, ".claude/skills/new-skill")); !os.IsNotExist(err) {
			t.Fatal("remove left the Claude link")
		}
	})
}

func TestSkillRootsRefuseDuplicateNames(t *testing.T) {
	h := newHarness(t)
	useRoots(t, h)
	h.write("plugins/local/skills/manual/SKILL.md", manifest("manual", "a second copy"))
	h.git("add", "--", "plugins")
	h.commitAll()
	for _, args := range [][]string{{"list"}, {"check"}, {"update"}} {
		_, stderr, code := h.try(args...)
		if code != 1 || !strings.Contains(stderr, "manual") {
			t.Fatalf("%v: exit %d stderr %s", args, code, stderr)
		}
	}
}

func TestRootFlagRequiresConfiguration(t *testing.T) {
	h := newHarness(t)
	_, stderr, code := h.try("add", "--root", "core", "fixture/source:new-skill")
	if code != 1 || !strings.Contains(stderr, ".agents/skillctrl/config.json") {
		t.Fatalf("exit %d stderr %s", code, stderr)
	}
}
