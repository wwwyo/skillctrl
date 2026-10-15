package upstream

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/wwwyo/skillctrl/internal/gitx"
	"github.com/wwwyo/skillctrl/internal/jsonfmt"
)

// Originals is the repository-relative directory holding the most recently
// imported original of each single-input skill. It is the base of the next
// update's three-way merge and is verified against the registered tree hash
// before use, so a locally edited snapshot is never trusted as a base.
const Originals = ".agents/skillctrl/originals"

// PreparedOriginals is the staged copy of Originals beside import results.
const PreparedOriginals = "originals"

// PreparedUpdates is the staged reconciliation report beside import results.
const PreparedUpdates = "updates.json"

// Update reports how one changed upstream original reached the local skill.
type Update struct {
	Name      string        `json:"name"`
	Path      string        `json:"path"`
	Layout    string        `json:"layout"`
	Status    string        `json:"status"`
	Base      string        `json:"base,omitempty"`
	Reason    string        `json:"reason,omitempty"`
	Original  string        `json:"original,omitempty"`
	Changes   []Change      `json:"changes,omitempty"`
	Conflicts []Conflict    `json:"conflicts,omitempty"`
	Inputs    []InputUpdate `json:"inputs,omitempty"`
}

// Update statuses. Merged and conflicted results keep local edits; replaced
// results discard them because no verified previous original was available.
const (
	StatusMerged     = "merged"
	StatusConflicted = "conflicted"
	StatusReplaced   = "replaced"
	StatusRefreshed  = "refreshed"
)

// Change is one file the upstream changed between the previous and new original.
type Change struct {
	Path     string `json:"path"`
	Upstream string `json:"upstream"`
	Result   string `json:"result"`
}

// Conflict locates a file needing manual resolution. Lines are 1-based,
// inclusive ranges of diff3 conflict blocks in the written file.
type Conflict struct {
	Path  string   `json:"path"`
	Kind  string   `json:"kind"`
	Lines [][2]int `json:"lines,omitempty"`
}

// InputUpdate reports one refreshed original of a merged skill.
type InputUpdate struct {
	Skill   string `json:"skill"`
	Path    string `json:"path"`
	Changed bool   `json:"changed"`
}

// ReadUpdates returns the reconciliation report staged by a preparation.
func ReadUpdates(directory string) ([]Update, error) {
	data, err := os.ReadFile(filepath.Join(directory, PreparedUpdates))
	if errors.Is(err, os.ErrNotExist) {
		return []Update{}, nil
	}
	if err != nil {
		return nil, err
	}
	var updates []Update
	if err := json.Unmarshal(data, &updates); err != nil {
		return nil, err
	}
	return updates, nil
}

func writeUpdates(directory string, updates []Update) error {
	if updates == nil {
		updates = []Update{}
	}
	return jsonfmt.WriteFile(filepath.Join(directory, PreparedUpdates), updates)
}

// file is one regular file of a skill tree, keyed by its slash-separated path.
type file struct {
	executable bool
	data       []byte
}

func (f file) mode() string {
	if f.executable {
		return "100755"
	}
	return "100644"
}

func sameFile(a file, inA bool, b file, inB bool) bool {
	if !inA || !inB {
		return inA == inB
	}
	return a.executable == b.executable && bytes.Equal(a.data, b.data)
}

// readTree reads a skill directory, refusing anything but directories and
// regular files so a merge can never follow a link out of the skill.
func readTree(root string) (map[string]file, error) {
	files := map[string]file{}
	err := filepath.WalkDir(root, func(current string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("skill content must be regular files: %s", entry.Name())
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		data, err := os.ReadFile(current)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, current)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(relative)] = file{executable: info.Mode().Perm()&0o111 != 0, data: data}
		return nil
	})
	return files, err
}

func writeTree(target string, files map[string]file) error {
	if err := os.RemoveAll(target); err != nil {
		return err
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		return err
	}
	for name, entry := range files {
		destination := filepath.Join(target, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			return err
		}
		mode := os.FileMode(0o644)
		if entry.executable {
			mode = 0o755
		}
		if err := os.WriteFile(destination, entry.data, mode); err != nil {
			return err
		}
		if err := os.Chmod(destination, mode); err != nil {
			return err
		}
	}
	return nil
}

