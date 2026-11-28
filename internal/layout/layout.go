// Package layout resolves where a repository keeps its skill directories.
//
// A repository without configuration keeps every skill in .agents/skills. A
// repository that distributes skills as several packages can instead declare
// named skill roots in .agents/skillctrl/config.json, for example one root per
// plugin. Skill names stay unique across all roots, so registrations, intents
// and accepted hashes keep using the bare skill name and existing locks remain
// valid.
package layout

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// Repository-relative locations that are part of the on-disk contract.
const (
	Config      = ".agents/skillctrl/config.json"
	DefaultRoot = ".agents/skills"
	Version     = 1
)

var rootName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// Root is one directory whose direct children are skill directories.
type Root struct {
	// Name selects the root on the command line. The implicit default root has
	// an empty name.
	Name string `json:"name"`
	// Path is repository-relative and slash-separated.
	Path string `json:"path"`
}

// Layout lists the skill roots of one repository in a stable order.
type Layout struct {
	Roots []Root
}

// Default is the layout of a repository without configuration.
func Default() Layout { return Layout{Roots: []Root{{Path: DefaultRoot}}} }

type config struct {
	Version    int               `json:"version"`
	SkillRoots map[string]string `json:"skillRoots"`
}

// Load reads the repository's skill roots from its working copy.
func Load(repo string) (Layout, error) {
	data, err := os.ReadFile(filepath.Join(repo, filepath.FromSlash(Config)))
	if os.IsNotExist(err) {
		return Default(), nil
	}
	if err != nil {
		return Layout{}, err
	}
	return Parse(data)
}

// Parse validates a configuration document.
func Parse(data []byte) (Layout, error) {
	var value config
	if err := json.Unmarshal(data, &value); err != nil || value.Version != Version || len(value.SkillRoots) == 0 {
		return Layout{}, fmt.Errorf("unsupported %s; expected version %d with at least one skillRoots entry", Config, Version)
	}
	roots := make([]Root, 0, len(value.SkillRoots))
	for name, raw := range value.SkillRoots {
		if !rootName.MatchString(name) {
			return Layout{}, fmt.Errorf("skill root names must be plain names: %q", name)
		}
		cleaned := path.Clean(raw)
		if raw == "" || path.IsAbs(raw) || strings.Contains(raw, `\`) || cleaned != raw || cleaned == "." ||
			cleaned == ".." || strings.HasPrefix(cleaned, "../") || cleaned == ".git" || strings.HasPrefix(cleaned, ".git/") {
			return Layout{}, fmt.Errorf("skill root %s must be a clean repository-relative path: %q", name, raw)
		}
		roots = append(roots, Root{Name: name, Path: cleaned})
	}
	slices.SortFunc(roots, func(a, b Root) int { return strings.Compare(a.Name, b.Name) })
	// A root inside another would make one directory both a skill and a root.
	for _, outer := range roots {
		for _, inner := range roots {
			if outer.Name != inner.Name && (outer.Path == inner.Path || strings.HasPrefix(inner.Path, outer.Path+"/")) {
				return Layout{}, fmt.Errorf("skill roots must not overlap: %s and %s", outer.Name, inner.Name)
			}
		}
	}
	return Layout{Roots: roots}, nil
}

// Configured reports whether the repository declares its own roots.
func (layout Layout) Configured() bool {
	return len(layout.Roots) != 1 || layout.Roots[0].Name != ""
}

// Root selects a root by name. An empty name selects the only root, and is an
// error when several roots exist, because the destination would be a guess.
func (layout Layout) Root(name string) (Root, error) {
	if name == "" {
		if len(layout.Roots) == 1 {
			return layout.Roots[0], nil
		}
		return Root{}, fmt.Errorf("this repository has several skill roots; choose one with --root (%s)", strings.Join(layout.Names(), ", "))
	}
	for _, root := range layout.Roots {
		if root.Name == name {
			return root, nil
		}
	}
	if !layout.Configured() {
		return Root{}, fmt.Errorf("--root requires skill roots in %s", Config)
	}
	return Root{}, fmt.Errorf("unknown skill root %q; choose one of %s", name, strings.Join(layout.Names(), ", "))
}

// Names lists the configured root names.
func (layout Layout) Names() []string {
	names := make([]string, 0, len(layout.Roots))
	for _, root := range layout.Roots {
		names = append(names, root.Name)
	}
	return names
}

// Hidden reports whether a root entry belongs to tooling rather than to a skill.
// Plugin packagers such as Codex keep a manifest directory like .codex-plugin
// next to the skills; skill names never start with a dot.
func Hidden(name string) bool { return strings.HasPrefix(name, ".") }

// Locations maps each skill directory name to its root. A name present in two
// roots is refused: registrations, intents and hashes are keyed by bare name.
func (layout Layout) Locations(repo string) (map[string]Root, error) {
	result := map[string]Root{}
	for _, root := range layout.Roots {
		entries, err := os.ReadDir(filepath.Join(repo, filepath.FromSlash(root.Path)))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if Hidden(entry.Name()) || !entry.IsDir() {
				continue
			}
			if other, ok := result[entry.Name()]; ok {
				return nil, fmt.Errorf("skill %s exists in two skill roots: %s and %s", entry.Name(), other.Path, root.Path)
			}
			result[entry.Name()] = root
		}
	}
	return result, nil
}

// Dir returns the repository-relative directory of a skill: where it exists
// now, or in fallback when it does not exist yet.
func (layout Layout) Dir(locations map[string]Root, name string, fallback Root) string {
	if root, ok := locations[name]; ok {
		return root.Path + "/" + name
	}
	return fallback.Path + "/" + name
}
