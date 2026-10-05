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
				"if [ \"$1\" = --version ]; then printf '1.7.0\\n'; exit 0; fi\n" +
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
			if result["local"].(map[string]any)["lock_changed"] != false {
				t.Fatalf("intent-free skill reported local drift: %v", result)
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

func TestCheckIgnoresUpstreamChangesAndUnavailableAdapters(t *testing.T) {
	h := newHarness(t)
	h.upstreamSkill("manual", "canonical-manual", "upstream v2; generic browser")
	h.originGit("add", "-A")
	h.originGit("commit", "-qm", "new original")
	if err := os.RemoveAll(h.origin); err != nil {
		t.Fatal(err)
	}
	for _, backend := range []string{"skills", "gh"} {
		path := filepath.Join(h.binDir, backend)
		h.writeFile(path, "#!/bin/sh\necho invoked >> '"+h.base+"/adapter.log'\nexit 19\n")
		if err := os.Chmod(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	before := h.git("status", "--porcelain")
	for _, backend := range []string{"skills", "gh", "git"} {
		result := h.run(0, "--adapter", backend, "check", "manual")
		if _, exists := result["updates"]; exists {
			t.Fatalf("local check reports upstream state: %v", result)
		}
		local := result["local"].(map[string]any)
		if local["lock_changed"] != false || len(list(local["skills"])) != 0 {
			t.Fatalf("accepted local content reported drift: %v", local)
		}
	}
	if _, err := os.Stat(filepath.Join(h.base, "adapter.log")); !os.IsNotExist(err) {
		t.Fatalf("check invoked an acquisition adapter: %v", err)
	}
	if h.git("status", "--porcelain") != before || !bytes.Equal(h.originalUpstream, h.read("skills-lock.json")) || h.log() != "" {
		t.Fatal("check changed project state or invoked reviewer")
	}
}

func TestCheckReportsSelectedLocalDriftWithoutAcceptingIt(t *testing.T) {
	h := newHarness(t)
	h.run(0, "add", "fixture/source", "--skill", "new-skill")
	h.commitAll()
	h.write(".agents/skills/manual/SKILL.md", manifest("manual", "manual local edit"))
	h.write(".agents/skills/new-skill/SKILL.md", manifest("new-skill", "new skill local edit"))
	h.write("notes.md", "unrelated staged notes\n")
	h.git("add", "--", "notes.md")
	index, accepted := h.read(".fixture-git/index"), h.acceptedBytes()
	before := h.git("status", "--porcelain")
	result := h.run(0, "check", "manual")
	local := result["local"].(map[string]any)
	equal(t, list(local["skills"]), []string{"manual"}, "selected local drift")
	equal(t, list(local["review_skills"]), []string{"manual"}, "selected intent review")
	if local["needs_review"] != true || local["lock_changed"] != true {
		t.Fatalf("local drift not reported: %v", local)
	}
	local = h.run(0, "check")["local"].(map[string]any)
	equal(t, list(local["skills"]), []string{"manual"}, "only intent-bound local drift")
	if h.git("status", "--porcelain") != before || !bytes.Equal(index, h.read(".fixture-git/index")) || !bytes.Equal(accepted, h.acceptedBytes()) || h.log() != "" {
		t.Fatal("check changed files, staging, acceptance, or invoked a reviewer")
	}
}

func TestAdapterErrorsDoNotFallBack(t *testing.T) {
	h := newHarness(t)
	for _, backend := range []string{"unknown", "skills"} {
		if backend == "skills" {
			h.writeFile(filepath.Join(h.binDir, "skills"), "#!/bin/sh\nif [ \"$1\" = --version ]; then printf '1.7.0\\n'; exit 0; fi\nexit 19\n")
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
