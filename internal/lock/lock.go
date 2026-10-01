// Package lock records accepted skill trees and selects the hashes that still
// need adaptation.
//
// Two locks exist by design. The upstream lock describes where a skill came
// from; the accepted lock in this package records what the maintainer has
// actually reviewed. Only Git tree object IDs are stored, so a hash covers the
// whole skill directory including body, references, scripts, and executable
// bits, and never covers the intent document itself.
package lock

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/wwwyo/skillctrl/internal/gitx"
	"github.com/wwwyo/skillctrl/internal/jsonfmt"
)

// Repository-relative locations. These are part of the on-disk contract and are
// shared with the CI workflow, so they are constants rather than flags.
const (
	Skills  = ".agents/skills/"
	Intents = ".agents/skillctrl/intents/"
	Lock    = Intents + "lock.json"
	Version = 2
)

// Entry is the hash-only accepted-content lock.
type Entry struct {
	Version int               `json:"version"`
	Skills  map[string]string `json:"skills"`
}

// Plan is the immutable selection handed to adaptation and to CI.
type Plan struct {
	Base         string            `json:"base"`
	Head         string            `json:"head"`
	Comparison   string            `json:"comparison"`
	Skills       []string          `json:"skills"`
	ReviewSkills []string          `json:"review_skills"`
	InputTrees   map[string]string `json:"input_trees"`
	NeedsReview  bool              `json:"needs_review"`
	LockChanged  bool              `json:"lock_changed"`
}

// Empty returns a plan with the collection fields initialized, so that a plan
// round-trips through JSON without turning empty lists into null. CI compares
// stored plans against recomputed ones, and null versus [] would read as a
// mismatch.
func Empty() Entry {
	return Entry{Version: Version, Skills: map[string]string{}}
}

func emptyPlan() Plan {
	return Plan{Skills: []string{}, ReviewSkills: []string{}, InputTrees: map[string]string{}}
}

// Entry is one result of a Git tree listing.
type entry struct {
	kind string
	oid  string
}

func entries(dir, ref, path string, recursive bool) (map[string]entry, error) {
	args := []string{"ls-tree", "-z"}
	if recursive {
		args = append(args, "-r")
	}
	args = append(args, ref, "--", path)
	out, err := gitx.Output(dir, args...)
	if err != nil {
		return nil, err
	}
	result := map[string]entry{}
	for _, row := range strings.Split(string(out), "\x00") {
		if row == "" {
			continue
		}
		meta, name, found := strings.Cut(row, "\t")
		if !found {
			return nil, fmt.Errorf("unreadable tree entry for %s", path)
		}
		fields := strings.Fields(meta)
		if len(fields) != 3 {
			return nil, fmt.Errorf("unreadable tree entry for %s", path)
		}
		result[name] = entry{kind: fields[1], oid: fields[2]}
	}
	return result, nil
}

// Snapshot hashes complete skill directories using Git tree objects.
func Snapshot(dir, ref string) (Entry, error) {
	files, err := entries(dir, ref, Skills, true)
	if err != nil {
		return Entry{}, err
	}
	skills, err := entries(dir, ref, Skills, false)
	if err != nil {
		return Entry{}, err
	}
	result := Empty()
	for path, item := range skills {
		if item.kind == "tree" {
			if _, ok := files[path+"/SKILL.md"]; ok {
				result.Skills[strings.TrimPrefix(path, Skills)] = item.oid
			}
		}
	}
	return result, nil
}

// Parse validates the hash-only accepted-content lock.
func Parse(data []byte) (Entry, error) {
	var value Entry
	if err := json.Unmarshal(data, &value); err != nil {
		return Entry{}, fmt.Errorf("unsupported accepted skill lock; expected version %d", Version)
	}
	if value.Version != Version || value.Skills == nil {
		return Entry{}, fmt.Errorf("unsupported accepted skill lock; expected version %d", Version)
	}
	for name, oid := range value.Skills {
		if name == "" || oid == "" {
			return Entry{}, fmt.Errorf("invalid accepted skill hashes")
		}
	}
	value.Skills = sortedMap(value.Skills)
	return value, nil
}

func sortedMap(input map[string]string) map[string]string {
	result := make(map[string]string, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
}

// Local reads accepted hashes including uncommitted local updates.
func Local(dir string) (Entry, error) {
	data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(Lock)))
	if errors.Is(err, os.ErrNotExist) {
		return Empty(), nil
	}
	if err != nil {
		return Entry{}, err
	}
	return Parse(data)
}

