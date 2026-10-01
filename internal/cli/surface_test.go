package cli_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"
)

// runBinary invokes the built command and returns stdout, stderr and the exit
// code without asserting anything, so a test can state its own expectation.
func runBinary(t *testing.T, env []string, args ...string) (string, string, int) {
	t.Helper()
	command := exec.Command(binaryPath(t), args...)
	if env != nil {
		command.Env = env
	}
	var stdout, stderr strings.Builder
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	code := 0
	if err != nil {
		exit, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("run: %v", err)
		}
		code = exit.ExitCode()
	}
	return stdout.String(), stderr.String(), code
}

// TestArgumentErrorsAreReported guards the machine-readable contract at its
// edges: a caller that mistypes a flag or omits a required one must get a
// non-zero status, an explanation on stderr, and nothing on stdout that could be
// mistaken for a result.
func TestArgumentErrorsAreReported(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"unknown flag", []string{"status", "--nonsense"}, "unknown flag"},
		{"unknown command", []string{"install"}, "unknown command"},
		{"add without a source", []string{"add"}, "accepts 1 arg"},
		{"add without a skill", []string{"add", "owner/repo"}, "required flag"},
		{"remove without names", []string{"remove"}, "requires at least 1 arg"},
		{"plan without a base", []string{"plan"}, "requires --base"},
		{"extra arguments", []string{"status", "extra"}, "unknown command"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			stdout, stderr, code := runBinary(t, nil, test.args...)
			if code == 0 {
				t.Fatalf("expected a non-zero exit: %v", test.args)
			}
			if strings.TrimSpace(stderr) == "" {
				t.Fatalf("no explanation was written to stderr: %v", test.args)
			}
			// Argument errors are attributed prose; a command's own validation
			// reports the same failure as JSON. Both are machine-consumable.
			if !strings.HasPrefix(stderr, "skillctrl: ") && !strings.HasPrefix(stderr, "{") {
				t.Fatalf("unattributed error output: %q", stderr)
			}
			if test.want != "" && !strings.Contains(stderr, test.want) {
				t.Fatalf("error did not mention %q: %q", test.want, stderr)
			}
			if strings.TrimSpace(stdout) != "" {
				t.Fatalf("a usage error wrote to stdout: %q", stdout)
			}
		})
	}
}

// TestBareInvocationShowsHelp keeps the no-argument case useful rather than an
// error: someone who has only the binary should see what it can do.
func TestBareInvocationShowsHelp(t *testing.T) {
	stdout, stderr, code := runBinary(t, nil)
	if code != 0 {
		t.Fatalf("bare invocation exited %d: %s", code, stderr)
	}
	if !strings.Contains(stdout, "Usage:") {
		t.Fatalf("bare invocation printed no help: %q", stdout)
	}
}

// TestHelpIsDiscoverable keeps the documented surface reachable without a
// repository, since a new user has nothing but the binary.
func TestHelpIsDiscoverable(t *testing.T) {
	stdout, _, code := runBinary(t, nil, "--help")
	if code != 0 {
		t.Fatal("--help failed")
	}
	for _, command := range []string{"add", "update", "remove", "status", "record", "find", "schema", "plan"} {
		if !strings.Contains(stdout, "\n  "+command+" ") {
			t.Fatalf("help does not list %s:\n%s", command, stdout)
		}
	}
	stdout, _, code = runBinary(t, nil, "add", "--help")
	if code != 0 {
		t.Fatal("add --help failed")
	}
	if !strings.Contains(stdout, "--skill") || !strings.Contains(stdout, "--repo") {
		t.Fatalf("add help omits flags:\n%s", stdout)
	}
}

// TestSchemaDescribesTheContract keeps the machine-readable description of the
// commands and the exit codes in step with the command tree.
func TestSchemaDescribesTheContract(t *testing.T) {
	stdout, _, code := runBinary(t, nil, "schema")
	if code != 0 {
		t.Fatal("schema failed")
	}
	var schema struct {
		Commands map[string]string `json:"commands"`
		Options  []string          `json:"options"`
		Output   string            `json:"output"`
	}
	if err := json.Unmarshal([]byte(stdout), &schema); err != nil {
		t.Fatalf("schema is not one JSON document: %v\n%s", err, stdout)
	}
	for _, command := range []string{"add", "update", "remove", "record", "status", "find", "plan"} {
		if _, ok := schema.Commands[command]; !ok {
			t.Fatalf("schema omits %s", command)
		}
	}
	if !strings.Contains(schema.Output, "exit 2") {
		t.Fatalf("schema omits the unresolved exit code: %q", schema.Output)
	}
}

