// Package gitx runs Git plumbing in an explicitly selected repository.
//
// Two entry points exist because the original design treats trust levels
// differently. Run is plain Git for operations on the caller's own checkout
// where the user's configuration is expected to apply. Safe disables hooks and
// the filesystem monitor for anything reading a possibly untrusted tree, so a
// malicious upstream cannot execute code through this tool.
package gitx

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// ExitError reports a non-zero Git exit status. Callers surface the status code
// rather than Git's own text so diagnostics stay stable across Git versions.
type ExitError struct {
	Args   []string
	Code   int
	Detail string
	Err    error
}

func (e *ExitError) Error() string {
	// Naming the failing plumbing command is what makes a status-only failure
	// actionable; Git itself prints nothing useful for several of them.
	message := fmt.Sprintf("%s exited with status %d", strings.Join(e.Args, " "), e.Code)
	if detail := strings.TrimSpace(e.Detail); detail != "" {
		// Git reports the specific refusal on stderr; surfacing it turns an
		// opaque status into an actionable message.
		return message + ": " + detail
	}
	return message
}

func (e *ExitError) Unwrap() error { return e.Err }

// run executes Git in dir and returns stdout. Stdin is supplied separately from
// the environment so callers can stage a private index without racing.
func run(dir string, env []string, stdin []byte, hardened bool, args []string) ([]byte, error) {
	full := []string{"git"}
	if hardened {
		// A fresh object write must not run hooks or consult the fsmonitor
		// daemon, which is not available in short-lived CI checkouts.
		full = append(full, "-c", "core.hooksPath=/dev/null", "-c", "core.fsmonitor=false")
	}
	full = append(full, args...)
	command := exec.Command(full[0], full[1:]...)
	if dir != "" {
		command.Dir = dir
	}
	command.Env = env
	if env == nil {
		command.Env = Environment()
	}
	if stdin != nil {
		command.Stdin = bytes.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		code := 1
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			code = exit.ExitCode()
		}
		return stdout.Bytes(), &ExitError{Args: full, Code: code,
			Detail: strings.TrimSpace(stderr.String()), Err: err}
	}
	return stdout.Bytes(), nil
}

// Environment drops inherited repository context before applying intentional
// overrides. Git hooks export this context, which otherwise takes precedence
// over a command's working directory and can redirect writes to another index.
func Environment(overrides ...string) []string {
	var environment []string
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		switch name {
		case "GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_CONFIG", "GIT_CONFIG_PARAMETERS", "GIT_CONFIG_COUNT",
			"GIT_OBJECT_DIRECTORY", "GIT_DIR", "GIT_WORK_TREE", "GIT_IMPLICIT_WORK_TREE", "GIT_GRAFT_FILE",
			"GIT_INDEX_FILE", "GIT_NO_REPLACE_OBJECTS", "GIT_REPLACE_REF_BASE", "GIT_PREFIX", "GIT_SHALLOW_FILE", "GIT_COMMON_DIR":
			continue
		}
		if strings.HasPrefix(name, "GIT_CONFIG_KEY_") || strings.HasPrefix(name, "GIT_CONFIG_VALUE_") {
			continue
		}
		environment = append(environment, entry)
	}
	return append(environment, overrides...)
}

// Output returns raw stdout from a Git command run in dir.
func Output(dir string, args ...string) ([]byte, error) {
	return run(dir, nil, nil, false, args)
}

// OutputEnv is Output with an explicit environment, used when the caller must
// add variables such as a private index file.
func OutputEnv(dir string, env []string, args ...string) ([]byte, error) {
	return run(dir, env, nil, false, args)
}

// Safe reads Git objects without invoking hooks or the filesystem monitor.
func Safe(dir string, args ...string) ([]byte, error) {
	return run(dir, nil, nil, true, args)
}

// SafeEnv is Safe with an explicit environment.
func SafeEnv(dir string, env []string, args ...string) error {
	_, err := run(dir, env, nil, true, args)
	return err
}

// Run executes a Git command whose failure only needs a status code.
func Run(dir string, args ...string) error {
	_, err := run(dir, nil, nil, false, args)
	return err
}

// RunEnv executes a Git command with an explicit environment, discarding output.
func RunEnv(dir string, env []string, args ...string) error {
	_, err := run(dir, env, nil, false, args)
	return err
}

// OutputErr runs a Git command with stdin and tolerates the small set of
// non-zero statuses Git documents for the command. check-ignore exits 1 when
// nothing matched, which is a result rather than a failure.
func OutputErr(dir string, args []string, stdin []byte, tolerated ...int) ([]byte, error) {
	out, err := run(dir, nil, stdin, false, args)
	if err == nil {
		return out, nil
	}
	var exit *ExitError
	if errors.As(err, &exit) {
		for _, code := range tolerated {
			if exit.Code == code {
				return out, nil
			}
		}
	}
	return out, err
}

// SafeRun executes a hardened Git command, discarding output.
func SafeRun(dir string, args ...string) error {
	_, err := run(dir, nil, nil, true, args)
	return err
}

// SafeStdin runs a hardened Git command with the given stdin.
func SafeStdin(dir string, stdin []byte, args ...string) ([]byte, error) {
	return run(dir, nil, stdin, true, args)
}

// Trimmed returns stdout without surrounding whitespace, matching the previous
// text-mode subprocess calls.
func Trimmed(out []byte) string { return strings.TrimSpace(string(out)) }
