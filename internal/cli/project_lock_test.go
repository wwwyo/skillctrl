package cli_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestNativeProjectLockIsKeptAtTheRepositoryRoot(t *testing.T) {
	h := newHarness(t)
	h.writeOrigin("skills/new-skill/SKILL.md", manifest("new-skill", "native name"))
	h.writeOrigin("skills/manual/SKILL.md", manifest("manual", "upstream v1; generic browser"))
	h.originGit("add", "-A")
	h.originGit("-c", "commit.gpgsign=false", "commit", "-qm", "use native name")
	original := manifest("manual", "upstream v1; generic browser")
	digest := sha256.Sum256([]byte("SKILL.md" + original))
	h.write("skills-lock.json", mustJSON(map[string]any{
		"version": 1,
		"future":  map[string]any{"preserve": true},
		"skills": map[string]any{
			"manual":      map[string]any{"source": "fixture/source", "sourceType": "github", "skillPath": "skills/manual/SKILL.md", "computedHash": hex.EncodeToString(digest[:]), "subagents": []string{"root"}},
			"local-skill": map[string]any{"source": "./local", "sourceType": "local", "computedHash": "local-hash", "future": "keep"},
		},
	}))
	h.commitAll()
	beforeBody, beforeRegistration := h.read(".agents/skills/manual/SKILL.md"), h.read("skills-lock.json")
	h.run(0, "update")
	if string(h.read(".agents/skills/manual/SKILL.md")) != string(beforeBody) || h.log() != "" {
		t.Fatal("an unchanged native original overwrote the adaptation")
	}
	if string(h.read("skills-lock.json")) != string(beforeRegistration) {
		t.Fatal("an unchanged update rewrote the native registration")
	}
	h.run(0, "add", "fixture/source", "--skill", "new-skill")
	var root map[string]any
	if err := json.Unmarshal(h.read("skills-lock.json"), &root); err != nil {
		t.Fatal(err)
	}
	if root["version"] != float64(1) || root["future"].(map[string]any)["preserve"] != true {
		t.Fatal("project lock version or unknown top-level metadata was lost")
	}
	skills := root["skills"].(map[string]any)
	if skills["local-skill"].(map[string]any)["future"] != "keep" || skills["manual"].(map[string]any)["subagents"].([]any)[0] != "root" {
		t.Fatal("another tool's metadata was lost")
	}
	if len(skills["new-skill"].(map[string]any)["computedHash"].(string)) != 64 {
		t.Fatal("new source has no native content hash")
	}
	if _, err := os.Lstat(filepath.Join(h.root, ".agents/.skill-lock.json")); !os.IsNotExist(err) {
		t.Fatal("installer created the old lock location")
	}
}

func TestLegacyProjectLockMigratesOnlyAfterSuccessfulImport(t *testing.T) {
	h := newHarness(t)
	h.git("mv", "skills-lock.json", ".agents/.skill-lock.json")
	h.commitAll()
	before := h.read(".agents/.skill-lock.json")
	h.run(0, "check")
	h.run(0, "--dry-run", "update")
	h.run(1, "add", "fixture/source", "--skill", "missing")
	if string(h.read(".agents/.skill-lock.json")) != string(before) {
		t.Fatal("read-only or refused operation changed the legacy lock")
	}
	if _, err := os.Stat(filepath.Join(h.root, "skills-lock.json")); !os.IsNotExist(err) {
		t.Fatal("read-only or refused operation migrated the legacy lock")
	}
	index := h.read(".fixture-git/index")
	h.run(0, "update")
	if string(h.read(".fixture-git/index")) != string(index) {
		t.Fatal("migration changed staging")
	}
	if _, err := os.Stat(filepath.Join(h.root, ".agents/.skill-lock.json")); !os.IsNotExist(err) {
		t.Fatal("successful import kept the duplicate legacy lock")
	}
	if len(h.upstreamSkills()) != 1 {
		t.Fatal("migration lost source registrations")
	}
}

func TestExistingRootLockTakesPrecedenceOverTheLegacyLock(t *testing.T) {
	h := newHarness(t)
	h.write(".agents/.skill-lock.json", `{"version":3,"skills":{"stale":{"source":"other/source","sourceType":"github"}}}`)
	h.commitAll()
	h.run(0, "update", "manual")
	if len(h.upstreamSkills()) != 1 {
		t.Fatal("legacy entries were merged into the root lock")
	}
	h.write("skills-lock.json", "invalid root lock")
	h.run(1, "check")
	if string(h.read("skills-lock.json")) != "invalid root lock" {
		t.Fatal("invalid root lock was overwritten with legacy data")
	}
}