// TestDryRunIsRefusedWhereItCannotBeHonored keeps a flag from implying a
// guarantee the tool cannot make. The CI phases write the index, a lock, and a
// remote pull request, so they refuse rather than silently doing the work.
func TestDryRunIsRefusedWhereItCannotBeHonored(t *testing.T) {
	for _, phase := range [][]string{
		{"ci", "prepare", "/nonexistent-artifacts"}, {"ci", "export", "/nonexistent-artifacts"},
		{"ci", "apply", "/nonexistent-artifacts"}, {"ci", "publish", "/nonexistent-artifacts"},
		{"ci", "configure"}, {"schedule", "prepare", "/nonexistent-artifacts"},
		{"schedule", "restore", "/nonexistent-artifacts"}, {"schedule", "publish", "/nonexistent-artifacts"},
	} {
		arguments := append([]string{"--dry-run"}, phase...)
		stdout, stderr, code := runBinary(t, nil, arguments...)
		if code != 1 {
			t.Fatalf("%v exited %d want 1", phase, code)
		}
		if strings.TrimSpace(stdout) != "" {
			t.Fatalf("%v wrote to stdout: %q", phase, stdout)
		}
		if !strings.Contains(stderr, "--dry-run applies to") {
			t.Fatalf("%v did not explain the refusal: %q", phase, stderr)
		}
	}
	// The installer commands keep the meaning the flag advertises.
	if _, _, code := runBinary(t, nil, "--dry-run", "--repo", "/nonexistent", "update"); code != 1 {
		t.Fatalf("a dry-run update outside a repository should fail cleanly, got %d", code)
	}
}

// TestVersionReflectsTheInstalledModule pins the documented install path: a
// binary installed with `go install module@version` must report that version
// even though no linker flags were supplied.
func TestVersionReflectsTheInstalledModule(t *testing.T) {
	stdout, _, code := runBinary(t, nil, "--version")
	if code != 0 {
		t.Fatal("--version failed")
	}
	if !strings.Contains(stdout, "skillctrl version") {
		t.Fatalf("unexpected version output: %q", stdout)
	}
	// The test binary is built from a working tree, where no module version is
	// recorded; a module install must not report that as a released version.
	info, ok := debug.ReadBuildInfo()
	if !ok || info.Main.Version == "(devel)" {
		return
	}
	if !strings.Contains(stdout, info.Main.Version) {
		t.Fatalf("version %q does not match the installed module %q", stdout, info.Main.Version)
	}
}

// TestInjectedVersionWins keeps the release build's linker value authoritative.
func TestInjectedVersionWins(t *testing.T) {
	directory := t.TempDir()
	binary := filepath.Join(directory, "skillctrl")
	command := exec.Command("go", "build",
		"-ldflags", "-X github.com/wwwyo/skillctrl/internal/cli.Version=v9.9.9-test",
		"-o", binary, "github.com/wwwyo/skillctrl")
	if out, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	out, err := exec.Command(binary, "--version").CombinedOutput()
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "v9.9.9-test") {
		t.Fatalf("injected version was not reported: %s", out)
	}
}

// TestFailuresAreJSONOnStderr keeps a failure machine-readable while leaving
// stdout free for results.
func TestFailuresAreJSONOnStderr(t *testing.T) {
	h := newHarness(t)
	stdout, stderr, code := h.try("status", "--repo", filepath.Join(h.root, "missing"))
	if code != 1 {
		t.Fatalf("exit %d want 1", code)
	}
	if strings.TrimSpace(stdout) != "" {
		t.Fatalf("failure wrote to stdout: %q", stdout)
	}
	var failure map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(stderr)), &failure); err != nil {
		t.Fatalf("failure is not one JSON document: %v\n%s", err, stderr)
	}
	if failure["ok"] != false || failure["error"] == "" {
		t.Fatalf("unexpected failure payload: %v", failure)
	}
	if _, err := os.Stat(h.git("rev-parse", "--git-dir")); err != nil {
		t.Fatal(err)
	}
}