// objectTree reads a Git tree as skill files. Only plain blobs are accepted,
// matching what an import would have written.
func objectTree(repository, tree string) (map[string]file, error) {
	listing, err := gitx.Safe(repository, "ls-tree", "-rz", tree)
	if err != nil {
		return nil, err
	}
	modes := map[string]string{}
	oids := map[string]string{}
	for _, row := range strings.Split(string(listing), "\x00") {
		if row == "" {
			continue
		}
		meta, name, found := strings.Cut(row, "\t")
		fields := strings.Fields(meta)
		if !found || len(fields) != 3 || fields[1] != "blob" || (fields[0] != "100644" && fields[0] != "100755") {
			return nil, fmt.Errorf("recorded original contains unsupported entries")
		}
		modes[name], oids[name] = fields[0], fields[2]
	}
	blobs, err := readBlobs(repository, slices.Collect(maps.Values(oids)))
	if err != nil {
		return nil, err
	}
	files := map[string]file{}
	for name, oid := range oids {
		files[name] = file{executable: modes[name] == "100755", data: blobs[oid]}
	}
	return files, nil
}

// treeHash computes the SHA-1 Git tree identifier of files, the identity
// recorded as skillFolderHash. Computing it in-process avoids staging the
// snapshot through an index that the caller's Git configuration could filter.
func treeHash(files map[string]file) string {
	type node struct {
		blobs map[string]file
		dirs  map[string]*node
	}
	root := &node{blobs: map[string]file{}, dirs: map[string]*node{}}
	for name, entry := range files {
		current := root
		parts := strings.Split(name, "/")
		for _, part := range parts[:len(parts)-1] {
			next := current.dirs[part]
			if next == nil {
				next = &node{blobs: map[string]file{}, dirs: map[string]*node{}}
				current.dirs[part] = next
			}
			current = next
		}
		current.blobs[parts[len(parts)-1]] = entry
	}
	object := func(kind string, body []byte) []byte {
		hash := sha1.New()
		fmt.Fprintf(hash, "%s %d\x00", kind, len(body))
		hash.Write(body)
		return hash.Sum(nil)
	}
	var hashNode func(*node) []byte
	hashNode = func(current *node) []byte {
		type entry struct {
			sortKey, header string
			oid             []byte
		}
		entries := []entry{}
		for name, blob := range current.blobs {
			entries = append(entries, entry{name, blob.mode() + " " + name, object("blob", blob.data)})
		}
		for name, child := range current.dirs {
			// Git orders a subtree as though its name ended with a slash.
			entries = append(entries, entry{name + "/", "40000 " + name, hashNode(child)})
		}
		slices.SortFunc(entries, func(a, b entry) int { return strings.Compare(a.sortKey, b.sortKey) })
		var body bytes.Buffer
		for _, item := range entries {
			body.WriteString(item.header)
			body.WriteByte(0)
			body.Write(item.oid)
		}
		return object("tree", body.Bytes())
	}
	return hex.EncodeToString(hashNode(root))
}

// matchesRegistration reports whether files are exactly the registered original.
func matchesRegistration(files map[string]file, registration map[string]any) bool {
	if len(files) == 0 {
		return false
	}
	if recorded := field(registration, "skillFolderHash"); recorded != "" {
		return treeHash(files) == recorded
	}
	recorded := field(registration, "computedHash")
	if recorded == "" {
		return false
	}
	entries := map[string]treeFile{}
	contents := map[string][]byte{}
	for name, entry := range files {
		entries[name] = treeFile{oid: name}
		contents[name] = entry.data
	}
	return contentHash(entries, contents) == recorded
}

// snapshot returns the stored original when it still matches the registration.
func snapshot(directory string, registration map[string]any) (map[string]file, bool) {
	files, err := readTree(directory)
	if err != nil || !matchesRegistration(files, registration) {
		return nil, false
	}
	return files, true
}