// Read loads accepted hashes from the selected commit.
func Read(dir, ref string) (Entry, error) {
	tree, err := entries(dir, ref, Intents, false)
	if err != nil {
		return Entry{}, err
	}
	if _, ok := tree[Lock]; !ok {
		return Empty(), nil
	}
	out, err := gitx.Output(dir, "show", ref+":"+Lock)
	if err != nil {
		return Entry{}, err
	}
	return Parse(out)
}

// Select chooses the differing hashes. Intent files supply review criteria, not
// another baseline: a skill whose hash already matches the accepted lock is
// never reviewed again, and a changed intent alone never triggers a run.
func Select(current, recorded Entry, intents map[string]bool) Plan {
	plan := emptyPlan()
	names := make([]string, 0, len(current.Skills)+len(recorded.Skills))
	for name := range current.Skills {
		names = append(names, name)
	}
	for name := range recorded.Skills {
		if _, ok := current.Skills[name]; !ok {
			names = append(names, name)
		}
	}
	for name := range current.Skills {
		plan.InputTrees[name] = current.Skills[name]
	}
	for _, name := range slices.Sorted(slices.Values(names)) {
		if current.Skills[name] != recorded.Skills[name] {
			plan.Skills = append(plan.Skills, name)
		}
	}
	for _, name := range plan.Skills {
		if intents[name] {
			plan.ReviewSkills = append(plan.ReviewSkills, name)
		}
	}
	plan.NeedsReview = len(plan.ReviewSkills) > 0
	plan.LockChanged = len(plan.Skills) > 0
	return plan
}

// Compare builds a full plan for CI, comparing skill hashes only against the
// accepted lock at head.
func Compare(dir, base, head, since string) (Plan, error) {
	current, err := Snapshot(dir, head)
	if err != nil {
		return Plan{}, err
	}
	recorded, err := Read(dir, head)
	if err != nil {
		return Plan{}, err
	}
	comparison := base
	if since != "" && exists(dir, since+"^{commit}") {
		comparison = since
	}
	tree, err := entries(dir, head, Intents, false)
	if err != nil {
		return Plan{}, err
	}
	intents := map[string]bool{}
	for path := range tree {
		if strings.HasSuffix(path, ".md") && path != Intents+"README.md" {
			intents[strings.TrimSuffix(strings.TrimPrefix(path, Intents), ".md")] = true
		}
	}
	plan := Select(current, recorded, intents)
	plan.Base = base
	plan.Head = head
	plan.Comparison = comparison
	return plan, nil
}

func exists(dir, object string) bool {
	return gitx.Run(dir, "cat-file", "-e", object) == nil
}

// WorkingTree hashes selected working files through a private index so that the
// caller's staging area and commits are never touched. Reading uncommitted state
// is required for `status` and `record` in a main checkout with pending edits.
func WorkingTree(dir string, paths ...string) (string, error) {
	if len(paths) == 0 {
		paths = []string{Skills}
	}
	directory, err := os.MkdirTemp("", "skillctrl-index-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(directory)
	env := append(os.Environ(), "GIT_INDEX_FILE="+filepath.Join(directory, "index"))
	for _, arguments := range [][]string{
		{"read-tree", "HEAD"},
		append([]string{"add", "-A", "--"}, paths...),
	} {
		if err := gitx.RunEnv(dir, env, arguments...); err != nil {
			return "", err
		}
	}
	out, err := gitx.OutputEnv(dir, env, "write-tree")
	if err != nil {
		return "", err
	}
	return gitx.Trimmed(out), nil
}

// Record advances only explicitly accepted hashes and preserves every other
// entry. A skill that disappeared from the tree has its entry removed.
func Record(dir string, current, recorded Entry, names []string) (Entry, error) {
	values := sortedMap(recorded.Skills)
	for _, name := range names {
		if hash, ok := current.Skills[name]; ok {
			values[name] = hash
		} else {
			delete(values, name)
		}
	}
	result := Entry{Version: Version, Skills: sortedMap(values)}
	path := filepath.Join(dir, filepath.FromSlash(Lock))
	if _, err := os.Stat(path); err == nil {
		existing, err := os.ReadFile(path)
		if err == nil {
			var previous Entry
			if json.Unmarshal(existing, &previous) == nil && sameSkills(previous, result) {
				// Rewriting an unchanged lock would dirty the working tree for
				// no reason and hide "the lock did not move" from the caller.
				return result, nil
			}
		}
	}
	if err := jsonfmt.WriteFile(path, result); err != nil {
		return Entry{}, err
	}
	return result, nil
}

func sameSkills(a, b Entry) bool {
	if len(a.Skills) != len(b.Skills) {
		return false
	}
	for name, hash := range a.Skills {
		if b.Skills[name] != hash {
			return false
		}
	}
	return true
}
