package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallerPreservesUnrelatedPendingEditsAndStaging(t *testing.T) {
	for _, operation := range []string{"add", "update", "remove", "merge"} {
		t.Run(operation, func(t *testing.T) {
			h := newHarness(t)
			if operation == "merge" {
				h = mergeHarness(t)
			}
			h.write("notes.md", "committed notes\n")
			h.write("obsolete.md", "delete me\n")
			h.git("add", "--", "notes.md", "obsolete.md")
			h.commitAll()
			if operation == "update" {
				h.publishSecondVersion(t)
			}
			h.write("notes.md", "staged notes\n")
			h.write(".agents/skills/other/SKILL.md", "staged handwritten skill\n")
			h.git("add", "--", "notes.md", ".agents/skills/other/SKILL.md")
			h.write("notes.md", "unstaged notes\n")
			h.write(".agents/skills/other/SKILL.md", "unstaged handwritten skill\n")
			h.write("new file.bin", "\x00\xff\n")
			if err := os.Remove(filepath.Join(h.root, "obsolete.md")); err != nil {
				t.Fatal(err)
			}
			beforeIndex := h.read(".fixture-git/index")
			beforeHead := h.git("rev-parse", "HEAD")
			args := []string{operation}
			switch operation {
			case "add":
				args = append(args, "fixture/source", "--skill", "new-skill")
			case "update", "remove":
				args = append(args, "manual")
			case "merge":
				args = mergeArguments()
			}
			h.run(0, args...)
			if string(h.read(".fixture-git/index")) != string(beforeIndex) {
				t.Fatal("installer changed the caller's index")
			}
			if h.git("rev-parse", "HEAD") != beforeHead {
				t.Fatal("installer committed pending work")
			}
			if string(h.read("notes.md")) != "unstaged notes\n" || string(h.read("new file.bin")) != "\x00\xff\n" || string(h.read(".agents/skills/other/SKILL.md")) != "unstaged handwritten skill\n" {
				t.Fatal("unrelated pending content changed")
			}
			if _, err := os.Stat(filepath.Join(h.root, "obsolete.md")); !os.IsNotExist(err) {
				t.Fatal("unrelated deletion was restored")
			}
		})
	}
}

func TestInstallerRefusesOnlyReplacementsThatLosePendingSkillEdits(t *testing.T) {
	for _, operation := range []string{"add", "update", "remove"} {
		t.Run(operation, func(t *testing.T) {
			h := newHarness(t)
			h.publishSecondVersion(t)
			h.write(".agents/skills/manual/SKILL.md", manifest("manual", "pending customization"))
			h.git("add", "--", ".agents/skills/manual/SKILL.md")
			before := h.git("status", "--porcelain")
			index := h.read(".fixture-git/index")
			args := []string{operation, "manual"}
			if operation == "add" {
				args = []string{"add", "fixture/source", "--skill", "manual", "--skill", "new-skill"}
			}
			stdout, stderr, code := h.try(args...)
			if code != 1 || !strings.Contains(stdout+stderr, "pending edits to skill manual") {
				t.Fatalf("expected specific overlap refusal: %d %s %s", code, stdout, stderr)
			}
			if h.git("status", "--porcelain") != before || string(h.read(".fixture-git/index")) != string(index) {
				t.Fatal("overlap refusal was not atomic")
			}
		})
	}
}

func TestInstallerProtectsIgnoredAndStagedOnlySkillChanges(t *testing.T) {
	for _, kind := range []string{"ignored", "staged-only"} {
		for _, operation := range []string{"update", "remove"} {
			t.Run(kind+"/"+operation, func(t *testing.T) {
				h := newHarness(t)
				h.publishSecondVersion(t)
				body := h.read(".agents/skills/manual/SKILL.md")
				if kind == "ignored" {
					h.write(".agents/skills/manual/cache.local.bin", "private cached content\n")
				} else {
					h.write(".agents/skills/manual/SKILL.md", "staged customization\n")
					h.git("add", "--", ".agents/skills/manual/SKILL.md")
					h.write(".agents/skills/manual/SKILL.md", string(body))
				}
				before := h.read(".fixture-git/index")
				stdout, stderr, code := h.try(operation, "manual")
				if code != 1 || !strings.Contains(stdout+stderr, "pending edits to skill manual") {
					t.Fatalf("pending content not protected: %d %s %s", code, stdout, stderr)
				}
				if string(h.read(".fixture-git/index")) != string(before) || string(h.read(".agents/skills/manual/SKILL.md")) != string(body) {
					t.Fatal("overlap refusal changed existing work")
				}
				if kind == "ignored" && string(h.read(".agents/skills/manual/cache.local.bin")) != "private cached content\n" {
					t.Fatal("ignored local content was removed")
				}
			})
		}
	}
}

func TestReviewFailurePreservesUnrelatedStaging(t *testing.T) {
	for _, knob := range []string{"FIXTURE_REVIEW_FAIL=1", "FIXTURE_UNRESOLVED=1", "FIXTURE_SCOPE=1"} {
		t.Run(knob, func(t *testing.T) {
			h := newHarness(t)
			h.publishSecondVersion(t)
			h.write(".agents/skills/other/SKILL.md", "staged handwritten content\n")
			h.git("add", "--", ".agents/skills/other/SKILL.md")
			h.write(".agents/skills/other/SKILL.md", "unstaged handwritten content\n")
			index, accepted := h.read(".fixture-git/index"), h.read(".agents/skillctrl/intents/lock.json")
			h.knobs(knob)
			code := 1
			if knob == "FIXTURE_UNRESOLVED=1" {
				code = 2
			}
			h.run(code, "update", "manual")
			if string(h.read(".fixture-git/index")) != string(index) || string(h.read(".agents/skillctrl/intents/lock.json")) != string(accepted) {
				t.Fatal("failed or unresolved review changed staging or acceptance")
			}
			if string(h.read(".agents/skills/other/SKILL.md")) != "unstaged handwritten content\n" {
				t.Fatal("failed or unresolved review changed unrelated content")
			}
		})
	}
}
