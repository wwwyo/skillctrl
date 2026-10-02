package upstream

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/wwwyo/skillctrl/internal/gitx"
)

func TestSourcesArrayRejectsInvalidIdentitiesAndPreservesLegacyRecords(t *testing.T) {
	valid := map[string]any{"source": "owner/repo", "sourceType": "github", "skill": "first"}
	cases := []struct {
		name  string
		entry map[string]any
	}{
		{"empty", map[string]any{"sources": []any{}}},
		{"object instead of array", map[string]any{"sources": valid}},
		{"invalid element", map[string]any{"sources": []any{"owner/repo"}}},
		{"duplicate", map[string]any{"sources": []any{valid, valid}}},
		{"missing skill", map[string]any{"sources": []any{map[string]any{"source": "owner/repo", "sourceType": "github"}}}},
		{"credentials", map[string]any{"sources": []any{map[string]any{"source": "https://user:token@github.com/owner/repo", "sourceType": "github", "skill": "first"}}}},
		{"mixed", map[string]any{"source": "owner/repo", "sources": []any{valid}}},
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(entry map[string]any) {
		t.Helper()
		data, err := json.Marshal(Record{Version: Version, Skills: map[string]any{"combined": entry}})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, Lock), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			write(test.entry)
			if _, err := Load(dir); err == nil {
				t.Fatal("invalid sources array was accepted")
			}
		})
	}
	legacy := map[string]any{"source": "owner/repo", "sourceType": "github", "pluginName": "retained"}
	write(legacy)
	loaded, err := Load(dir)
	if err != nil || !reflect.DeepEqual(loaded.Skills["combined"], legacy) {
		t.Fatalf("legacy record was not preserved: %v %v", loaded, err)
	}
	write(map[string]any{"sources": []any{valid}, "extension": "retained"})
	if _, err := Load(dir); err != nil {
		t.Fatal(err)
	}
}

func TestMergedSourcesFromDifferentRepositoriesFollowRelocations(t *testing.T) {
	o := newOrigin(t)
	o.write("skills/first/SKILL.md", manifest("first", "first original"))
	o.commit("first original")
	o.withRepository("fixture/first", o.dir)
	second := newOrigin(t)
	second.write("skills/second/SKILL.md", manifest("second", "second original"))
	second.write("skills/second/run.sh", "#!/bin/sh\nexit 0\n")
	if err := os.Chmod(filepath.Join(second.dir, "skills/second/run.sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	second.commit("second original")
	o.withRepository("fixture/second", second.dir)
	withGitConfig(t, o.gitConfig)
	dir := newDestination(t, "")
	if err := os.MkdirAll(filepath.Join(dir, ".agents/skillctrl/intents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".agents/skillctrl/intents/combined.md"), []byte("Integrate both workflows."), 0o644); err != nil {
		t.Fatal(err)
	}
	inputs, err := ParseInputs([]string{"fixture/first:first", "https://github.com/fixture/second:second"})
	if err != nil {
		t.Fatal(err)
	}
	target, registration, err := Merge(dir, "combined", inputs, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// Promote the prepared files as a caller would, keeping this test independent
	// of the install package's separate link-handling contract.
	if err := copyTree(target, filepath.Join(dir, ".agents/skills")); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(registration)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, Lock), data, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "-A"}, {"commit", "-qm", "merged candidate"}} {
		if err := gitx.Run(dir, args...); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(second.dir, "moved"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(second.dir, "skills/second"), filepath.Join(second.dir, "moved/second")); err != nil {
		t.Fatal(err)
	}
	second.write("moved/second/SKILL.md", manifest("second", "second original v2"))
	second.commit("move and change second original")
	target, registration, err = Install(dir, "update", []string{"combined"}, "", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(registration)
	if err != nil || !strings.Contains(string(data), "moved/second/SKILL.md") {
		t.Fatalf("second repository relocation was not recorded: %s %v", data, err)
	}
	info, err := os.Stat(filepath.Join(target, "combined", SourceDirectory, "1/run.sh"))
	if err != nil || info.Mode().Perm()&0o111 == 0 {
		t.Fatal("executable mode was not preserved for an upstream input")
	}
	// A distributed merged package has one discoverable entrypoint; its originals
	// do not become unrelated skills in the source repository's skill index.
	if err := copyTree(target, filepath.Join(dir, ".agents/skills")); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "-A"}, {"commit", "-qm", "updated candidate"}} {
		if err := gitx.Run(dir, args...); err != nil {
			t.Fatal(err)
		}
	}
	o.withRepository("fixture/combined", dir)
	repository, err := New("fixture/combined", filepath.Join(t.TempDir(), "clone"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Select("combined", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Select("first", ""); err == nil {
		t.Fatal("original snapshot was exposed as an independent skill")
	}
}
