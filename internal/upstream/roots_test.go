package upstream

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/wwwyo/skillctrl/internal/layout"
)

func TestDestinationDirsFollowsTheOwningSkill(t *testing.T) {
	repo := t.TempDir()
	for _, dir := range []string{"core/skills/bundle", "local/skills"} {
		if err := os.MkdirAll(filepath.Join(repo, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	config := filepath.Join(repo, layout.Config)
	if err := os.MkdirAll(filepath.Dir(config), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config, []byte(`{"version":1,"skillRoots":{"core":"core/skills","local":"local/skills"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	dirs, err := destinationDirs(repo, "bundle/references/input")
	if err != nil || !slices.Equal(dirs, []string{"core/skills/bundle/references/input"}) {
		t.Fatalf("merged input: %v %v", dirs, err)
	}
	dirs, err = destinationDirs(repo, "fresh")
	if err != nil || !slices.Equal(dirs, []string{"core/skills/fresh", "local/skills/fresh"}) {
		t.Fatalf("new skill: %v %v", dirs, err)
	}
}
