// Package install drives the user-facing commands: fetch upstream originals,
// import them into the selected repository, and reconcile the accepted hashes.
package install

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/wwwyo/skillctrl/internal/gitx"
	"github.com/wwwyo/skillctrl/internal/lock"
	"github.com/wwwyo/skillctrl/internal/skillstate"
	"github.com/wwwyo/skillctrl/internal/upstream"
)

// Paths define the repository-local installation layout.
const (
	SkillsDir    = ".agents/skills"
	ClaudeDir    = ".claude/skills"
	UpstreamLock = upstream.Lock
)

// PrepareSkills creates missing skill directories in the selected repository and
// refuses symlinked directory ancestors.
func PrepareSkills(repo string) error {
	for _, relative := range []string{".agents", SkillsDir} {
		path := filepath.Join(repo, filepath.FromSlash(relative))
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			if err := os.Mkdir(path, 0o755); err != nil {
				return err
			}
		} else if err != nil {
			return err
		} else if !info.IsDir() {
			return fmt.Errorf("skills require a real directory: %s", relative)
		}
	}
	return nil
}

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
	prepared := filepath.Join(filepath.Dir(upstreamLock), upstream.PreparedNative)
	if _, err := os.Stat(prepared); err == nil {
		upstreamLock = prepared
	} else if !os.IsNotExist(err) {
		return err
	}
	tracking, trackingErr := os.ReadFile(filepath.Join(filepath.Dir(upstreamLock), upstream.PreparedTracking))
	if trackingErr != nil && !os.IsNotExist(trackingErr) {
		return trackingErr
	}
	var private *skillstate.Record
	if trackingErr == nil {
		var err error
		private, err = skillstate.Parse(tracking)
		if err != nil {
			return err
		}
		if err := upstream.ValidateRegistrations(private.Upstreams); err != nil {
			return err
		}
		if err := skillstate.ValidateDestination(repo); err != nil {
			return err
		}
		// Acquisition may take minutes. Validate current state before replacing
		// skills, and preserve acceptance recorded during preparation.
		latest, err := skillstate.Local(repo)
		if err != nil {
			return err
		}
		private.AcceptedHashes = latest.AcceptedHashes
	}
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
	replacements, err := protectPendingSkills(repo, target, names)
	if err != nil {
		return err
	}
	ordered := slices.Sorted(maps.Keys(names))
	intentRemovals := []string{}
	for _, name := range ordered {
		if afterSet[name] {
			continue
		}
		// Validate the entire path before importing anything; removal must not
		// follow an intent directory link outside this repository.
		for _, relative := range []string{".agents", ".agents/skillctrl", strings.TrimSuffix(lock.Intents, "/")} {
			info, err := os.Lstat(filepath.Join(repo, relative))
			if os.IsNotExist(err) {
				break
			}
			if err != nil {
				return err
			}
			if !info.IsDir() {
				return fmt.Errorf("intent path must be a real directory: %s", relative)
			}
		}
		path := filepath.Join(repo, lock.Intents, name+".md")
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("intent must be a regular file: %s", name)
		}
		intentRemovals = append(intentRemovals, path)
	}
	claude := filepath.Join(repo, filepath.FromSlash(ClaudeDir))
	if err := os.MkdirAll(claude, 0o755); err != nil {
		return err
	}
	for _, name := range ordered {
		link := filepath.Join(claude, name)
		if info, err := os.Lstat(link); err == nil && info.Mode()&os.ModeSymlink == 0 {
			return fmt.Errorf("Claude skill path is not a symlink: %s", name)
		}
	}
	for _, name := range ordered {
		destination := filepath.Join(existing, name)
		source := filepath.Join(target, name)
		if replacements[name] {
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
	for _, path := range intentRemovals {
		if err := os.Remove(path); err != nil {
			return err
		}
	}
	if trackingErr == nil {
		if err := skillstate.Write(repo, private); err != nil {
			return err
		}
	}
	if _, err := os.Stat(upstreamLock); err == nil {
		root := filepath.Join(repo, UpstreamLock)
		info, rootErr := os.Lstat(root)
		if rootErr != nil && !os.IsNotExist(rootErr) {
			return rootErr
		}
		if rootErr == nil && !info.Mode().IsRegular() {
			return fmt.Errorf("upstream lock must be a regular file: %s", UpstreamLock)
		}
		migrate := os.IsNotExist(rootErr)
		data, err := os.ReadFile(upstreamLock)
		if err != nil {
			return err
		}
		if err := os.WriteFile(root, data, 0o644); err != nil {
			return err
		}
		if migrate {
			if err := os.Remove(filepath.Join(repo, filepath.FromSlash(upstream.LegacyLock))); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
		return nil
	}
	return nil
}

// protectPendingSkills allows unrelated edits and unchanged originals, but a
// directory replacement must not silently discard an uncommitted customization.
func protectPendingSkills(repo, target string, names map[string]bool) (map[string]bool, error) {
	replacements := map[string]bool{}
	for _, name := range slices.Sorted(maps.Keys(names)) {
		current, err := fingerprint(filepath.Join(repo, filepath.FromSlash(SkillsDir), name))
		if err != nil {
			return nil, err
		}
		replacement, err := fingerprint(filepath.Join(target, name))
		if err != nil {
			return nil, err
		}
		if sameFingerprint(current, replacement) {
			continue
		}
		// A Git tree alone misses staged-only changes and ignored local files.
		// Disable optional index writes so the status check preserves caller staging.
		status, err := gitx.OutputEnv(repo, gitx.Environment("GIT_OPTIONAL_LOCKS=0"),
			"status", "--porcelain", "-z", "--ignored", "--untracked-files=all", "--", SkillsDir+"/"+name+"/")
		if err != nil {
			return nil, err
		}
		if len(status) > 0 {
			return nil, fmt.Errorf("import would overwrite pending edits to skill %s; preserve those edits before replacing it", name)
		}
		replacements[name] = true
	}
	return replacements, nil
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
func Selection(repo string) (lock.Selection, error) {
	tree, err := lock.WorkingTree(repo)
	if err != nil {
		return lock.Selection{}, err
	}
	current, err := lock.Snapshot(repo, tree)
	if err != nil {
		return lock.Selection{}, err
	}
	recorded, err := lock.Read(repo, tree)
	if err != nil {
		return lock.Selection{}, err
	}
	intents, err := Intents(repo)
	if err != nil {
		return lock.Selection{}, err
	}
	registered, err := upstream.Read(repo, tree)
	if err != nil {
		return lock.Selection{}, err
	}
	return lock.Select(current, recorded, intents, registered.ManagedSkills()), nil
}

// Intents reads saved intent names from the working copy.
func Intents(repo string) (map[string]bool, error) {
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
