package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wwwyo/skillctrl/internal/lock"
)

func TestNamedAddAndIntentLifecycle(t *testing.T) {
	h := newHarness(t)
	original := h.read(lock.Lock)
	h.run(0, "add", "fixture/source", "--skill", "new-skill", "--name", "local-name")
	entry := h.upstreamSkills()["local-name"].(map[string]any)
	if entry["skill"] != "new-skill" || !bytes.Equal(original, h.read(lock.Lock)) || h.log() != "" {
		t.Fatal("named acquisition changed intent acceptance or lost upstream identity")
	}
	body := h.read(".agents/skills/local-name/SKILL.md")
	if !strings.Contains(string(body), "name: canonical-new-skill") {
		t.Fatal("named acquisition changed original bytes")
	}
	h.run(1, "record", "local-name")
	h.write("intent-input.md", "Preserve the local workflow.\n")
	h.run(0, "intent", "set", "local-name", "--file", filepath.Join(h.root, "intent-input.md"))
	if !bytes.Equal(original, h.read(lock.Lock)) || h.log() != "" {
		t.Fatal("saving intent reviewed or accepted content")
	}
	h.run(0, "record", "local-name")
	if h.lockedSkills()["local-name"] == "" {
		t.Fatal("intent-bearing named skill was not accepted")
	}
	h.commitAll()
	h.writeOrigin("skills/new-skill/SKILL.md", manifest("canonical-new-skill", "changed original"))
	h.originGit("add", "-A")
	h.originGit("-c", "commit.gpgsign=false", "commit", "-qm", "update named source")
	accepted := h.read(lock.Lock)
	h.run(0, "check", "local-name")
	h.run(0, "update", "local-name")
	if !strings.Contains(string(h.read(".agents/skills/local-name/SKILL.md")), "changed original") || !bytes.Equal(accepted, h.read(lock.Lock)) || h.log() != "" {
		t.Fatal("named update failed to refresh its original without accepting it")
	}
	h.run(0, "intent", "remove", "local-name")
	if _, ok := h.lockedSkills()["local-name"]; ok {
		t.Fatal("removed intent retained its accepted hash")
	}
	if _, ok := h.upstreamSkills()["local-name"]; !ok {
		t.Fatal("intent removal removed the upstream")
	}
	if _, err := os.Stat(filepath.Join(h.root, ".agents/skills/local-name/SKILL.md")); err != nil {
		t.Fatal(err)
	}
	h.run(0, "intent", "remove", "local-name")
	h.run(1, "record", "local-name")
}

func TestIntentApplyExplicitlyReviewsChangedIntentOnly(t *testing.T) {
	h := newHarness(t)
	h.write("intent-input.md", "Use the default browser and retain local behavior.\n")
	script := `#!/usr/bin/env bash
set -euo pipefail
. ` + h.base + `/reviewer.env
echo review >> "$SKILLCTRL_FIXTURE_LOG"
grep -q 'retain local behavior' .agents/skillctrl/intents/manual.md
result="${@: -1}"
result="${result##*Write completion JSON to: }"
printf '{"accepted":["manual"],"unresolved":[]}' > "$result"
`
	if err := os.WriteFile(filepath.Join(h.binDir, "pi"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	h.run(0, "intent", "set", "manual", "--file", filepath.Join(h.root, "intent-input.md"))
	h.run(0, "intent", "apply", "manual")
	if h.log() == "" {
		t.Fatal("explicit application skipped an unchanged skill")
	}
	h.run(1, "intent", "apply", "other")
	h.run(1, "--dry-run", "intent", "apply", "manual")
}

func TestPureMergeProducesRoutingWithoutIntent(t *testing.T) {
	h := newHarness(t)
	before := h.read(lock.Lock)
	h.run(0, "merge", "--name", "combined", "--from", "fixture/source:manual", "--from", "fixture/source:new-skill")
	body := string(h.read(".agents/skills/combined/SKILL.md"))
	for _, ref := range []string{".skillctrl-sources/0/SKILL.md", ".skillctrl-sources/1/SKILL.md"} {
		if !strings.Contains(body, "("+ref+")") {
			t.Fatalf("routing has no reference to %s", ref)
		}
		if _, err := os.Stat(filepath.Join(h.root, ".agents/skills/combined", ref)); err != nil {
			t.Fatal(err)
		}
	}
	if !bytes.Equal(before, h.read(lock.Lock)) || h.log() != "" {
		t.Fatal("pure merge reviewed or accepted content")
	}
	if _, err := os.Stat(filepath.Join(h.root, ".agents/skillctrl/intents/combined.md")); !os.IsNotExist(err) {
		t.Fatal("pure merge created intent")
	}
	h.commitAll()
	h.run(0, "merge", "combined", "--from", "fixture/source:new-skill", "--from", "fixture/source:manual")
	body = string(h.read(".agents/skills/combined/SKILL.md"))
	if !strings.Contains(body, "[new-skill](.skillctrl-sources/0/SKILL.md)") {
		t.Fatal("reconfigured merge kept outdated routing")
	}
	if !bytes.Equal(before, h.read(lock.Lock)) {
		t.Fatal("reconfigured merge accepted content")
	}
}

func TestIntentSetRejectsUnsafePathsAndEmptyInput(t *testing.T) {
	for _, kind := range []string{"empty", "name", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			h := newHarness(t)
			h.write("intent-input.md", "new requirements\n")
			name := "manual"
			switch kind {
			case "empty":
				h.write("intent-input.md", " \n")
			case "name":
				name = "../escape"
			case "symlink":
				target := filepath.Join(t.TempDir(), "external.md")
				if err := os.WriteFile(target, []byte("external\n"), 0o644); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(filepath.Join(h.root, lock.Intents, "manual.md")); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, filepath.Join(h.root, lock.Intents, "manual.md")); err != nil {
					t.Fatal(err)
				}
			}
			before := h.git("status", "--porcelain")
			h.run(1, "intent", "set", name, "--file", filepath.Join(h.root, "intent-input.md"))
			if before != h.git("status", "--porcelain") || !bytes.Equal(h.originalLock, h.read(lock.Lock)) || h.log() != "" {
				t.Fatal("rejected intent operation changed project state")
			}
		})
	}
}
