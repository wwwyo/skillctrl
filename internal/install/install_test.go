package install_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wwwyo/skillctrl/internal/install"
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

// TestWorktreeIsolationCreatesACleanCheckout is the portable isolation path: the
// installer must never mutate the caller's checkout.
func TestWorktreeIsolationCreatesACleanCheckout(t *testing.T) {
	dir := newRepo(t)
	worktree, err := install.Worktree(dir, "git")
	if err != nil {
		t.Fatal(err)
	}
	if worktree == dir {
		t.Fatal("the main checkout was used as the isolation target")
	}
	for _, path := range []string{".git", ".agents/skills/manual/SKILL.md"} {
		if _, err := os.Stat(filepath.Join(worktree, filepath.FromSlash(path))); err != nil {
			t.Fatalf("the worktree is missing %s: %v", path, err)
		}
	}
	command := exec.Command("git", "status", "--porcelain")
	command.Dir = worktree
	if out, err := command.CombinedOutput(); err != nil || strings.TrimSpace(string(out)) != "" {
		t.Fatalf("the worktree is not clean: %s (%v)", out, err)
	}
	if err := os.WriteFile(filepath.Join(worktree, ".agents/skills/manual/SKILL.md"),
		[]byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(dir, ".agents/skills/manual/SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "body\n" {
		t.Fatal("the main checkout was mutated")
	}
}

func TestWorktreeCarriesPendingFilesWithoutChangingTheCaller(t *testing.T) {
	dir := newRepo(t)
	run := func(args ...string) string {
		t.Helper()
		command := exec.Command("git", args...)
		command.Dir = dir
		out, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
		return string(out)
	}
	path := ".agents/skills/manual/SKILL.md"
	if err := os.WriteFile(filepath.Join(dir, path), []byte("staged\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "--", path)
	if err := os.WriteFile(filepath.Join(dir, path), []byte("pending\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "new file.bin"), []byte{0, 255, 10}, 0o755); err != nil {
		t.Fatal(err)
	}
	staged, status, head := run("diff", "--cached", "--binary"), run("status", "--porcelain"), run("rev-parse", "HEAD")
	worktree, err := install.Worktree(dir, "git")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{path, "new file.bin"} {
		want, err := os.ReadFile(filepath.Join(dir, file))
		if err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(filepath.Join(worktree, file))
		if err != nil || string(got) != string(want) {
			t.Fatalf("pending file lost: %s: %q (%v)", file, got, err)
		}
	}
	info, err := os.Stat(filepath.Join(worktree, "new file.bin"))
	if err != nil || info.Mode().Perm()&0o111 == 0 {
		t.Fatal("pending executable mode lost")
	}
	if staged != run("diff", "--cached", "--binary") || status != run("status", "--porcelain") || head != run("rev-parse", "HEAD") {
		t.Fatal("isolation changed the caller's Git state")
	}
}

// TestWorktreeReusesAnExistingIsolationBoundary keeps a caller that is already
// working inside a linked worktree from nesting another one.
func TestWorktreeReusesAnExistingIsolationBoundary(t *testing.T) {
	dir := newRepo(t)
	inner, err := install.Worktree(dir, "git")
	if err != nil {
		t.Fatal(err)
	}
	// A linked worktree has a .git file rather than a .git directory.
	info, err := os.Stat(filepath.Join(inner, ".git"))
	if err != nil {
		t.Fatal(err)
	}
	if info.IsDir() {
		t.Fatal("expected a linked worktree with a .git file")
	}
	reused, err := install.Worktree(inner, "git")
	if err != nil {
		t.Fatal(err)
	}
	if reused != inner {
		t.Fatalf("a linked worktree was replaced: %s", reused)
	}
}

func TestWorktreeRejectsAnUnknownProvider(t *testing.T) {
	dir := newRepo(t)
	if _, err := install.Worktree(dir, "svn"); err == nil {
		t.Fatal("an unknown worktree provider was accepted")
	}
}
