package cli_test

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAcquisitionResolvesMiseBeforeIsolatingInstallerState(t *testing.T) {
	for _, backend := range []string{"skills", "npx", "gh", "node", "path-override", "path-spelling"} {
		t.Run(backend, func(t *testing.T) {
			h := newHarness(t)
			tools, runtimes := filepath.Join(h.base, "tools"), filepath.Join(h.base, "runtimes")
			callerHome := filepath.Join(h.base, "fake-home")
			body := manifest("new-skill", "downloaded original")
			hash := fmt.Sprintf("%x", sha256.Sum256([]byte("SKILL.md"+body)))
			metadata := mustJSON(map[string]any{"version": 1, "skills": map[string]any{"new-skill": map[string]any{
				"source": "fixture/source", "sourceType": "github", "skillPath": "skills/new-skill/SKILL.md", "computedHash": hash,
			}}})
			h.executable("mise", "#!/bin/sh\nset -eu\n"+
				"[ \"$1\" = bin-paths ]\n[ \"$HOME\" = '"+callerHome+"' ]\n[ \"$PWD\" = '"+h.root+"' ]\n"+
				"printf '%s\\n' '"+tools+"' '"+runtimes+"'\n")
			h.writeFile(filepath.Join(runtimes, "node"), "#!/bin/sh\nset -eu\n[ \"$HOME\" != '"+callerHome+"' ]\nprintf 'fixture-node\\n'\n")
			if err := os.Chmod(filepath.Join(runtimes, "node"), 0o755); err != nil {
				t.Fatal(err)
			}
			name, adapter := backend, "skills"
			if backend == "node" || backend == "path-override" || backend == "path-spelling" {
				name = "skills"
			}
			if backend == "gh" {
				adapter = "gh"
			}
			script := "#!/bin/sh\nset -eu\n[ -z \"${OPENCODE_API_KEY:-}\" ]\n"
			if name == "gh" {
				script += "if [ \"$1\" = auth ]; then [ \"$HOME\" = '" + callerHome + "' ]; printf 'fixture-token\\n'; exit 0; fi\n[ \"$GH_TOKEN\" = fixture-token ]\n"
			}
			script += "[ \"$HOME\" != '" + callerHome + "' ]\n[ \"$PWD\" != '" + h.root + "' ]\n" +
				"[ \"$XDG_CONFIG_HOME\" = \"$HOME/.config\" ]\n[ \"$XDG_CACHE_HOME\" = \"$HOME/.cache\" ]\n[ \"$XDG_STATE_HOME\" = \"$HOME/.state\" ]\n" +
				"[ \"$(node --version)\" = fixture-node ]\n"
			if name == "skills" {
				script += "if [ \"$1\" = --version ]; then printf '1.7.0\\n'; exit 0; fi\n"
			}
			if name == "npx" {
				script += "[ \"$1\" = --yes ] && [ \"$2\" = --ignore-scripts ] && [ \"$3\" = skills@1.7.0 ]\nshift 3\n"
			}
			script += "mkdir -p .agents/skills/new-skill\ncat > .agents/skills/new-skill/SKILL.md <<'BODY'\n" + body + "BODY\n"
			if name == "gh" {
				script += "mkdir -p \"$HOME/.agents\"\ncat > \"$HOME/.agents/.skill-lock.json\" <<'LOCK'\n" + metadata + "\nLOCK\n"
			} else {
				script += "cat > skills-lock.json <<'LOCK'\n" + metadata + "\nLOCK\n"
			}
			tool := filepath.Join(tools, name)
			target := tool
			if backend == "node" {
				target = filepath.Join(tools, "cli.mjs")
				script += "[ \"${0##*/}\" = skills ]\n"
			}
			h.writeFile(target, script)
			if err := os.Chmod(target, 0o755); err != nil {
				t.Fatal(err)
			}
			if target != tool {
				if err := os.Symlink(target, tool); err != nil {
					t.Fatal(err)
				}
			}
			if backend == "npx" {
				h.executable("skills", "#!/bin/sh\nprintf '1.6.9\\n'\n")
				if err := os.Remove(filepath.Join(h.binDir, "npx")); err != nil {
					t.Fatal(err)
				}
			}
			if backend != "node" {
				if err := os.Symlink(filepath.Join(h.binDir, "mise"), filepath.Join(h.binDir, name)); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Symlink(filepath.Join(h.binDir, "mise"), filepath.Join(h.binDir, "node")); err != nil {
				t.Fatal(err)
			}
			path := h.binDir + string(os.PathListSeparator) + os.Getenv("PATH")
			if backend == "path-spelling" {
				path = h.binDir + string(os.PathSeparator) + string(os.PathListSeparator) + os.Getenv("PATH")
			}
			if backend == "node" {
				path = tools + string(os.PathListSeparator) + path
			}
			if backend == "path-override" {
				overrides := filepath.Join(h.base, "overrides")
				h.writeFile(filepath.Join(overrides, "node"), "#!/bin/sh\nprintf 'fixture-node\\n'\n")
				if err := os.Chmod(filepath.Join(overrides, "node"), 0o755); err != nil {
					t.Fatal(err)
				}
				h.writeFile(filepath.Join(runtimes, "node"), "#!/bin/sh\nprintf 'must-not-override-user-node\\n'\n")
				path = overrides + string(os.PathListSeparator) + path
			}
			h.env = append(h.env, "PATH="+path, "GH_TOKEN=", "GITHUB_TOKEN=")
			before := h.read(".fixture-git/index")
			stdout, stderr, code := h.try("--adapter", adapter, "add", "fixture/source:new-skill")
			if code != 0 || !strings.Contains(stdout, "new-skill") {
				t.Fatalf("acquisition through mise failed: %d %s %s", code, stdout, stderr)
			}
			if string(h.read(".agents/skills/new-skill/SKILL.md")) != body || string(h.read(".fixture-git/index")) != string(before) {
				t.Fatal("acquisition changed imported content or caller staging")
			}
			if _, err := os.Stat(filepath.Join(callerHome, ".agents/.skill-lock.json")); !os.IsNotExist(err) {
				t.Fatalf("installer wrote caller global tracking: %v", err)
			}
			if strings.Contains(stderr, "using npx") != (backend == "npx") {
				t.Fatalf("unexpected acquisition executable: %s", stderr)
			}
		})
	}
}

func TestMiseRuntimeResolutionFailurePreservesProject(t *testing.T) {
	for _, test := range []struct{ name, script, diagnostic string }{
		{"command-failure", "printf 'fixture resolution failed\\n' >&2\nexit 1\n", "resolve adapter runtime with mise"},
		{"empty-paths", "exit 0\n", "mise could not resolve adapter tool"},
		{"relative-path", "printf 'relative/bin\\n'\n", "mise bin-paths must return absolute tool directories"},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := newHarness(t)
			h.executable("mise", "#!/bin/sh\n"+test.script)
			if err := os.Symlink(filepath.Join(h.binDir, "mise"), filepath.Join(h.binDir, "skills")); err != nil {
				t.Fatal(err)
			}
			before := h.git("status", "--porcelain")
			stdout, stderr, code := h.try("--adapter", "skills", "add", "fixture/source:new-skill")
			if code != 1 || stdout != "" || !strings.Contains(stderr, test.diagnostic) {
				t.Fatalf("unexpected resolution failure: %d %s %s", code, stdout, stderr)
			}
			if h.git("status", "--porcelain") != before || string(h.read("skills-lock.json")) != string(h.originalUpstream) {
				t.Fatal("runtime resolution failure changed project state")
			}
		})
	}
}