// previousOriginal finds the base of a three-way update. Every source is
// verified against the registered identity; a missing base is reported with
// the reasons each source failed instead of guessing.
func previousOriginal(dir, snapshotDir string, request ExportRequest, adapter Adapter) (map[string]file, string, string) {
	previous := request.Previous
	if files, ok := snapshot(snapshotDir, previous); ok {
		return files, "snapshot", ""
	}
	reasons := []string{"no verified snapshot in " + Originals}
	if tree := field(previous, "skillFolderHash"); tree != "" {
		if gitx.SafeRun(dir, "cat-file", "-e", tree+"^{tree}") == nil {
			if files, err := objectTree(dir, tree); err == nil && treeHash(files) == tree {
				return files, "repository", ""
			}
		}
		reasons = append(reasons, "original tree is not in the repository's Git objects")
	} else {
		reasons = append(reasons, "registration has no original tree hash")
	}
	if source, ok := adapter.(originalSource); ok {
		files, err := source.Original(request)
		if err == nil {
			return files, "upstream", ""
		}
		reasons = append(reasons, err.Error())
	} else {
		reasons = append(reasons, "the acquisition adapter cannot fetch a recorded original")
	}
	return nil, "", strings.Join(reasons, "; ")
}

// originalSource is implemented by adapters able to fetch a previously
// imported original again from its recorded upstream commit.
type originalSource interface {
	Original(request ExportRequest) (map[string]file, error)
}

// Conflict kinds.
const (
	conflictContent      = "content"
	conflictBinary       = "binary"
	conflictModifyDelete = "modified locally, deleted upstream"
	conflictDeleteModify = "deleted locally, modified upstream"
)

// threeWay applies the upstream change from base to theirs onto ours, one file
// at a time. Files the upstream left alone keep their local content without
// being read by a merge tool, so a large unchanged skill costs only comparisons.
func threeWay(scratch string, base, ours, theirs map[string]file) (map[string]file, []Change, []Conflict, error) {
	result := maps.Clone(ours)
	changes := []Change{}
	conflicts := []Conflict{}
	names := map[string]bool{}
	for _, tree := range []map[string]file{base, ours, theirs} {
		for name := range tree {
			names[name] = true
		}
	}
	for _, name := range slices.Sorted(maps.Keys(names)) {
		b, inBase := base[name]
		o, inOurs := ours[name]
		t, inTheirs := theirs[name]
		if sameFile(b, inBase, t, inTheirs) {
			continue
		}
		change := Change{Path: name, Upstream: "modified", Result: "applied"}
		switch {
		case !inBase:
			change.Upstream = "added"
		case !inTheirs:
			change.Upstream = "deleted"
		case bytes.Equal(b.data, t.data):
			change.Upstream = "mode"
		}
		switch {
		case sameFile(o, inOurs, b, inBase):
			if inTheirs {
				result[name] = t
			} else {
				delete(result, name)
			}
		case sameFile(o, inOurs, t, inTheirs):
		case !inOurs:
			change.Result = "conflict"
			conflicts = append(conflicts, Conflict{Path: name, Kind: conflictDeleteModify})
		case !inTheirs:
			change.Result = "conflict"
			conflicts = append(conflicts, Conflict{Path: name, Kind: conflictModifyDelete})
		default:
			merged := file{executable: t.executable}
			if inBase && o.executable != b.executable {
				merged.executable = o.executable
			}
			change.Result = "merged"
			switch {
			case bytes.Equal(o.data, t.data), inBase && bytes.Equal(t.data, b.data):
				merged.data = o.data
			case inBase && bytes.Equal(o.data, b.data):
				merged.data = t.data
			case binary(o.data) || binary(t.data) || binary(b.data):
				// A binary file cannot carry conflict markers. Keep the local
				// bytes; the new original remains available in the snapshot.
				merged.data = o.data
				change.Result = "conflict"
				conflicts = append(conflicts, Conflict{Path: name, Kind: conflictBinary})
			default:
				data, conflicted, lines, err := mergeText(scratch, b.data, o.data, t.data)
				if err != nil {
					return nil, nil, nil, fmt.Errorf("merge %s: %w", name, err)
				}
				merged.data = data
				if conflicted {
					change.Result = "conflict"
					conflicts = append(conflicts, Conflict{Path: name, Kind: conflictContent, Lines: lines})
				}
			}
			result[name] = merged
		}
		changes = append(changes, change)
	}
	return result, changes, conflicts, nil
}

