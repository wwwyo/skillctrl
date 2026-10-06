package cli_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestFirstImportCreatesMissingSkillDirectories(t *testing.T) {
	for _, operation := range []struct {
		name string
		args []string
		body string
	}{
		{"add", []string{"add", "fixture/source:new-skill"}, ".agents/skills/new-skill/SKILL.md"},
		{"merge", mergeArguments(), ".agents/skills/combined/references/new-skill/SKILL.md"},
	} {
		for _, missing := range []string{".agents", ".agents/skills"} {
			t.Run(operation.name+"/"+missing, func(t *testing.T) {
				h := newHarness(t)
				if err := os.RemoveAll(filepath.Join(h.root, missing)); err != nil {
					t.Fatal(err)
				}
				h.write("skills-lock.json", `{"version":3,"skills":{}}`)
				h.commitAll()
				before := h.git("status", "--porcelain")
				result := h.run(0, append(operation.args, "--dry-run")...)
				if result["dry_run"] != true || h.git("status", "--porcelain") != before {
					t.Fatal("first-import dry run changed Git state")
				}
				if _, err := os.Lstat(filepath.Join(h.root, missing)); !os.IsNotExist(err) {
					t.Fatalf("dry run created missing directories: %v", err)
				}
				h.run(0, operation.args...)
				if !strings.Contains(string(h.read(operation.body)), "upstream v1") {
					t.Fatal("first import did not install the selected original")
				}
			})
		}
	}
}

func TestFirstImportRefusesDirectoriesOutsideGit(t *testing.T) {
	for _, args := range [][]string{{"add", "fixture/source:new-skill"}, mergeArguments()} {
		for _, dry := range []bool{false, true} {
			command := exec.Command(binaryPath(t), args...)
			if dry {
				command.Args = append(command.Args, "--dry-run")
			}
			command.Dir = t.TempDir()
			if output, err := command.CombinedOutput(); err == nil || !strings.Contains(string(output), "inside a Git repository") {
				t.Fatalf("first import outside Git did not fail cleanly: %v\n%s", err, output)
			}
			if _, err := os.Lstat(filepath.Join(command.Dir, ".agents")); !os.IsNotExist(err) {
				t.Fatalf("first import created directories outside Git: %v", err)
			}
		}
	}
}

func TestFirstImportRefusesSymlinkedAncestors(t *testing.T) {
	for _, ancestor := range []string{".agents", ".agents/skills"} {
		t.Run(ancestor, func(t *testing.T) {
			h := newHarness(t)
			outside := t.TempDir()
			if err := os.RemoveAll(filepath.Join(h.root, ancestor)); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, filepath.Join(h.root, ancestor)); err != nil {
				t.Fatal(err)
			}
			for _, args := range [][]string{{"add", "fixture/source:new-skill"}, mergeArguments()} {
				for _, dry := range []bool{false, true} {
					if dry {
						args = append(args, "--dry-run")
					}
					_, stderr, code := h.try(args...)
					if code != 1 || !strings.Contains(stderr, "skills require a real directory") {
						t.Fatalf("symlinked ancestor was not refused: exit %d\n%s", code, stderr)
					}
				}
			}
			entries, err := os.ReadDir(outside)
			if err != nil || len(entries) != 0 {
				t.Fatalf("first import wrote through a symlink: %v %v", entries, err)
			}
		})
	}
}
