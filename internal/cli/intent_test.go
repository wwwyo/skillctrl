package cli_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
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
	h.write(".agents/skillctrl/intents/local-name.md", "Preserve the local workflow.\n")
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
	if err := os.Remove(filepath.Join(h.root, lock.Intents, "local-name.md")); err != nil {
		t.Fatal(err)
	}
	h.run(0, "record", "manual")
	if _, ok := h.lockedSkills()["local-name"]; ok {
		t.Fatal("removed intent retained its accepted hash")
	}
	if _, ok := h.upstreamSkills()["local-name"]; !ok {
		t.Fatal("intent removal removed the upstream")
	}
	if _, err := os.Stat(filepath.Join(h.root, ".agents/skills/local-name/SKILL.md")); err != nil {
		t.Fatal(err)
	}
	h.run(1, "record", "local-name")
}

func TestPureMergeProducesRoutingWithoutIntent(t *testing.T) {
	h := newHarness(t)
	before := h.read(lock.Lock)
	h.run(0, "merge", "--name", "combined", "fixture/source:manual", "fixture/source:new-skill")
	body := string(h.read(".agents/skills/combined/SKILL.md"))
	for _, ref := range []string{"references/manual/SKILL.md", "references/new-skill/SKILL.md"} {
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
	h.run(0, "merge", "--name", "combined", "fixture/source:new-skill", "fixture/source:manual")
	body = string(h.read(".agents/skills/combined/SKILL.md"))
	if !strings.Contains(body, "[new-skill](references/new-skill/SKILL.md)") {
		t.Fatal("reconfigured merge kept outdated routing")
	}
	if !bytes.Equal(before, h.read(lock.Lock)) {
		t.Fatal("reconfigured merge accepted content")
	}
}

func TestNamedMergePreservesUnregisteredReferencesAndRejectsCollisions(t *testing.T) {
	h := newHarness(t)
	h.run(0, "merge", "--name", "combined", "fixture/source:manual", "fixture/source:new-skill")
	h.write(".agents/skills/combined/references/guide.md", "Handwritten integration notes.\n")
	h.commitAll()
	h.run(0, "merge", "--name", "combined", "fixture/source:new-skill")
	if string(h.read(".agents/skills/combined/references/guide.md")) != "Handwritten integration notes.\n" {
		t.Fatal("remerge removed handwritten references")
	}
	if _, err := os.Stat(filepath.Join(h.root, ".agents/skills/combined/references/manual")); !os.IsNotExist(err) {
		t.Fatal("removed input retained its snapshot")
	}
	h.commitAll()
	before := h.git("status", "--porcelain")
	h.run(1, "merge", "--name", "combined", "fixture/source:new-skill", "fixture/other:new-skill")
	if before != h.git("status", "--porcelain") {
		t.Fatal("colliding input names changed project state")
	}
	h.write(".agents/skills/combined/references/manual/SKILL.md", "Handwritten reference.\n")
	h.commitAll()
	h.run(1, "merge", "--name", "combined", "fixture/source:manual")
	if string(h.read(".agents/skills/combined/references/manual/SKILL.md")) != "Handwritten reference.\n" {
		t.Fatal("merge overwrote an unregistered reference")
	}
}

func TestLegacyMergeUpdatesInPlaceAndExplicitMergeMigratesReferences(t *testing.T) {
	h := newHarness(t)
	h.run(0, "merge", "--name", "combined", "fixture/source:manual", "fixture/source:new-skill")
	if err := os.MkdirAll(filepath.Join(h.root, ".agents/skills/combined/.skillctrl-sources"), 0o755); err != nil {
		t.Fatal(err)
	}
	for index, name := range []string{"manual", "new-skill"} {
		if err := os.Rename(filepath.Join(h.root, ".agents/skills/combined/references", name), filepath.Join(h.root, ".agents/skills/combined/.skillctrl-sources", fmt.Sprint(index))); err != nil {
			t.Fatal(err)
		}
	}
	var registration map[string]any
	if err := json.Unmarshal(h.read("skills-lock.json"), &registration); err != nil {
		t.Fatal(err)
	}
	delete(registration["skills"].(map[string]any)["combined"].(map[string]any), "sourceLayout")
	h.write("skills-lock.json", mustJSON(registration))
	legacyBody := "Legacy routing to .skillctrl-sources/0/SKILL.md and .skillctrl-sources/1/SKILL.md.\n"
	h.write(".agents/skills/combined/SKILL.md", legacyBody)
	h.commitAll()
	h.writeOrigin("skills/new-skill/SKILL.md", manifest("canonical-new-skill", "updated legacy original"))
	h.originGit("add", "-A")
	h.originGit("-c", "commit.gpgsign=false", "commit", "-qm", "changed legacy source")
	h.run(0, "update", "combined")
	if string(h.read(".agents/skills/combined/SKILL.md")) != legacyBody || !strings.Contains(string(h.read(".agents/skills/combined/.skillctrl-sources/1/SKILL.md")), "updated legacy original") {
		t.Fatal("legacy update moved routing or failed to refresh source")
	}
	h.commitAll()
	if err := os.Remove(filepath.Join(h.root, ".agents/skills/combined/SKILL.md")); err != nil {
		t.Fatal(err)
	}
	h.commitAll()
	h.run(0, "update", "combined")
	if _, marked := h.upstreamSkills()["combined"].(map[string]any)["sourceLayout"]; marked {
		t.Fatal("legacy routing recovery changed the source layout marker")
	}
	if !strings.Contains(string(h.read(".agents/skills/combined/SKILL.md")), ".skillctrl-sources/0/SKILL.md") {
		t.Fatal("legacy routing recovery points to missing named references")
	}
	h.commitAll()
	h.run(0, "merge", "--name", "combined", "fixture/source:manual", "fixture/source:new-skill")
	if _, err := os.Stat(filepath.Join(h.root, ".agents/skills/combined/.skillctrl-sources")); !os.IsNotExist(err) {
		t.Fatal("explicit migration retained numeric originals")
	}
	if !strings.Contains(string(h.read(".agents/skills/combined/SKILL.md")), "references/new-skill/SKILL.md") {
		t.Fatal("migration did not update routing")
	}
}

func TestNamedScheduleRestoreRecomputesPlanWithoutEnvironmentPlan(t *testing.T) {
	h := newHarness(t)
	h.run(0, "merge", "--name", "combined", "fixture/source:manual", "fixture/source:new-skill")
	h.commitAll()
	base := h.git("rev-parse", "HEAD")
	h.publishSecondVersion(t)
	if err := os.WriteFile(filepath.Join(h.binDir, "gh"), []byte("#!/bin/sh\nprintf '[]'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	h.env = slices.DeleteFunc(h.env, func(value string) bool {
		return strings.HasPrefix(value, "SKILL_PLAN=") || strings.HasPrefix(value, "CHECKER_SOURCE=")
	})
	h.env = append(h.env, "CHECKER_SOURCE="+base)
	artifacts := filepath.Join(h.base, "schedule-artifacts")
	result := h.run(0, "schedule", "prepare", artifacts)
	if result["changed"] != true {
		t.Fatal("schedule did not prepare changed named references")
	}
	h.git("checkout", "--detach", base)
	restored := h.run(0, "schedule", "restore", artifacts)
	if restored["head"] == base || !strings.Contains(string(h.read(".agents/skills/combined/references/manual/SKILL.md")), "upstream v2") {
		t.Fatal("restore did not verify and restore named originals")
	}
	if h.log() != "" {
		t.Fatal("schedule restoration invoked a reviewer")
	}
}
