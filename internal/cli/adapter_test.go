package cli_test

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommandAdaptersPreserveProjectState(t *testing.T) {
	for _, backend := range []string{"skills", "gh"} {
		t.Run(backend, func(t *testing.T) {
			h := newHarness(t)
			body := strings.ReplaceAll(manifest("new-skill", "downloaded original"), "\n", "\r\n")
			attributes := filepath.Join(h.base, "global-attributes")
			h.writeFile(attributes, "*.md text eol=lf\n")
			file, err := os.OpenFile(h.gitConfig, os.O_APPEND|os.O_WRONLY, 0o644)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := fmt.Fprintf(file, "[core]\n attributesFile = %s\n", attributes); err != nil {
				t.Fatal(err)
			}
			file.Close()
			hash := fmt.Sprintf("%x", sha256.Sum256([]byte("SKILL.md"+body)))
			metadata := mustJSON(map[string]any{"version": 1, "skills": map[string]any{"new-skill": map[string]any{
				"source": "fixture/source", "sourceType": "github", "skillPath": "skills/new-skill/SKILL.md", "computedHash": hash,
			}}})
			// The executable observes actual arguments and isolation from outside
			// skillctrl. It emits the backend's native tracking location.
			script := "#!/bin/sh\nset -eu\n[ \"$1\" != auth ] || exit 1\n" +
				"[ -z \"${OPENCODE_API_KEY:-}\" ]\n" +
				"printf '%s\\n' \"$PWD\" \"$HOME\" \"$@\" >> '" + h.base + "/adapter.log'\n" +
				"mkdir -p .agents/skills/new-skill\ncat > .agents/skills/new-skill/SKILL.md <<'BODY'\n" + body + "BODY\n"
			if backend == "skills" {
				script += "cat > skills-lock.json <<'LOCK'\n" + metadata + "\nLOCK\n"
			} else {
				script += "mkdir -p \"$HOME/.agents\"\ncat > \"$HOME/.agents/.skill-lock.json\" <<'LOCK'\n" + metadata + "\nLOCK\n"
			}
			h.writeFile(filepath.Join(h.binDir, backend), script)
			if err := os.Chmod(filepath.Join(h.binDir, backend), 0o755); err != nil {
				t.Fatal(err)
			}
			h.write("notes.md", "staged notes\n")
			h.git("add", "notes.md")
			h.write("notes.md", "working notes\n")
			index := filepath.Join(h.root, ".fixture-git/index")
			before, err := os.ReadFile(index)
			if err != nil {
				t.Fatal(err)
			}
			h.run(0, "--adapter", backend, "install", "fixture/source", "--skill", "new-skill")
			if !bytes.Equal(h.read(".agents/skills/new-skill/SKILL.md"), []byte(body)) {
				t.Fatal("global Git attributes changed downloaded original")
			}
			entry := h.upstreamSkills()["new-skill"].(map[string]any)
			if entry["computedHash"] != hash || entry["skillPath"] != "skills/new-skill/SKILL.md" {
				t.Fatalf("wrong native registration: %v", entry)
			}
			if _, err := os.Stat(filepath.Join(h.root, ".agents/.skill-lock.json")); !os.IsNotExist(err) {
				t.Fatalf("project lock moved: %v", err)
			}
			originalLock := h.read("skills-lock.json")
			h.write(".agents/skills/new-skill/SKILL.md", manifest("new-skill", "local adaptation"))
			result := h.run(0, "--adapter", backend, "check", "new-skill")
			if len(list(result["updates"])) != 0 {
				t.Fatalf("unchanged original reported update: %v", result)
			}
			h.run(0, "--adapter", backend, "update", "new-skill")
			if !strings.Contains(string(h.read(".agents/skills/new-skill/SKILL.md")), "local adaptation") {
				t.Fatal("no-op update replaced adaptation")
			}
			if !bytes.Equal(originalLock, h.read("skills-lock.json")) {
				t.Fatal("unchanged registration rewritten")
			}
			after, err := os.ReadFile(index)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("caller staging changed: %v", err)
			}
			if string(h.read("notes.md")) != "working notes\n" {
				t.Fatal("unrelated edits lost")
			}
			log, err := os.ReadFile(filepath.Join(h.base, "adapter.log"))
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(log), h.root+"\n") || !strings.Contains(string(log), "adapter-") {
				t.Fatalf("installer not isolated: %s", log)
			}
			for _, argument := range map[string][]string{"skills": {"add", "--copy", "--yes", "--skill"}, "gh": {"skill", "install", "--dir", "--force"}}[backend] {
				if !strings.Contains(string(log), argument+"\n") {
					t.Fatalf("missing adapter argument %s: %s", argument, log)
				}
			}
			equal(t, list(h.run(0, "ls")["skills"]), []string{"manual", "new-skill", "other"}, "installed list")
		})
	}
}

func TestCheckReportsUpstreamChangesWithoutImport(t *testing.T) {
	h := newHarness(t)
	h.upstreamSkill("manual", "canonical-manual", "upstream v2; generic browser")
	h.originGit("add", "-A")
	h.originGit("commit", "-qm", "new original")
	before := h.git("status", "--porcelain")
	result := h.run(0, "check", "manual")
	equal(t, list(result["updates"]), []string{"manual"}, "upstream updates")
	if h.git("status", "--porcelain") != before || !bytes.Equal(h.originalUpstream, h.read("skills-lock.json")) || h.log() != "" {
		t.Fatal("check changed project state or invoked reviewer")
	}
}

func TestAdapterErrorsDoNotFallBack(t *testing.T) {
	h := newHarness(t)
	for _, backend := range []string{"unknown", "skills"} {
		if backend == "skills" {
			h.writeFile(filepath.Join(h.binDir, "skills"), "#!/bin/sh\nexit 19\n")
			os.Chmod(filepath.Join(h.binDir, "skills"), 0o755)
		}
		stdout, stderr, code := h.try("--adapter", backend, "add", "fixture/source", "--skill", "new-skill")
		if code != 1 || stdout != "" || !strings.Contains(stderr, "adapter") {
			t.Fatalf("unexpected error: %d %s %s", code, stdout, stderr)
		}
		if h.git("status", "--porcelain") != "" {
			t.Fatal("failed adapter changed caller")
		}
	}
}
