package cli_test

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/wwwyo/skillctrl/internal/lock"
)

// harness builds a throwaway repository plus a fake upstream, then drives the
// real binary against them. Everything the test asserts about behavior is
// observed from outside the process: stdout JSON, exit codes, files on disk,
// and Git state.
type harness struct {
	t                *testing.T
	root             string
	origin           string
	binary           string
	binDir           string
	gitConfig        string
	base             string
	head             string
	originalLock     []byte
	originalUpstream []byte
	env              []string
}

var buildOnce sync.Once
var builtBinary string

// binaryPath compiles the command once per test run. Tests execute the built
// artifact rather than calling package functions so that exit codes, stdout,
// and stderr are exercised exactly as a user or CI job sees them.
func binaryPath(t *testing.T) string {
	t.Helper()
	buildOnce.Do(func() {
		directory, err := os.MkdirTemp("", "skillctrl-bin-")
		if err != nil {
			t.Fatal(err)
		}
		builtBinary = filepath.Join(directory, "skillctrl")
		command := exec.Command("go", "build", "-o", builtBinary, "github.com/wwwyo/skillctrl")
		command.Env = os.Environ()
		if out, err := command.CombinedOutput(); err != nil {
			t.Fatalf("build: %v\n%s", err, out)
		}
	})
	if builtBinary == "" {
		t.Fatal("binary was not built")
	}
	return builtBinary
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	base := t.TempDir()
	h := &harness{
		t:      t,
		root:   filepath.Join(base, "worktree"),
		origin: filepath.Join(base, "origin"),
		binary: binaryPath(t),
		binDir: filepath.Join(base, "bin"),
		base:   base,
	}
	if err := os.MkdirAll(h.root, 0o755); err != nil {
		t.Fatal(err)
	}
	h.git("init", "-q")
	h.git("config", "user.name", "Fixture")
	h.git("config", "user.email", "fixture@example.invalid")
	h.git("config", "commit.gpgsign", "false")
	// A gitdir file stands in for an existing worktree boundary without
	// contacting an external worktree manager.
	if err := os.Rename(filepath.Join(h.root, ".git"), filepath.Join(h.root, ".fixture-git")); err != nil {
		t.Fatal(err)
	}
	h.write(".git", "gitdir: .fixture-git\n")
	h.write(".fixture-git/info/exclude", ".fixture-git/\n")

	if err := os.MkdirAll(h.origin, 0o755); err != nil {
		t.Fatal(err)
	}
	runIn(t, h.origin, nil, "git", "init", "-q")
	runIn(t, h.origin, nil, "git", "config", "user.name", "Fixture")
	runIn(t, h.origin, nil, "git", "config", "user.email", "fixture@example.invalid")
	h.upstreamSkill("manual", "canonical-manual", "upstream v1; generic browser")
	h.upstreamSkill("new-skill", "canonical-new-skill", "upstream v1; generic browser")
	h.upstreamSkill("linked", "linked", "upstream v1; generic browser")
	h.upstreamSkill("rules", "rules", "upstream v1; generic browser")
	h.upstreamSkill("attributes", "attributes", "upstream v1; generic browser")
	h.upstreamSkill("ignored", "ignored", "upstream v1; generic browser")
	h.upstreamSymlink("linked/escape", "/tmp")
	h.writeOrigin("skills/rules/.gitignore", "reference.md\n")
	h.writeOrigin("skills/attributes/.gitattributes", "*.md text eol=lf\n")
	h.writeOrigin("skills/ignored/secret.local.md", "tracked by original, ignored by destination\n")
	h.originGit("add", "--", "skills")
	h.originGit("add", "--force", "--", "skills/ignored/secret.local.md")
	h.originGit("-c", "commit.gpgsign=false", "commit", "-qm", "original v1")
	originalTree := h.originGit("rev-parse", "HEAD:skills/manual")

	h.gitConfig = filepath.Join(base, "gitconfig")
	h.writeFile(h.gitConfig, fmt.Sprintf("[url \"file://%s\"]\n\tinsteadOf = https://github.com/fixture/source.git\n", h.origin))

	h.write(".gitignore", "*.local.*\n")
	h.git("add", "--", ".gitignore")
	for _, name := range []string{"manual", "other"} {
		h.write(".agents/skills/"+name+"/SKILL.md", manifest(name, "upstream v1; default browser"))
	}
	h.write(".agents/skillctrl/intents/manual.md", "use default browser\n")
	h.write("skills-lock.json", mustJSON(map[string]any{
		"version": 3,
		"skills": map[string]any{"manual": map[string]any{
			"source": "fixture/source", "sourceType": "github",
			"sourceUrl":       "https://github.com/fixture/source.git",
			"skillPath":       "skills/manual/SKILL.md",
			"skillFolderHash": originalTree,
			"pluginName":      "fixture-plugin",
		}},
	}))
	h.write("home/dot_config/mise/config.toml",
		"[tools]\nnode=\"1.2.3\"\n\"npm:@earendil-works/pi-coding-agent\"=\"4.5.6\"\n[settings]\npin=true\nminimum_release_age=\"7d\"\n")
	h.write("home/dot_pi/agent/models.json", "{}")
	h.commitAll()
	value, err := lock.Snapshot(h.root, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	delete(value.Skills, "other")
	h.write(lock.Lock, mustJSON(value))
	h.commitAll()
	h.head = h.git("rev-parse", "HEAD")
	h.originalLock = h.read(lock.Lock)
	h.originalUpstream = h.read("skills-lock.json")

	h.installFakes()
	h.env = append(os.Environ(),
		"PATH="+h.binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"OPENCODE_API_KEY=fixture-credential",
		"SKILLCTRL_FIXTURE_LOG="+filepath.Join(h.base, "pi.log"),
		"SKILLCTRL_ADAPTER=git",
		"GIT_CONFIG_GLOBAL="+h.gitConfig,
		"CLAUDE_CONFIG_DIR=/must-not-write",
		"CODEX_HOME=/must-not-write",
		"XDG_STATE_HOME=/must-not-write",
		"VIBE_HOME=/must-not-write",
		"APPDATA=/must-not-write",
		"HOME="+filepath.Join(base, "fake-home"),
	)
	return h
}

func manifest(name, body string) string {
	return "---\nname: " + name + "\n---\n" + body + "\n"
}

func (h *harness) git(args ...string) string {
	h.t.Helper()
	return runIn(h.t, h.root, nil, "git", args...)
}

func (h *harness) originGit(args ...string) string {
	h.t.Helper()
	return runIn(h.t, h.origin, nil, "git", args...)
}

func (h *harness) write(path, content string) {
	h.t.Helper()
	h.writeFile(filepath.Join(h.root, filepath.FromSlash(path)), content)
}

func (h *harness) writeFile(full, content string) {
	h.t.Helper()
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		h.t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		h.t.Fatal(err)
	}
}

