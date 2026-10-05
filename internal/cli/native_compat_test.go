package cli_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
)

func TestNamedAndMergedRegistrationsLeaveNativeLockUntouched(t *testing.T) {
	h := newHarness(t)
	digest := sha256.Sum256([]byte("SKILL.md" + manifest("canonical-manual", "upstream v1; generic browser")))
	h.write("skills-lock.json", mustJSON(map[string]any{"version": 1, "skills": map[string]any{
		"manual": map[string]any{"source": "fixture/source", "sourceType": "github", "skillPath": "skills/manual/SKILL.md", "computedHash": hex.EncodeToString(digest[:])},
	}}))
	h.commitAll()
	native := h.read("skills-lock.json")
	index := h.read(".fixture-git/index")
	h.run(0, "add", "fixture/source:new-skill", "--name", "local-name")
	h.run(0, "merge", "fixture/source:manual", "fixture/source:new-skill", "--name", "combined")
	if !bytes.Equal(native, h.read("skills-lock.json")) || !bytes.Equal(index, h.read(".fixture-git/index")) {
		t.Fatal("skillctrl-only imports changed native registrations or caller staging")
	}
	entries := h.upstreamSkills()
	if entries["local-name"].(map[string]any)["skill"] != "new-skill" || len(entries["combined"].(map[string]any)["sources"].([]any)) != 2 {
		t.Fatal("private tracking lost the alias or ordered merged inputs")
	}
	h.commitAll()
	h.run(0, "remove", "local-name", "combined")
	if !bytes.Equal(native, h.read("skills-lock.json")) || len(h.upstreamSkills()) != 1 {
		t.Fatal("removing private registrations changed native registrations")
	}
}

func TestLegacyProvenanceMovesOutOfNativeLockOnlyOnImport(t *testing.T) {
	h := newHarness(t)
	var native map[string]any
	if err := json.Unmarshal(h.read("skills-lock.json"), &native); err != nil {
		t.Fatal(err)
	}
	entry := native["skills"].(map[string]any)["manual"].(map[string]any)
	entry["sourceCommit"] = h.originGit("rev-parse", "HEAD")
	entry["installedAt"] = "2026-01-01T00:00:00.000Z"
	entry["updatedAt"] = "2026-01-01T00:00:00.000Z"
	entry["nativeName"] = "manual"
	entry["future"] = map[string]any{"preserve": true}
	h.write("skills-lock.json", mustJSON(native))
	h.commitAll()
	original := h.read("skills-lock.json")
	h.run(0, "check")
	h.run(0, "--dry-run", "update")
	if !bytes.Equal(original, h.read("skills-lock.json")) {
		t.Fatal("read-only command migrated legacy fields")
	}
	h.run(0, "update", "manual")
	if err := json.Unmarshal(h.read("skills-lock.json"), &native); err != nil {
		t.Fatal(err)
	}
	project := native["skills"].(map[string]any)["manual"].(map[string]any)
	private := h.upstreamSkills()["manual"].(map[string]any)
	for _, key := range []string{"sourceCommit", "skillFolderHash", "installedAt", "updatedAt", "nativeName"} {
		if _, exists := project[key]; exists || private[key] != entry[key] {
			t.Fatalf("legacy provenance was not moved intact: %s", key)
		}
	}
	if project["future"].(map[string]any)["preserve"] != true {
		t.Fatal("migration removed unknown metadata")
	}
}

func TestOrdinaryNativeRegistrationWinsOverSupplementalMetadata(t *testing.T) {
	h := newHarness(t)
	h.writeOrigin("skills/new-skill/SKILL.md", manifest("new-skill", "native-compatible original"))
	h.originGit("add", "-A")
	h.originGit("-c", "commit.gpgsign=false", "commit", "-qm", "use canonical native name")
	h.run(0, "add", "fixture/source:new-skill")
	var native map[string]any
	if err := json.Unmarshal(h.read("skills-lock.json"), &native); err != nil {
		t.Fatal(err)
	}
	entries := native["skills"].(map[string]any)
	entry := entries["new-skill"].(map[string]any)
	for _, forbidden := range []string{"skill", "sources", "sourceLayout", "sourceCommit", "skillFolderHash", "installedAt", "updatedAt", "nativeExport", "nativeName"} {
		if _, exists := entry[forbidden]; exists {
			t.Fatalf("native registration acquired a skillctrl-only field: %s", forbidden)
		}
	}
	if len(entry["computedHash"].(string)) != 64 || entry["skillPath"] != "skills/new-skill/SKILL.md" {
		t.Fatal("ordinary import lost native restore metadata")
	}
	if h.upstreamSkills()["new-skill"].(map[string]any)["sourceCommit"] == nil {
		t.Fatal("private metadata lost immutable source provenance")
	}
	oldHash := entry["computedHash"]
	h.commitAll()
	h.writeOrigin("skills/new-skill/SKILL.md", manifest("new-skill", "native-compatible original v2"))
	h.originGit("add", "-A")
	h.originGit("-c", "commit.gpgsign=false", "commit", "-qm", "refresh ordinary original")
	h.run(0, "update", "new-skill")
	if err := json.Unmarshal(h.read("skills-lock.json"), &native); err != nil {
		t.Fatal(err)
	}
	entries = native["skills"].(map[string]any)
	entry = entries["new-skill"].(map[string]any)
	if entry["computedHash"] == oldHash {
		t.Fatal("ordinary update left the native content hash stale")
	}
	h.commitAll()
	nativeBytes := h.read("skills-lock.json")
	h.run(0, "update", "new-skill")
	if !bytes.Equal(nativeBytes, h.read("skills-lock.json")) {
		t.Fatal("unchanged ordinary update rewrote native metadata")
	}
	privateBytes := h.read(".agents/skillctrl/lock.json")
	var tracking map[string]any
	if err := json.Unmarshal(privateBytes, &tracking); err != nil {
		t.Fatal(err)
	}
	tracking["upstreams"].(map[string]any)["new-skill"].(map[string]any)["source"] = "fixture/other"
	h.write(".agents/skillctrl/lock.json", mustJSON(tracking))
	h.run(1, "check")
	h.write(".agents/skillctrl/lock.json", string(privateBytes))
	entry["computedHash"] = "changed-by-native-manager"
	h.write("skills-lock.json", mustJSON(native))
	h.run(0, "list")
	if h.upstreamSkills()["new-skill"].(map[string]any)["sourceCommit"] != nil {
		t.Fatal("stale metadata shadowed an independently updated native registration")
	}
	delete(entries, "new-skill")
	h.write("skills-lock.json", mustJSON(native))
	h.run(1, "update", "new-skill")
	if _, exists := h.upstreamSkills()["new-skill"]; exists {
		t.Fatal("supplemental metadata resurrected a native removal")
	}
}