// binary uses Git's heuristic: a NUL byte near the start marks binary content.
func binary(data []byte) bool {
	return bytes.IndexByte(data[:min(len(data), 8000)], 0) >= 0
}

const (
	labelLocal    = "local"
	labelOriginal = "previous original"
	labelUpstream = "new original"
)

// mergeText runs git merge-file with diff3 markers, which keep the previous
// original between the local and upstream sides so a resolver sees all three.
// Git's exit status decides whether the merge conflicted: skill text may itself
// contain marker-shaped lines, so the markers only locate the conflicts.
func mergeText(scratch string, base, ours, theirs []byte) ([]byte, bool, [][2]int, error) {
	directory, err := os.MkdirTemp(scratch, "merge-")
	if err != nil {
		return nil, false, nil, err
	}
	defer os.RemoveAll(directory)
	paths := map[string][]byte{"local": ours, "base": base, "upstream": theirs}
	for name, data := range paths {
		if err := os.WriteFile(filepath.Join(directory, name), data, 0o600); err != nil {
			return nil, false, nil, err
		}
	}
	// merge-file exits with the number of conflicts, capped at 127; higher
	// statuses are errors.
	out, count, err := gitx.OutputStatus(directory, []string{"merge-file", "--stdout", "--diff3",
		"-L", labelLocal, "-L", labelOriginal, "-L", labelUpstream, "local", "base", "upstream"}, 127)
	if err != nil {
		return nil, false, nil, err
	}
	if count == 0 {
		return out, false, nil, nil
	}
	ranges := conflictLines(out)
	if len(ranges) != count {
		// Marker-shaped content made the blocks ambiguous; report the conflict
		// without locations rather than point at the wrong lines.
		ranges = nil
	}
	return out, true, ranges, nil
}

// conflictLines locates complete diff3 blocks: local, previous original,
// separator, and new original markers in that order.
func conflictLines(data []byte) [][2]int {
	markers := []string{"<<<<<<< " + labelLocal, "||||||| " + labelOriginal, "=======", ">>>>>>> " + labelUpstream}
	var ranges [][2]int
	start, next := 0, 0
	for index, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSuffix(line, "\r")
		switch {
		case line == markers[0]:
			start, next = index+1, 1
		case next > 0 && line == markers[next]:
			next++
			if next == len(markers) {
				ranges = append(ranges, [2]int{start, index + 1})
				next = 0
			}
		}
	}
	return ranges
}

// reconcile merges a changed original into the prepared skill directory and
// describes the result. Without a verified base, the new original replaces
// the skill as before three-way updates existed, and the report says why.
func reconcile(dir, directory, name, skillDir, snapshotDir, staged string, request ExportRequest, adapter Adapter) (Update, error) {
	update := Update{Name: name, Path: ".agents/skills/" + name, Layout: "single", Original: Originals + "/" + name}
	theirs, err := readTree(staged)
	if err != nil {
		return update, err
	}
	if _, err := os.Stat(skillDir); os.IsNotExist(err) {
		// Merging into nothing would delete every file the upstream kept.
		update.Status, update.Reason = StatusReplaced, "the local skill directory is missing"
		return update, writeTree(skillDir, theirs)
	} else if err != nil {
		return update, err
	}
	ours, err := readTree(skillDir)
	if err != nil {
		return update, err
	}
	base, source, reason := previousOriginal(dir, snapshotDir, request, adapter)
	if base == nil {
		update.Status, update.Reason = StatusReplaced, reason
		return update, writeTree(skillDir, theirs)
	}
	result, changes, conflicts, err := threeWay(directory, base, ours, theirs)
	if err != nil {
		return update, err
	}
	update.Base, update.Changes, update.Conflicts = source, changes, conflicts
	update.Status = StatusMerged
	if len(conflicts) > 0 {
		update.Status = StatusConflicted
	}
	return update, writeTree(skillDir, result)
}

// relativeSkillPath strips the manifest name from a registered skillPath.
func relativeSkillPath(skillPath string) string {
	folder := path.Dir(skillPath)
	if folder == "." {
		return ""
	}
	return folder
}

// shortHash keeps report reasons readable without hiding the identity.
func shortHash(value string) string {
	if len(value) > 12 {
		return value[:12]
	}
	return value
}
