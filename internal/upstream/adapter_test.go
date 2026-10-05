package upstream

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommandAdapterRejectsMissingTrackingPath(t *testing.T) {
	bin := t.TempDir()
	script := `#!/bin/sh
set -eu
if [ "$1" = --version ]; then printf '1.7.0\n'; exit 0; fi
mkdir -p .agents/skills/chosen
printf '%s\n' '---' 'name: chosen' '---' 'original' > .agents/skills/chosen/SKILL.md
cat > skills-lock.json <<'LOCK'
{"version":1,"skills":{"chosen":{"source":"fixture/source","sourceType":"github","computedHash":"unused"}}}
LOCK
`
	if err := os.WriteFile(filepath.Join(bin, "skills"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	adapter, err := NewAdapter("skills")
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	target := filepath.Join(directory, "result")
	_, err = adapter.Export(ExportRequest{Destination: newDestination(t, ""), Source: "fixture/source", Skill: "chosen", Name: "chosen", Target: target, Directory: directory})
	if err == nil || !strings.Contains(err.Error(), "non-empty skillPath") {
		t.Fatalf("missing tracking accepted: %v", err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("invalid tracking produced an import: %v", err)
	}
}

func TestGhAdapterResolvesCallerAuthWithoutPersistingToken(t *testing.T) {
	bin, callerHome := t.TempDir(), t.TempDir()
	script := `#!/bin/sh
set -eu
if [ "$1" = auth ]; then
  [ "$HOME" = "$FIXTURE_CALLER_HOME" ]
  printf '%s\n' fixture-keyring-token
  exit 0
fi
[ "$HOME" != "$FIXTURE_CALLER_HOME" ]
[ "${GH_TOKEN:-}" = fixture-keyring-token ]
mkdir -p .agents/skills/chosen "$HOME/.agents"
printf '%s\n' '---' 'name: chosen' '---' 'original' > .agents/skills/chosen/SKILL.md
cat > "$HOME/.agents/.skill-lock.json" <<'LOCK'
{"version":3,"skills":{"chosen":{"source":"fixture/source","sourceType":"github","skillPath":"skills/chosen/SKILL.md"}}}
LOCK
`
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("HOME", callerHome)
	t.Setenv("FIXTURE_CALLER_HOME", callerHome)
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	adapter, err := NewAdapter("gh")
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	target := filepath.Join(directory, "result")
	entry, err := adapter.Export(ExportRequest{Destination: newDestination(t, ""), Source: "fixture/source", Skill: "chosen", Name: "chosen", Target: target, Directory: directory})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(fmt.Sprint(entry), "fixture-keyring-token") {
		t.Fatal("credential stored in source tracking")
	}
	if err := filepath.WalkDir(directory, func(current string, item os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if item.Type().IsRegular() {
			data, err := os.ReadFile(current)
			if err != nil {
				return err
			}
			if bytes.Contains(data, []byte("fixture-keyring-token")) {
				t.Fatalf("credential persisted to %s", current)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestGitAdapterRespectsRegisteredRef(t *testing.T) {
	o := newOrigin(t)
	o.withRepository("fixture/source", o.dir)
	o.write("skills/chosen/SKILL.md", manifest("chosen", "pinned original"))
	o.commit("pinned original")
	o.git("tag", "stable")
	o.write("skills/chosen/SKILL.md", manifest("chosen", "new default branch original"))
	o.commit("default branch changed")
	withGitConfig(t, o.gitConfig)
	destination := newDestination(t, "")
	adapter := NewGitAdapter()
	for _, request := range []ExportRequest{
		{Destination: destination, Source: "fixture/source", Skill: "chosen", Name: "chosen", Previous: map[string]any{"source": "fixture/source", "ref": "stable"}},
		{Destination: destination, Source: "fixture/source", Skill: "chosen", Name: "chosen", Previous: map[string]any{"source": "fixture/different", "ref": "missing-stale-ref"}},
	} {
		request.Directory = t.TempDir()
		request.Target = filepath.Join(request.Directory, "result")
		entry, err := adapter.Export(request)
		if err != nil {
			t.Fatal(err)
		}
		body, err := os.ReadFile(filepath.Join(request.Target, "SKILL.md"))
		if err != nil {
			t.Fatal(err)
		}
		if request.Previous["source"] == "fixture/source" {
			if !strings.Contains(string(body), "pinned original") || entry["ref"] != "stable" {
				t.Fatalf("ref ignored: %s %v", body, entry)
			}
		} else if !strings.Contains(string(body), "new default branch original") || entry["ref"] != nil {
			t.Fatalf("retarget kept stale ref: %s %v", body, entry)
		}
	}
}

func TestDiagnosticTailDrainsBoundedOutput(t *testing.T) {
	tail := diagnosticTail{limit: 16}
	for _, value := range [][]byte{[]byte("prefix"), bytes.Repeat([]byte("x"), 100_000), []byte("final error")} {
		written, err := tail.Write(value)
		if err != nil || written != len(value) || len(tail.data) > 16 {
			t.Fatalf("writer did not drain bounded output: %d %v %d", written, err, len(tail.data))
		}
	}
	if !strings.HasSuffix(tail.String(), "final error") {
		t.Fatalf("lost final diagnostic: %q", tail.String())
	}
}
