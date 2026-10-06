package upstream

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// adapterRuntime resolves mise's tool directories in the caller's environment.
// Preserving HOME for the installer would also expose its global tracking state.
func adapterRuntime(ctx context.Context, name string, env []string) ([]string, error) {
	names := []string{name, "git"}
	if name == "skills" || name == "npx" {
		names = append(names, "node")
	}
	for _, tool := range names {
		executable, err := adapterLookPath(tool, env)
		if err != nil {
			continue
		}
		mise, err := filepath.EvalSymlinks(executable)
		if err != nil || filepath.Base(mise) != "mise" {
			continue
		}
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		command := exec.CommandContext(ctx, mise, "bin-paths")
		command.Env = env
		stderr := diagnosticTail{limit: 16 * 1024}
		command.Stderr = &stderr
		output, err := command.Output()
		if err != nil {
			return nil, fmt.Errorf("resolve adapter runtime with mise: %w\n%s", err, strings.TrimSpace(stderr.String()))
		}
		var directories []string
		for directory := range strings.SplitSeq(strings.TrimSpace(string(output)), "\n") {
			if !filepath.IsAbs(directory) {
				return nil, fmt.Errorf("mise bin-paths must return absolute tool directories")
			}
			directories = append(directories, directory)
		}
		var path []string
		for _, directory := range filepath.SplitList(adapterEnv(env, "PATH")) {
			if directory == filepath.Dir(executable) {
				// Keep earlier user wrappers ahead of managed tools, while making
				// child commands such as node resolve without the isolated shim.
				path = append(path, directories...)
			}
			path = append(path, directory)
		}
		env = append(removeEnv(env, "PATH"), "PATH="+strings.Join(path, string(os.PathListSeparator)))
		for _, tool := range names {
			resolved, err := adapterLookPath(tool, env)
			if err != nil {
				continue
			}
			if target, err := filepath.EvalSymlinks(resolved); err == nil && filepath.Base(target) == "mise" {
				return nil, fmt.Errorf("mise could not resolve adapter tool %q; install or activate it before acquisition", tool)
			}
		}
		return env, nil
	}
	return env, nil
}

func adapterLookPath(name string, env []string) (string, error) {
	for _, directory := range filepath.SplitList(adapterEnv(env, "PATH")) {
		if directory == "" {
			directory = "."
		}
		candidate := filepath.Join(directory, name)
		if !filepath.IsAbs(candidate) {
			candidate = "." + string(os.PathSeparator) + candidate
		}
		if executable, err := exec.LookPath(candidate); err == nil {
			if !filepath.IsAbs(executable) {
				return "", &exec.Error{Name: name, Err: exec.ErrDot}
			}
			return executable, nil
		}
	}
	return "", &exec.Error{Name: name, Err: exec.ErrNotFound}
}

func adapterEnv(env []string, key string) string {
	for index := len(env) - 1; index >= 0; index-- {
		if value, ok := strings.CutPrefix(env[index], key+"="); ok {
			return value
		}
	}
	return ""
}
