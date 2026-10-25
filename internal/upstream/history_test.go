package upstream

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wwwyo/skillctrl/internal/gitx"
)

func TestUpdateHistoryFetchLoadsOnlyRequiredBlobs(t *testing.T) {
	o := newOrigin(t)
	o.git("config", "uploadpack.allowFilter", "true")
	o.git("config", "uploadpack.allowAnySHA1InWant", "true")
	o.write("skills/demo/SKILL.md", manifest("demo", "old original"))
	o.write("skills/demo/guide.md", "base guide\n")
	o.write("unrelated.bin", "unrelated repository content\n")
	o.commit("old original")
	tree := o.git("rev-parse", "HEAD:skills/demo")
	unrelated := o.git("rev-parse", "HEAD:unrelated.bin")
	o.withRepository("fixture/source", o.dir)
	withGitConfig(t, o.gitConfig)
	dir := newDestination(t, "")
	if err := os.MkdirAll(filepath.Join(dir, ".agents/skills/demo"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string]string{"SKILL.md": manifest("demo", "adapted original"), "guide.md": "base guide\n", "local.txt": "local only\n"} {
		if err := os.WriteFile(filepath.Join(dir, ".agents/skills/demo", name), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	registration, err := json.Marshal(map[string]any{"version": 1, "skills": map[string]any{"demo": map[string]any{
		"source": "fixture/source", "sourceType": "github", "skillPath": "skills/demo/SKILL.md", "skillFolderHash": tree,
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, Lock), registration, 0o644); err != nil {
		t.Fatal(err)
	}
	o.write("skills/demo/new.md", "new upstream file\n")
	o.commit("new original")
	stage := t.TempDir()
	target, _, err := Install(dir, "update", []string{"demo"}, "", stage)
	if err != nil {
		t.Fatal(err)
	}
	reports, err := ReadUpdates(stage)
	if err != nil || len(reports) != 1 || reports[0].Status != "merged" || reports[0].Base != "upstream-history" {
		t.Fatalf("history recovery: %v %v", reports, err)
	}
	for name, want := range map[string]string{"SKILL.md": manifest("demo", "adapted original"), "local.txt": "local only\n", "new.md": "new upstream file\n"} {
		got, err := os.ReadFile(filepath.Join(target, "demo", name))
		if err != nil || string(got) != want {
			t.Fatalf("%s: %q %v", name, got, err)
		}
	}
	histories, err := filepath.Glob(filepath.Join(stage, "history-*"))
	if err != nil || len(histories) != 1 {
		t.Fatalf("history storage: %v %v", histories, err)
	}
	objects, err := gitx.Safe(histories[0], "cat-file", "--batch-all-objects", "--batch-check=%(objectname)")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(objects), unrelated) {
		t.Fatal("partial history fetch downloaded an unrelated blob")
	}
	if _, err := os.Stat(filepath.Join(histories[0], "shallow")); !os.IsNotExist(err) {
		t.Fatal("history recovery retained a shallow boundary")
	}
}