func (h *harness) read(path string) []byte {
	h.t.Helper()
	data, err := os.ReadFile(filepath.Join(h.root, filepath.FromSlash(path)))
	if err != nil {
		h.t.Fatal(err)
	}
	return data
}

func (h *harness) upstreamSkill(directory, declared, body string) {
	h.t.Helper()
	full := filepath.Join(h.origin, "skills", directory)
	if err := os.MkdirAll(full, 0o755); err != nil {
		h.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(full, "SKILL.md"), []byte(manifest(declared, body)), 0o644); err != nil {
		h.t.Fatal(err)
	}
}

func (h *harness) upstreamSymlink(relative, target string) {
	h.t.Helper()
	if err := os.Symlink(target, filepath.Join(h.origin, "skills", filepath.FromSlash(relative))); err != nil {
		h.t.Fatal(err)
	}
}

func (h *harness) commitAll() {
	h.t.Helper()
	h.git("add", "--", ".agents", "home")
	if _, err := os.Lstat(filepath.Join(h.root, "skills-lock.json")); err == nil || h.git("ls-files", "--", "skills-lock.json") != "" {
		h.git("add", "-A", "--", "skills-lock.json")
	}
	if _, err := os.Stat(filepath.Join(h.root, ".claude")); err == nil {
		h.git("add", "--", ".claude")
	}
	h.git("-c", "commit.gpgsign=false", "commit", "-qm", "fixture")
}

