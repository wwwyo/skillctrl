package cli_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mergeHarness(t *testing.T) *harness {
	t.Helper()
	h := newHarness(t)
	h.write(".agents/skillctrl/intents/combined.md", "Combine both skills; keep repository-local behavior.\n")
	h.commitAll()
	script := `#!/usr/bin/env bash
set -euo pipefail
. ` + h.base + `/reviewer.env
echo review >> "$SKILLCTRL_FIXTURE_LOG"
input=.agents/skills/combined/.skillctrl-sources
grep -q 'upstream v1' "$input/0/SKILL.md"
test -f "$input/1/SKILL.md"
result="${@: -1}"
result="${result##*Write completion JSON to: }"
if [ -n "${FIXTURE_UNRESOLVED:-}" ]; then
  printf '{"accepted":[],"unresolved":["combined"]}' > "$result"
else
  printf -- '---\nname: combined\ndescription: Combined workflow.\n---\nrepository-local merged behavior\n' > .agents/skills/combined/SKILL.md
  cat "$input/0/SKILL.md" "$input/1/SKILL.md" >> .agents/skills/combined/SKILL.md
  if [ -n "${FIXTURE_SCOPE:-}" ]; then
    echo forged-original >> "$input/0/SKILL.md"
  fi
  printf '{"accepted":["combined"],"unresolved":[]}' > "$result"
fi
echo checked both originals and saved intent
`
	if err := os.WriteFile(filepath.Join(h.binDir, "pi"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return h
}

func mergeArguments() []string {
	return []string{"merge", "combined", "--from", "fixture/source:manual", "--from", "fixture/source:new-skill"}
}

func mergedSources(t *testing.T, h *harness) []map[string]any {
	t.Helper()
	var document struct {
		Version int `json:"version"`
		Skills  map[string]struct {
			Sources []map[string]any `json:"sources"`
		} `json:"skills"`
	}
	if err := json.Unmarshal(h.read(".agents/.skill-lock.json"), &document); err != nil {
		t.Fatal(err)
	}
	if document.Version != 3 {
		t.Fatal("legacy lock version changed")
	}
	return document.Skills["combined"].Sources
}

func TestMergeTracksEverySourceAndUpdatePreservesUnchangedOutput(t *testing.T) {
	h := mergeHarness(t)
	h.run(0, mergeArguments()...)
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
	if !bytes.Contains(h.read(".agents/skills/combined/SKILL.md"), []byte("upstream v2")) {
		t.Fatal("second-source update did not reach the merged body")
	}
	h.commitAll()
	body, registration, calls := h.read(".agents/skills/combined/SKILL.md"), h.read(".agents/.skill-lock.json"), h.log()
	h.run(0, "update", "combined")
	if !bytes.Equal(body, h.read(".agents/skills/combined/SKILL.md")) || !bytes.Equal(registration, h.read(".agents/.skill-lock.json")) || calls != h.log() {
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

func TestMergeUnresolvedWorkKeepsAcceptedHashAndRetries(t *testing.T) {
	h := mergeHarness(t)
	h.run(0, mergeArguments()...)
	h.commitAll()
	accepted := h.read(".agents/skillctrl/intents/lock.json")
	h.writeOrigin("skills/new-skill/SKILL.md", manifest("canonical-new-skill", "upstream v2"))
	h.originGit("add", "-A")
	h.originGit("-c", "commit.gpgsign=false", "commit", "-qm", "update second original")
	h.knobs("FIXTURE_UNRESOLVED=1")
	result := h.run(2, "update", "combined")
	if fmt.Sprint(result["unresolved"]) != "[combined]" || !bytes.Equal(accepted, h.read(".agents/skillctrl/intents/lock.json")) {
		t.Fatal("unresolved merge advanced accepted hashes")
	}
	// The imported inputs remain available, but the old accepted baseline forces
	// another review even after those same inputs have been committed.
	h.commitAll()
	h.knobs()
	h.run(0, "update", "combined")
	if bytes.Equal(accepted, h.read(".agents/skillctrl/intents/lock.json")) {
		t.Fatal("resolved retry did not accept merged content")
	}
}

func TestMergeRefusesOriginalEditsAndKeepsAcceptanceAndStaging(t *testing.T) {
	h := mergeHarness(t)
	h.knobs("FIXTURE_SCOPE=1")
	stdout, stderr, code := h.try(mergeArguments()...)
	if code != 1 || !strings.Contains(stderr, "immutable upstream originals") {
		t.Fatalf("source edit was not refused: %d %s %s", code, stdout, stderr)
	}
	if !bytes.Equal(h.originalLock, h.read(".agents/skillctrl/intents/lock.json")) || h.git("diff", "--cached", "--name-only") != "" {
		t.Fatal("refused source edit changed acceptance or staging")
	}
}

func TestMergeDryRunAndPreparationFailureDoNotWrite(t *testing.T) {
	h := mergeHarness(t)
	before := h.git("status", "--porcelain")
	result := h.run(0, append(mergeArguments(), "--dry-run")...)
	if len(result["sources"].([]any)) != 2 || before != h.git("status", "--porcelain") || h.log() != "" {
		t.Fatal("dry-run did not describe inputs without writes or review")
	}
	h.run(1, "merge", "combined", "--from", "fixture/source:manual", "--from", "fixture/source:linked")
	if !bytes.Equal(h.originalUpstream, h.read(".agents/.skill-lock.json")) || before != h.git("status", "--porcelain") {
		t.Fatal("failure in the second source imported a partial result")
	}
	h.run(1, "merge", "unknown", "--from", "fixture/source:manual")
	if before != h.git("status", "--porcelain") {
		t.Fatal("missing intent failure changed the worktree")
	}
	h.write(".gitignore", "*.local.*\n.agents/skills/combined/SKILL.md\n")
	h.git("add", "--", ".gitignore")
	h.commitAll()
	h.run(1, mergeArguments()...)
	if !bytes.Equal(h.originalUpstream, h.read(".agents/.skill-lock.json")) || h.git("status", "--porcelain") != "" {
		t.Fatal("ignored merged entrypoint was imported")
	}
}

func TestFirstMergeInMainCheckoutRecreatesTheEmptySkillsDirectory(t *testing.T) {
	h := mergeHarness(t)
	if err := os.RemoveAll(filepath.Join(h.root, ".agents/skills")); err != nil {
		t.Fatal(err)
	}
	h.write(".agents/.skill-lock.json", `{"version":3,"skills":{}}`)
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
	result := h.run(0, append(mergeArguments(), "--worktree-provider", "git")...)
	working := result["repo"].(string)
	if working == h.root {
		t.Fatal("main checkout was used as the mutation target")
	}
	t.Cleanup(func() { os.RemoveAll(filepath.Dir(working)) })
	if _, err := os.Stat(filepath.Join(working, ".agents/skills/combined/SKILL.md")); err != nil {
		t.Fatalf("first merge did not produce an entrypoint: %v", err)
	}
	if _, err := os.Stat(filepath.Join(h.root, ".agents/skills/combined")); !os.IsNotExist(err) {
		t.Fatal("first merge changed the original checkout")
	}
}
