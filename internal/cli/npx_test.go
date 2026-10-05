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

func TestSkillsAcquisitionReusesCompatibleCLIOrPinnedNpx(t *testing.T) {
	for _, version := range []string{"missing", "missing-custom-cache", "1.7.0", "1.8.2", "v1.7.1", "1.6.9", "2.0.0", "broken", "1.7.0-beta.1"} {
		t.Run(version, func(t *testing.T) {
			h := newHarness(t)
			// Limit PATH to fixture tools, so an installed host skills cannot hide
			// the missing-CLI case or accidentally download from the registry.
			for _, name := range []string{"git", "mkdir", "cat"} {
				path := runIn(t, h.root, nil, "which", name)
				if err := os.Symlink(path, filepath.Join(h.binDir, name)); err != nil {
					t.Fatal(err)
				}
			}
			h.env = append(h.env, "PATH="+h.binDir)
			cacheVariable, cache := "npm_config_cache", filepath.Join(h.base, "fake-home/.npm")
			if version == "missing-custom-cache" {
				cacheVariable, cache = "NPM_CONFIG_CACHE", filepath.Join(h.base, "explicit-cache")
				h.env = append(h.env, cacheVariable+"="+cache)
			}
			body := manifest("new-skill", "downloaded original")
			hash := fmt.Sprintf("%x", sha256.Sum256([]byte("SKILL.md"+body)))
			metadata := mustJSON(map[string]any{"version": 1, "skills": map[string]any{"new-skill": map[string]any{
				"source": "fixture/source", "sourceType": "github", "skillPath": "skills/new-skill/SKILL.md", "computedHash": hash,
			}}})
			acquire := "[ -z \"${OPENCODE_API_KEY:-}\" ]\n" +
				"[ \"$PWD\" != '" + h.root + "' ]\n" +
				"mkdir -p .agents/skills/new-skill\ncat > .agents/skills/new-skill/SKILL.md <<'BODY'\n" + body + "BODY\n" +
				"cat > skills-lock.json <<'LOCK'\n" + metadata + "\nLOCK\n"
			if !strings.HasPrefix(version, "missing") {
				script := "#!/bin/sh\nset -eu\nif [ \"$1\" = --version ]; then printf '%s\\n' '" + version + "'; exit 0; fi\n" +
					"echo skills >> '" + h.base + "/acquisition.log'\n" + acquire
				h.executable("skills", script)
			}
			h.executable("npx", "#!/bin/sh\nset -eu\n"+
				"[ \"$1\" = --yes ] && [ \"$2\" = --ignore-scripts ] && [ \"$3\" = skills@1.7.0 ]\n"+
				"shift 3\n[ \"$1\" = add ]\n"+
				"[ \"$"+cacheVariable+"\" = '"+cache+"' ]\n"+
				"echo npx >> '"+h.base+"/acquisition.log'\n"+acquire)
			before := h.read(".fixture-git/index")
			stdout, stderr, code := h.try("--adapter", "skills", "add", "fixture/source:new-skill")
			if code != 0 || !strings.Contains(stdout, "new-skill") || !bytes.Equal(h.read(".agents/skills/new-skill/SKILL.md"), []byte(body)) {
				t.Fatalf("acquisition failed: %d %s %s", code, stdout, stderr)
			}
			log, err := os.ReadFile(filepath.Join(h.base, "acquisition.log"))
			if err != nil {
				t.Fatal(err)
			}
			want := "npx\n"
			if version == "1.7.0" || version == "1.8.2" || version == "v1.7.1" {
				want = "skills\n"
			}
			if string(log) != want || (strings.Contains(stderr, "using npx") != (want == "npx\n")) {
				t.Fatalf("unexpected resolver: %q %s", log, stderr)
			}
			if !bytes.Equal(before, h.read(".fixture-git/index")) {
				t.Fatal("acquisition changed caller index")
			}
		})
	}
}

func TestSkillsAcquisitionWithoutRuntimePreservesProject(t *testing.T) {
	h := newHarness(t)
	git := runIn(t, h.root, nil, "which", "git")
	if err := os.Symlink(git, filepath.Join(h.binDir, "git")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(h.binDir, "npx")); err != nil {
		t.Fatal(err)
	}
	h.env = append(h.env, "PATH="+h.binDir)
	before := h.git("status", "--porcelain")
	stdout, stderr, code := h.try("--adapter", "skills", "add", "fixture/source:new-skill")
	if code != 1 || stdout != "" || !strings.Contains(stderr, "Node.js/npm with npx") {
		t.Fatalf("missing runtime: %d %s %s", code, stdout, stderr)
	}
	if before != h.git("status", "--porcelain") || !bytes.Equal(h.originalUpstream, h.read("skills-lock.json")) {
		t.Fatal("missing runtime changed project")
	}
}

func (h *harness) executable(name, script string) {
	h.t.Helper()
	h.writeFile(filepath.Join(h.binDir, name), script)
	if err := os.Chmod(filepath.Join(h.binDir, name), 0o755); err != nil {
		h.t.Fatal(err)
	}
}