// installFakes refuses accidental installer downloads or local reviewer execution.
func (h *harness) installFakes() {
	h.t.Helper()
	if err := os.MkdirAll(h.binDir, 0o755); err != nil {
		h.t.Fatal(err)
	}
	stub := func(name, script string) {
		full := filepath.Join(h.binDir, name)
		if err := os.WriteFile(full, []byte(script), 0o755); err != nil {
			h.t.Fatal(err)
		}
	}
	stub("npx", "#!/bin/sh\necho 'npx must never run' >&2\nexit 97\n")
	stub("mise", "#!/bin/sh\necho \"{\\\"PATH\\\":\\\"$PATH\\\"}\"\n")
	stub("pi", "#!/bin/sh\necho invoked >> \"$SKILLCTRL_FIXTURE_LOG\"\nexit 97\n")
}

func (h *harness) log() string {
	data, err := os.ReadFile(filepath.Join(h.base, "pi.log"))
	if err != nil {
		return ""
	}
	return string(data)
}

// run invokes the binary and asserts the exit code, returning the decoded stdout.
func (h *harness) run(exitCode int, args ...string) map[string]any {
	h.t.Helper()
	stdout, stderr, code := h.try(args...)
	if code != exitCode {
		h.t.Fatalf("skillctrl %v: exit %d want %d\nstdout: %s\nstderr: %s", args, code, exitCode, stdout, stderr)
	}
	if strings.TrimSpace(stdout) == "" {
		return nil
	}
	var value map[string]any
	if err := json.Unmarshal([]byte(stdout), &value); err != nil {
		h.t.Fatalf("stdout is not one JSON document: %v\n%s", err, stdout)
	}
	return value
}

func (h *harness) try(args ...string) (string, string, int) {
	h.t.Helper()
	command := exec.Command(h.binary, append([]string{"--repo", h.root}, args...)...)
	command.Env = h.env
	command.Dir = h.root
	var stdout, stderr strings.Builder
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	code := 0
	if err != nil {
		var exit *exec.ExitError
		if ok := asExit(err, &exit); ok {
			code = exit.ExitCode()
		} else {
			h.t.Fatalf("run: %v", err)
		}
	}
	return stdout.String(), stderr.String(), code
}

func (h *harness) reset() {
	h.t.Helper()
	h.git("reset", "--hard", h.head)
	h.git("clean", "-fd", "--", ".claude", ".agents/skills", "scratch-notes.md")
}

func (h *harness) skillTree(name string) string {
	h.t.Helper()
	return h.git("rev-parse", "HEAD:"+filepath.ToSlash(filepath.Join(".agents/skills", name)))
}

func (h *harness) lockedSkills() map[string]string {
	h.t.Helper()
	var value struct {
		Skills map[string]string `json:"skills"`
	}
	if err := json.Unmarshal(h.read(lock.Lock), &value); err != nil {
		h.t.Fatal(err)
	}
	return value.Skills
}

func (h *harness) upstreamSkills() map[string]any {
	h.t.Helper()
	var value struct {
		Skills map[string]any `json:"skills"`
	}
	if err := json.Unmarshal(h.read("skills-lock.json"), &value); err != nil {
		h.t.Fatal(err)
	}
	return value.Skills
}

func runIn(t *testing.T, dir string, env []string, name string, args ...string) string {
	t.Helper()
	command := exec.Command(name, args...)
	command.Dir = dir
	if env != nil {
		command.Env = env
	}
	out, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func lockBytes(t *testing.T, repo string) string {
	t.Helper()
	value, err := lock.Snapshot(repo, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	return mustJSON(value)
}

func mustJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(data)
}

func asExit(err error, target **exec.ExitError) bool {
	if exit, ok := err.(*exec.ExitError); ok {
		*target = exit
		return true
	}
	return false
}

func str(value any) string { return fmt.Sprint(value) }

func list(value any) []string {
	items, _ := value.([]any)
	result := make([]string, 0, len(items))
	for _, item := range items {
		result = append(result, str(item))
	}
	return result
}

func equal(t *testing.T, got []string, want []string, label string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: got %v want %v", label, got, want)
	}
	for index := range got {
		if got[index] != want[index] {
			t.Fatalf("%s: got %v want %v", label, got, want)
		}
	}
}
