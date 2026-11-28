package layout_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wwwyo/skillctrl/internal/layout"
)

func TestLoadDefaultsWithoutConfiguration(t *testing.T) {
	value, err := layout.Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if value.Configured() || len(value.Roots) != 1 || value.Roots[0].Path != layout.DefaultRoot {
		t.Fatalf("default layout: %+v", value)
	}
	root, err := value.Root("")
	if err != nil || root.Path != layout.DefaultRoot {
		t.Fatalf("only root: %+v %v", root, err)
	}
}

func TestParseOrdersRootsByName(t *testing.T) {
	value, err := layout.Parse([]byte(`{"version":1,"skillRoots":{"local":"plugins/local/skills","core":"plugins/core/skills"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(value.Names(), ","); got != "core,local" {
		t.Fatalf("names: %s", got)
	}
	if _, err := value.Root(""); err == nil || !strings.Contains(err.Error(), "--root") {
		t.Fatalf("ambiguous root: %v", err)
	}
	if root, err := value.Root("local"); err != nil || root.Path != "plugins/local/skills" {
		t.Fatalf("named root: %+v %v", root, err)
	}
}

func TestParseRejectsUnsafeRoots(t *testing.T) {
	for _, document := range []string{
		`{"version":2,"skillRoots":{"core":"a"}}`,
		`{"version":1,"skillRoots":{}}`,
		`{"version":1,"skillRoots":{"core":"/abs"}}`,
		`{"version":1,"skillRoots":{"core":"../outside"}}`,
		`{"version":1,"skillRoots":{"core":"a/../b"}}`,
		`{"version":1,"skillRoots":{"core":"."}}`,
		`{"version":1,"skillRoots":{"core":".git/hooks"}}`,
		`{"version":1,"skillRoots":{"core":"a\\b"}}`,
		`{"version":1,"skillRoots":{"../x":"a"}}`,
		`{"version":1,"skillRoots":{"core":"plugins","local":"plugins/local"}}`,
		`{"version":1,"skillRoots":{"core":"same","local":"same"}}`,
	} {
		if _, err := layout.Parse([]byte(document)); err == nil {
			t.Errorf("accepted %s", document)
		}
	}
}

func TestLocationsRefusesDuplicateNamesAndSkipsTooling(t *testing.T) {
	repo := t.TempDir()
	for _, dir := range []string{"a/skills/one", "a/skills/.codex-plugin", "b/skills/two"} {
		if err := os.MkdirAll(filepath.Join(repo, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	value, err := layout.Parse([]byte(`{"version":1,"skillRoots":{"a":"a/skills","b":"b/skills","c":"c/skills"}}`))
	if err != nil {
		t.Fatal(err)
	}
	locations, err := value.Locations(repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(locations) != 2 || locations["one"].Name != "a" || locations["two"].Name != "b" {
		t.Fatalf("locations: %+v", locations)
	}
	if err := os.MkdirAll(filepath.Join(repo, "b/skills/one"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := value.Locations(repo); err == nil || !strings.Contains(err.Error(), "one") {
		t.Fatalf("duplicate name: %v", err)
	}
}
