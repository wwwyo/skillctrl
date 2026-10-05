package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func mergeHarness(t *testing.T) *harness {
	t.Helper()
	h := newHarness(t)
	h.write(".agents/skillctrl/intents/combined.md", "Combine both skills; keep repository-local behavior.\n")
	h.commitAll()
	return h
}

func mergeArguments() []string {
	return []string{"merge", "--name", "combined", "fixture/source:manual", "fixture/source:new-skill"}
}

func mergedSources(t *testing.T, h *harness) []map[string]any {
	t.Helper()
	var document struct {
		Version int `json:"version"`
		Skills  map[string]struct {
			Sources []map[string]any `json:"sources"`
		} `json:"skills"`
	}
	if err := json.Unmarshal(h.read(".agents/skillctrl/upstreams.json"), &document); err != nil {
		t.Fatal(err)
	}
	if document.Version != 1 {
		t.Fatal("unsupported private tracking version")
	}
	return document.Skills["combined"].Sources
}

func TestMergeTracksEverySourceAndUpdatePreservesUnchangedOutput(t *testing.T) {
	h := mergeHarness(t)
	h.run(0, mergeArguments()...)
	h.write(".agents/skills/combined/SKILL.md", manifest("combined", "repository-local merged behavior"))
	h.run(0, "record", "combined")
	before := mergedSources(t, h)
	if len(before) != 2 || before[0]["skill"] != "manual" || before[1]["skill"] != "new-skill" {
		t.Fatalf("sources were not recorded in input order: %v", before)
	}
	if !bytes.Contains(h.read(".agents/skills/combined/SKILL.md"), []byte("repository-local merged behavior")) {
		t.Fatal("initial merge did not adapt the entrypoint")
	}
	h.commitAll()
	h.writeOrigin("skills/new-skill/SKILL.md", manifest("canonical-new-skill", "upstream v2; new behavior"))
	h.originGit("add", "-A")
	h.originGit("-c", "commit.gpgsign=false", "commit", "-qm", "second source changed")
	h.run(0, "update")
	after := mergedSources(t, h)
	if before[0]["skillFolderHash"] != after[0]["skillFolderHash"] || before[1]["skillFolderHash"] == after[1]["skillFolderHash"] {
		t.Fatal("update did not track the changed second source independently")
	}
	if !bytes.Contains(h.read(".agents/skills/combined/references/new-skill/SKILL.md"), []byte("upstream v2")) {
		t.Fatal("second-source update did not refresh the original")
	}
	h.commitAll()
	body, registration, calls := h.read(".agents/skills/combined/SKILL.md"), h.read("skills-lock.json"), h.log()
	h.run(0, "update", "combined")
	if !bytes.Equal(body, h.read(".agents/skills/combined/SKILL.md")) || !bytes.Equal(registration, h.read("skills-lock.json")) || calls != h.log() {
		t.Fatal("unchanged originals re-imported output or invoked review")
	}
	if h.git("status", "--porcelain") != "" {
		t.Fatal("unchanged update dirtied the worktree")
	}
	h.write(".agents/skillctrl/intents/combined.md", "Deliberately changed intent alone.\n")
	h.commitAll()
	h.run(0, "update", "combined")
	if calls != h.log() {
		t.Fatal("intent-only edit triggered adaptation")
	}
}

func TestMergeDryRunAndPreparationFailureDoNotWrite(t *testing.T) {
	h := mergeHarness(t)
	before := h.git("status", "--porcelain")
	result := h.run(0, append(mergeArguments(), "--dry-run")...)
	if len(result["sources"].([]any)) != 2 || before != h.git("status", "--porcelain") || h.log() != "" {
		t.Fatal("dry-run did not describe inputs without writes or review")
	}
	h.run(1, "merge", "--name", "combined", "fixture/source:manual", "fixture/source:linked")
	if !bytes.Equal(h.originalUpstream, h.read("skills-lock.json")) || before != h.git("status", "--porcelain") {
		t.Fatal("failure in the second source imported a partial result")
	}
	h.run(0, "merge", "--name", "unknown", "fixture/source:manual")
	if h.log() != "" {
		t.Fatal("intent-free routing invoked a reviewer")
	}
	h.reset()
	h.write(".gitignore", "*.local.*\n.agents/skills/combined/SKILL.md\n")
	h.git("add", "--", ".gitignore")
	h.commitAll()
	h.run(1, mergeArguments()...)
	if !bytes.Equal(h.originalUpstream, h.read("skills-lock.json")) || h.git("status", "--porcelain") != "" {
		t.Fatal("ignored merged entrypoint was imported")
	}
}

func TestFirstMergeUsesTheMainCheckout(t *testing.T) {
	h := mergeHarness(t)
	if err := os.RemoveAll(filepath.Join(h.root, ".agents/skills")); err != nil {
		t.Fatal(err)
	}
	h.write("skills-lock.json", `{"version":3,"skills":{}}`)
	h.write(".agents/skillctrl/intents/lock.json", `{"version":2,"skills":{}}`)
	h.commitAll()
	if err := os.MkdirAll(filepath.Join(h.root, ".agents/skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(h.root, ".git")); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(h.root, ".fixture-git"), filepath.Join(h.root, ".git")); err != nil {
		t.Fatal(err)
	}
	result := h.run(0, mergeArguments()...)
	working := result["repo"].(string)
	if working != h.root {
		t.Fatal("merge changed its mutation target")
	}
	if _, err := os.Stat(filepath.Join(working, ".agents/skills/combined/SKILL.md")); err != nil {
		t.Fatalf("first merge did not produce an entrypoint: %v", err)
	}

}
