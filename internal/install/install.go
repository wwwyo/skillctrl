// Package install drives the user-facing commands: fetch upstream originals,
// import them into an isolated worktree, and reconcile the accepted hashes.
package install

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/wwwyo/skillctrl/internal/adapt"
	"github.com/wwwyo/skillctrl/internal/gitx"
	"github.com/wwwyo/skillctrl/internal/lock"
	"github.com/wwwyo/skillctrl/internal/upstream"
)

// Paths shared with the accepted lock and the CI workflow.
const (
	SkillsDir    = ".agents/skills"
	ClaudeDir    = ".claude/skills"
	UpstreamLock = upstream.Lock
)

// ProviderEnv selects the worktree isolation backend.
const ProviderEnv = "SKILLCTRL_WORKTREE_PROVIDER"

// Names rejects directory escapes and malformed skill identifiers.
func Names(values []string) ([]string, error) {
	for _, name := range values {
		if !upstream.Name(name) {
			return nil, fmt.Errorf("skill names must be plain directory names")
		}
	}
	// Deduplicating before sorting only collapses neighbours, so a repeated name
	// such as "add a b a" would survive and be processed twice.
	ordered := slices.Clone(values)
	slices.Sort(ordered)
	return slices.Compact(ordered), nil
}

// CheckSkills rejects symlinks before the importer can write through them or
// import escapes. A symlink inside a skill directory would let a later write
// land anywhere on the caller's filesystem.
func CheckSkills(path string) error {
	entries, err := os.ReadDir(path)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if _, err := Names([]string{entry.Name()}); err != nil {
			return err
		}
		info, err := os.Lstat(filepath.Join(path, entry.Name()))
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("expected a real skill directory: %s", entry.Name())
		}
		err = filepath.WalkDir(filepath.Join(path, entry.Name()), func(current string, item os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if item.Type()&os.ModeSymlink != 0 {
				return fmt.Errorf("installer isolation requires symlink-free skills: %s", entry.Name())
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// fingerprint compares file names, contents, and permissions before replacing a
// directory. Replacing an unchanged directory would touch its mtime and make the
// caller think something changed.
func fingerprint(directory string) (map[string][2]any, error) {
	if _, err := os.Stat(directory); err != nil {
		return nil, nil
	}
	result := map[string][2]any{}
	err := filepath.WalkDir(directory, func(current string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !entry.Type().IsRegular() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		data, err := os.ReadFile(current)
		if err != nil {
			return err
		}
		digest := sha256.Sum256(data)
		relative, err := filepath.Rel(directory, current)
		if err != nil {
			return err
		}
		result[filepath.ToSlash(relative)] = [2]any{info.Mode().Perm(), hex.EncodeToString(digest[:])}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func sameFingerprint(a, b map[string][2]any) bool {
	if len(a) != len(b) {
		return false
	}
	for name, value := range a {
		other, ok := b[name]
		if !ok || fmt.Sprint(value[0]) != fmt.Sprint(other[0]) || value[1] != other[1] {
			return false
		}
	}
	return true
}

// Import copies isolated importer results into the caller's worktree and
// reconciles the relative Claude links. Any real directory occupying a Claude
// link path is refused rather than replaced.
func Import(repo, target, upstreamLock string) error {
	existing := filepath.Join(repo, filepath.FromSlash(SkillsDir))
	before, err := os.ReadDir(existing)
	if err != nil {
		return err
	}
	after, err := os.ReadDir(target)
	if err != nil {
		return err
	}
	names := map[string]bool{}
	for _, entry := range before {
		names[entry.Name()] = true
	}
	afterSet := map[string]bool{}
	for _, entry := range after {
		afterSet[entry.Name()] = true
		names[entry.Name()] = true
	}
	claude := filepath.Join(repo, filepath.FromSlash(ClaudeDir))
	if err := os.MkdirAll(claude, 0o755); err != nil {
		return err
	}
	ordered := slices.Sorted(maps.Keys(names))
	for _, name := range ordered {
		link := filepath.Join(claude, name)
		if info, err := os.Lstat(link); err == nil && info.Mode()&os.ModeSymlink == 0 {
			return fmt.Errorf("Claude skill path is not a symlink: %s", name)
		}
	}
	for _, name := range ordered {
		destination := filepath.Join(existing, name)
		source := filepath.Join(target, name)
		current, err := fingerprint(destination)
		if err != nil {
			return err
		}
		replacement, err := fingerprint(source)
		if err != nil {
			return err
		}
		if !sameFingerprint(current, replacement) {
			if _, err := os.Stat(destination); err == nil {
				if err := os.RemoveAll(destination); err != nil {
					return err
				}
			}
			if afterSet[name] {
				if err := copyDirectory(source, destination); err != nil {
					return err
				}
			}
		}
		link := filepath.Join(claude, name)
		expected := filepath.Join("..", "..", filepath.FromSlash(SkillsDir), name)
		if afterSet[name] {
			if target, err := os.Readlink(link); err == nil && target == expected {
				continue
			}
		}
		if _, err := os.Lstat(link); err == nil {
			if err := os.Remove(link); err != nil {
				return err
			}
		}
		if afterSet[name] {
			if err := os.Symlink(expected, link); err != nil {
				return err
			}
		}
	}
	if _, err := os.Stat(upstreamLock); err == nil {
		data, err := os.ReadFile(upstreamLock)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(repo, filepath.FromSlash(UpstreamLock)), data, 0o644)
	}
	return nil
}

func copyDirectory(source, destination string) error {
	return filepath.WalkDir(source, func(current string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, current)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		switch {
		case entry.IsDir():
			return os.MkdirAll(target, 0o755)
		case entry.Type()&os.ModeSymlink != 0:
			return fmt.Errorf("installer isolation requires symlink-free skills: %s", relative)
		default:
			data, err := os.ReadFile(current)
			if err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(target, data, info.Mode().Perm()); err != nil {
				return err
			}
			return os.Chmod(target, info.Mode().Perm())
		}
	})
}

// Selection returns the working-copy hashes using a private index and the
// current intents.
func Selection(repo string) (lock.Plan, error) {
	tree, err := lock.WorkingTree(repo)
	if err != nil {
		return lock.Plan{}, err
	}
	current, err := lock.Snapshot(repo, tree)
	if err != nil {
		return lock.Plan{}, err
	}
	recorded, err := lock.Local(repo)
	if err != nil {
		return lock.Plan{}, err
	}
	head, err := gitx.Output(repo, "rev-parse", "HEAD")
	if err != nil {
		return lock.Plan{}, err
	}
	commit := gitx.Trimmed(head)
	intents, err := localIntents(repo)
	if err != nil {
		return lock.Plan{}, err
	}
	plan := lock.Select(current, recorded, intents)
	plan.Base = commit
	plan.Head = commit
	plan.Comparison = commit
	return plan, nil
}

func localIntents(repo string) (map[string]bool, error) {
	directory := filepath.Join(repo, filepath.FromSlash(lock.Intents))
	entries, err := os.ReadDir(directory)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]bool{}, nil
		}
		return nil, err
	}
	intents := map[string]bool{}
	for _, entry := range entries {
		if entry.Name() == "README.md" {
			continue
		}
		if name, ok := strings.CutSuffix(entry.Name(), ".md"); ok {
			intents[name] = true
		}
	}
	return intents, nil
}

// Adapt runs local adaptation and advances only its explicitly accepted hashes.
// Unresolved work stays in the worktree with its old hash so that the next run
// still sees the difference.
func Adapt(repo string, plan lock.Plan, directory, prompt string, configPath, modelsPath string) ([]string, error) {
	resolved := map[string]bool{}
	for _, name := range plan.Skills {
		resolved[name] = true
	}
	for _, name := range plan.ReviewSkills {
		delete(resolved, name)
	}
	if plan.NeedsReview {
		if err := adapt.PrepareWithPaths(repo, plan, plan.Head, directory, configPath); err != nil {
			return nil, err
		}
		options := adapt.Options{
			Dir: repo, Plan: plan, Directory: directory, Source: plan.Head,
			Prompt: prompt, ConfigPath: configPath, ModelsPath: modelsPath,
		}
		// The accepted/unresolved partition is read while the reviewed skills are
		// still staged: it describes the state the reviewer produced. Reading it
		// after the index is restored would compare against the pre-review commit.
		var accepted []string
		reviewErr := func() error {
			// Reviewing stages skill bodies; callers retain ownership of staging
			// and commits, so the index is restored even when the review fails.
			defer gitx.Run(repo, "reset", "--quiet", "HEAD", "--", ".")
			if err := adapt.ReviewLocal(options); err != nil {
				return err
			}
			names, err := adapt.Accepted(repo, plan, directory)
			accepted = names
			return err
		}()
		if reviewErr != nil {
			return nil, reviewErr
		}
		for _, name := range accepted {
			resolved[name] = true
		}
	}
	tree, err := lock.WorkingTree(repo)
	if err != nil {
		return nil, err
	}
	current, err := lock.Snapshot(repo, tree)
	if err != nil {
		return nil, err
	}
	recorded, err := lock.Local(repo)
	if err != nil {
		return nil, err
	}
	names := slices.Sorted(maps.Keys(resolved))
	if _, err := lock.Record(repo, current, recorded, names); err != nil {
		return nil, err
	}
	var unresolved []string
	for _, name := range plan.Skills {
		if !resolved[name] {
			unresolved = append(unresolved, name)
		}
	}
	return unresolved, nil
}

// Worktree returns a clean, isolated checkout for the installer to mutate.
//
// Three shapes exist: an existing linked worktree is reused, an Orca worktree
// is created when the caller asks for it, and otherwise a detached Git worktree
// is created in a temporary directory. The Git path is the default because it
// works in any repository without an extra tool; the Orca path is kept for
// callers that manage their worktrees that way.
func Worktree(repo, provider string) (string, error) {
	if info, err := os.Stat(filepath.Join(repo, ".git")); err == nil && !info.IsDir() {
		return repo, nil
	}
	if provider == "" {
		provider = os.Getenv(ProviderEnv)
	}
	if provider == "" {
		provider = "git"
	}
	status, err := gitx.Output(repo, "status", "--porcelain")
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(string(status)) != "" {
		return "", fmt.Errorf("main checkout has pending changes; use an existing isolated worktree")
	}
	switch provider {
	case "git":
		directory, err := os.MkdirTemp("", "skillctrl-worktree-")
		if err != nil {
			return "", err
		}
		target := filepath.Join(directory, "worktree")
		if err := gitx.Run(repo, "-c", "core.hooksPath=/dev/null", "-c", "core.fsmonitor=false",
			"worktree", "add", "--detach", "--quiet", target, "HEAD"); err != nil {
			return "", err
		}
		return target, nil
	case "orca":
		return orcaWorktree(repo)
	default:
		return "", fmt.Errorf("unknown worktree provider: %s", provider)
	}
}

func orcaWorktree(repo string) (string, error) {
	executable := os.Getenv("ORCA_CLI_COMMAND")
	if executable == "" {
		switch {
		case os.Getenv("ORCA_DEV_REPO_ROOT") != "":
			executable = "orca-dev"
		case runtime.GOOS == "linux" && os.Getenv("ORCA_TERMINAL_ID") == "":
			executable = "orca-ide"
		default:
			executable = "orca"
		}
	}
	name := "skillctrl-" + time.Now().UTC().Format("20060102-150405")
	out, err := exec.Command(executable, "worktree", "create", "--repo", "path:"+repo,
		"--name", name, "--no-parent", "--json").Output()
	if err != nil {
		return "", fmt.Errorf("Orca worktree creation failed")
	}
	path := orcaWorktreePath(out)
	if path == "" {
		return "", fmt.Errorf("Orca did not return an isolated worktree")
	}
	if info, err := os.Stat(filepath.Join(path, ".git")); err != nil || info.IsDir() {
		return "", fmt.Errorf("Orca did not return an isolated worktree")
	}
	return path, nil
}

func orcaWorktreePath(out []byte) string {
	var payload struct {
		Result struct {
			Worktree struct {
				Path string `json:"path"`
			} `json:"worktree"`
		} `json:"result"`
	}
	if err := json.Unmarshal(out, &payload); err != nil {
		return ""
	}
	if payload.Result.Worktree.Path == "" {
		return ""
	}
	absolute, err := filepath.Abs(payload.Result.Worktree.Path)
	if err != nil {
		return ""
	}
	return absolute
}
