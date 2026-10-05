package cli_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAddInputsTracksSeparateSourcesAndNames(t *testing.T) {
	h := newHarness(t)
	second := filepath.Join(h.base, "second-origin")
	runIn(t, h.base, nil, "git", "clone", "-q", h.origin, second)
	newBody := manifest("canonical-new-skill", "independent second repository")
	h.writeFile(filepath.Join(second, "skills/new-skill/SKILL.md"), newBody)
	runIn(t, second, nil, "git", "add", "-A")
	runIn(t, second, nil, "git", "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "-c", "commit.gpgsign=false", "commit", "-qm", "independent original")
	config, err := os.OpenFile(h.gitConfig, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	_, err = fmt.Fprintf(config, "[url \"file://%s\"]\n\tinsteadOf = https://github.com/fixture/second.git\n", second)
	config.Close()
	if err != nil {
		t.Fatal(err)
	}
	h.write("notes.md", "unrelated staged notes\n")
	h.git("add", "notes.md")
	index := h.read(".fixture-git/index")
	result := h.run(0, "add", "fixture/second:new-skill", "fixture/source:manual")
	equal(t, list(result["skills"]), []string{"manual", "new-skill"}, "separate imports")
	if string(h.read(".agents/skills/new-skill/SKILL.md")) != newBody || !strings.Contains(string(h.read(".agents/skills/manual/SKILL.md")), "upstream v1") {
		t.Fatal("input order or source identity changed imported originals")
	}
	if h.upstreamSkills()["new-skill"].(map[string]any)["source"] != "fixture/second" || h.upstreamSkills()["manual"].(map[string]any)["source"] != "fixture/source" {
		t.Fatal("separate source registrations lost their identity")
	}
	if !bytes.Equal(h.originalLock, h.acceptedBytes()) || !bytes.Equal(index, h.read(".fixture-git/index")) || h.log() != "" {
		t.Fatal("add inputs reviewed, accepted, or changed staging")
	}
	h.commitAll()
	h.run(0, "add", "fixture/second:new-skill", "--name", "renamed")
	if string(h.read(".agents/skills/renamed/SKILL.md")) != newBody || h.upstreamSkills()["renamed"].(map[string]any)["skill"] != "new-skill" {
		t.Fatal("named add inputs changed original bytes or source skill identity")
	}
}

func TestAddInputsRejectsInvalidInputsWithoutAcquisition(t *testing.T) {
	h := newHarness(t)
	if err := os.RemoveAll(h.origin); err != nil {
		t.Fatal(err)
	}
	before := h.git("status", "--porcelain")
	for _, args := range [][]string{
		{"fixture/source:new-skill", "fixture/other:new-skill"},
		{"fixture/source:new-skill", "fixture/other:NEW-SKILL"},
		{"fixture/source:new-skill", "fixture/source:new-skill"},
		{"fixture/source:../escape"},
		{"fixture/source:new-skill", "fixture/source:manual", "--name", "combined"},
	} {
		for _, dry := range []bool{false, true} {
			command := append([]string{"add"}, args...)
			if dry {
				command = append(command, "--dry-run")
			}
			_, stderr, code := h.try(command...)
			if code != 1 || strings.Contains(stderr, "clone") {
				t.Fatalf("invalid input attempted acquisition: %d %s", code, stderr)
			}
		}
	}
	h.run(0, "add", "fixture/source:new-skill", "--dry-run")
	if before != h.git("status", "--porcelain") || !bytes.Equal(h.originalUpstream, h.read("skills-lock.json")) || !bytes.Equal(h.originalLock, h.acceptedBytes()) || h.log() != "" {
		t.Fatal("invalid inputs or dry run changed project state")
	}
}

func TestAddInputsPreparesEveryInputBeforeImporting(t *testing.T) {
	for _, failure := range []string{"missing", "pending"} {
		t.Run(failure, func(t *testing.T) {
			h := newHarness(t)
			second := "missing"
			if failure == "pending" {
				second = "manual"
				h.upstreamSkill("manual", "canonical-manual", "changed upstream original")
				h.originGit("add", "-A")
				h.originGit("-c", "commit.gpgsign=false", "commit", "-qm", "new original")
				h.write(".agents/skills/manual/SKILL.md", manifest("manual", "pending customization"))
			}
			h.write("notes.md", "unrelated staged notes\n")
			h.git("add", "notes.md")
			before, index := h.git("status", "--porcelain"), h.read(".fixture-git/index")
			h.run(1, "add", "fixture/source:new-skill", "fixture/source:"+second)
			if before != h.git("status", "--porcelain") || !bytes.Equal(index, h.read(".fixture-git/index")) || !bytes.Equal(h.originalUpstream, h.read("skills-lock.json")) || !bytes.Equal(h.originalLock, h.acceptedBytes()) || h.log() != "" {
				t.Fatal("failed batch partially imported or accepted a skill")
			}
		})
	}
}
