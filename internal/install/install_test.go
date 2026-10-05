package install_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wwwyo/skillctrl/internal/install"
	"github.com/wwwyo/skillctrl/internal/skillstate"
	"github.com/wwwyo/skillctrl/internal/upstream"
)

func newRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, arguments := range [][]string{
		{"init", "-q"},
		{"config", "user.name", "Fixture"},
		{"config", "user.email", "fixture@example.invalid"},
		{"config", "commit.gpgsign", "false"},
	} {
		command := exec.Command("git", arguments...)
		command.Dir = dir
		if out, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", arguments, err, out)
		}
	}
	if err := os.MkdirAll(filepath.Join(dir, ".agents/skills/manual"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".agents/skills/manual/SKILL.md"), []byte("body\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("git", "add", "-A")
	command.Dir = dir
	if out, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}
	command = exec.Command("git", "-c", "commit.gpgsign=false", "commit", "-qm", "fixture")
	command.Dir = dir
	if out, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v\n%s", err, out)
	}
	return dir
}

// TestImportRejectsInvalidPreparedProvenance guards the staged-data boundary:
// an invalid source must be refused before any existing skill is replaced.
func TestImportRejectsInvalidPreparedProvenance(t *testing.T) {
	for _, registration := range []string{
		`{"../escape":{"source":"fixture/source","sourceType":"github"}}`,
		`{"manual":{"source":"../escape","sourceType":"github"}}`,
		`{"manual":{"sources":[]}}`,
	} {
		t.Run(registration, func(t *testing.T) {
			repo, stage := newRepo(t), t.TempDir()
			target := filepath.Join(stage, "skills")
			if err := os.MkdirAll(filepath.Join(target, "manual"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(target, "manual/SKILL.md"), []byte("replacement\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			native := filepath.Join(stage, "skills-lock.json")
			if err := os.WriteFile(native, []byte(`{"version":1,"skills":{}}`), 0o644); err != nil {
				t.Fatal(err)
			}
			private := `{"version":1,"upstreams":` + registration + `,"acceptedHashes":{}}`
			if err := os.WriteFile(filepath.Join(stage, upstream.PreparedTracking), []byte(private), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := install.Import(repo, target, native); err == nil {
				t.Fatal("invalid prepared provenance was accepted")
			}
			body, err := os.ReadFile(filepath.Join(repo, ".agents/skills/manual/SKILL.md"))
			if err != nil || string(body) != "body\n" {
				t.Fatal("failed validation replaced the original")
			}
			for _, relative := range []string{skillstate.Lock, "skills-lock.json", ".claude"} {
				if _, err := os.Lstat(filepath.Join(repo, relative)); !os.IsNotExist(err) {
					t.Fatalf("failed import created %s", relative)
				}
			}
		})
	}
}

func TestPrepareSkillsRefusesSymlinkedAncestorsWithoutWritingOutside(t *testing.T) {
	for _, relative := range []string{".agents", ".agents/skills"} {
		t.Run(relative, func(t *testing.T) {
			dir, outside := t.TempDir(), t.TempDir()
			if relative == ".agents/skills" {
				if err := os.Mkdir(filepath.Join(dir, ".agents"), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Symlink(outside, filepath.Join(dir, filepath.FromSlash(relative))); err != nil {
				t.Fatal(err)
			}
			if err := install.PrepareSkills(dir); err == nil {
				t.Fatal("symlinked ancestor was accepted")
			}
			entries, err := os.ReadDir(outside)
			if err != nil || len(entries) != 0 {
				t.Fatal("directory preparation wrote through a symlink")
			}
		})
	}
}

// TestNamesRejectsEscapes is the first line of defense: a skill name that could
// point outside the skills directory never reaches the filesystem.
func TestNamesRejectsEscapes(t *testing.T) {
	accepted := []string{"a", "skill-1", "skill.name_2"}
	if got, err := install.Names([]string{"b", "a", "b", "a"}); err != nil || strings.Join(got, ",") != "a,b" {
		// Duplicates that are not adjacent must collapse too: deduplicating
		// before sorting only ever collapsed neighbours.
		t.Fatalf("names were not normalized: %v %v", got, err)
	}
	if got, _ := install.Names([]string{"a"}); len(got) != 1 || got[0] != "a" {
		t.Fatalf("a single name was altered: %v", got)
	}
	if _, err := install.Names(accepted); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"..", "../other", "/absolute", "a/b", ".hidden", "", "a b", "a\nb"} {
		if _, err := install.Names([]string{name}); err == nil {
			t.Fatalf("name %q was accepted", name)
		}
	}
}

// TestCheckSkillsRefusesSymlinks covers the isolation requirement: a symlink
// inside a skill directory would let a later write land anywhere on disk.
func TestCheckSkillsRefusesSymlinks(t *testing.T) {
	dir := newRepo(t)
	if err := install.CheckSkills(filepath.Join(dir, ".agents/skills")); err != nil {
		t.Fatalf("a clean skills directory was refused: %v", err)
	}
	link := filepath.Join(dir, ".agents/skills/manual/escape")
	if err := os.Symlink("/tmp", link); err != nil {
		t.Fatal(err)
	}
	err := install.CheckSkills(filepath.Join(dir, ".agents/skills"))
	if err == nil || !strings.Contains(err.Error(), "symlink-free") {
		t.Fatalf("a symlink inside a skill was accepted: %v", err)
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/tmp", filepath.Join(dir, ".agents/skills/linked")); err != nil {
		t.Fatal(err)
	}
	err = install.CheckSkills(filepath.Join(dir, ".agents/skills"))
	if err == nil || !strings.Contains(err.Error(), "real skill directory") {
		t.Fatalf("a symlinked skill directory was accepted: %v", err)
	}
}
