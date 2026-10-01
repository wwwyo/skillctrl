package gitx

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommandsUseTheSelectedRepositoryAndExplicitIndex(t *testing.T) {
	fixture := func() string {
		dir := t.TempDir()
		command := exec.Command("git", "init", "-q", dir)
		command.Env = Environment()
		if out, err := command.CombinedOutput(); err != nil {
			t.Fatalf("init: %v: %s", err, out)
		}
		if err := os.WriteFile(filepath.Join(dir, "body"), []byte("original\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := Run(dir, "add", "body"); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	caller, target := fixture(), fixture()
	callerIndex := filepath.Join(caller, ".git", "index")
	before, err := os.ReadFile(callerIndex)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_DIR", filepath.Join(caller, ".git"))
	t.Setenv("GIT_WORK_TREE", caller)
	t.Setenv("GIT_COMMON_DIR", filepath.Join(caller, ".git"))
	t.Setenv("GIT_INDEX_FILE", callerIndex)
	t.Setenv("GIT_OBJECT_DIRECTORY", filepath.Join(caller, ".git", "objects"))
	root, err := Output(target, "rev-parse", "--show-toplevel")
	if err != nil {
		t.Fatal(err)
	}
	want, err := filepath.EvalSymlinks(target)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(root)) != want {
		t.Fatalf("Git used a foreign repository: %s", root)
	}
	private := filepath.Join(t.TempDir(), "index")
	if err := RunEnv(target, Environment("GIT_INDEX_FILE="+private), "add", "body"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(private); err != nil {
		t.Fatalf("intentional private index was not used: %v", err)
	}
	after, err := os.ReadFile(callerIndex)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("a foreign caller's index changed")
	}
}
