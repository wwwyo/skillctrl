// Package upstream fetches GitHub skill trees directly without executing an
// external installer.
//
// Upstream repositories are untrusted input. Nothing here runs a script from a
// fetched repository: trees are read as Git objects, exported as plain files
// with recorded modes, and rejected outright when they contain symlinks,
// submodules, Git rules that could change how accepted content is tracked, or
// paths that the destination repository's ignore rules would exclude.
package upstream

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/wwwyo/skillctrl/internal/gitx"
	"github.com/wwwyo/skillctrl/internal/jsonfmt"
	"github.com/wwwyo/skillctrl/internal/skillstate"
)

// Lock is the repository-relative upstream lock path.
const Lock = "skills-lock.json"

// LegacyLock is read only when the project lock is absent. Successful imports
// migrate it to Lock; an existing project lock always takes precedence.
const LegacyLock = ".agents/.skill-lock.json"

// Version is the project lock version used by the skills CLI. Version 3
// registrations written by older skillctrl releases remain readable.
const Version = 1

var (
	sourcePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]*/[A-Za-z0-9][A-Za-z0-9._-]*$`)
	namePattern   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	ownerPattern  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]{0,38}$`)
)

// Name reports whether value is a plain skill directory name. Separators and
// dot-prefixed traversal are rejected so a name can never point outside the
// skills directory.
func Name(value string) bool { return namePattern.MatchString(value) }

// Source normalizes an owner/repository or credential-free GitHub HTTPS URL.
// Anything that could carry a token, a ref, or a non-GitHub host is refused
// instead of being parsed, so a URL with a query string fails.
func Source(value string) (string, error) {
	value = strings.TrimSuffix(strings.TrimPrefix(value, "https://github.com/"), "/")
	value = strings.TrimSuffix(value, ".git")
	if !sourcePattern.MatchString(value) {
		return "", fmt.Errorf("source must be owner/repo or a GitHub repository HTTPS URL")
	}
	return value, nil
}

// Record is the upstream lock. Skill entries stay as decoded maps so that
// fields written by other tools survive an update untouched.
type Record struct {
	Version  int                        `json:"version"`
	Skills   map[string]any             `json:"skills"`
	Extra    map[string]json.RawMessage `json:"-"`
	original []byte
	native   *Record
	state    *skillstate.Record
}

// MarshalJSON retains unknown top-level fields written by another tool.
func (record Record) MarshalJSON() ([]byte, error) {
	fields := maps.Clone(record.Extra)
	if fields == nil {
		fields = map[string]json.RawMessage{}
	}
	version, err := json.Marshal(record.Version)
	if err != nil {
		return nil, err
	}
	skills, err := jsonfmt.Compact(record.Skills)
	if err != nil {
		return nil, err
	}
	fields["version"], fields["skills"] = version, skills
	return jsonfmt.Compact(fields)
}

// ManagedSkills selects the GitHub registrations skillctrl can update. Other
// providers remain in the shared project lock without entering intent review.
func (record *Record) ManagedSkills() map[string]any {
	managed := map[string]any{}
	for name, raw := range record.Skills {
		entry := raw.(map[string]any)
		if entry["sourceType"] == "github" || entry["sources"] != nil {
			managed[name] = raw
		}
	}
	return managed
}

// loadNative reads the project lock, falling back to the old repository-local path.
func loadNative(dir string) (*Record, error) {
	for _, relative := range []string{Lock, LegacyLock} {
		path := filepath.Join(dir, filepath.FromSlash(relative))
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("upstream lock must be a regular file: %s", relative)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		return parseLock(data)
	}
	return &Record{Version: Version, Skills: map[string]any{}}, nil
}

// readNative loads native registrations from a fixed Git tree rather than the
// working copy, so CI selection cannot depend on unstaged edits.
func readNative(dir, ref string) (*Record, error) {
	for _, relative := range []string{Lock, LegacyLock} {
		listing, err := gitx.Output(dir, "ls-tree", ref, "--", relative)
		if err != nil {
			return nil, err
		}
		if len(listing) == 0 {
			continue
		}
		if !strings.HasPrefix(string(listing), "100644 blob ") && !strings.HasPrefix(string(listing), "100755 blob ") {
			return nil, fmt.Errorf("upstream lock must be a regular file: %s", relative)
		}
		data, err := gitx.Output(dir, "show", ref+":"+relative)
		if err != nil {
			return nil, err
		}
		return parseLock(data)
	}
	return &Record{Version: Version, Skills: map[string]any{}}, nil
}

func parseLock(data []byte) (*Record, error) {
	var value Record
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, fmt.Errorf("unsupported upstream lock; expected version %d", Version)
	}
	if (value.Version != Version && value.Version != 3) || value.Skills == nil {
		return nil, fmt.Errorf("unsupported upstream lock; expected version %d", Version)
	}
	if err := ValidateRegistrations(value.Skills); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &value.Extra); err != nil {
		return nil, err
	}
	delete(value.Extra, "version")
	delete(value.Extra, "skills")
	value.original = bytes.Clone(data)
	return &value, nil
}

// ValidateRegistrations checks skill names and source identities before import.
func ValidateRegistrations(skills map[string]any) error {
	for name, entry := range skills {
		if !Name(name) {
			return fmt.Errorf("invalid upstream skill record")
		}
		object, ok := entry.(map[string]any)
		if !ok {
			return fmt.Errorf("invalid upstream skill record")
		}
		if object["sourceType"] == "github" || object["sources"] != nil {
			if _, err := SourcePaths(object, name); err != nil {
				return err
			}
		} else if field(object, "sourceType") == "" || field(object, "source") == "" {
			return fmt.Errorf("invalid upstream skill record")
		}
	}
	return nil
}

// field reads a string field from a record entry.
func field(object map[string]any, key string) string {
	value, _ := object[key].(string)
	return value
}

// treeFile is one entry from a Git tree listing.
type treeFile struct {
	mode string
	kind string
	oid  string
}

// gitControlName reports whether a path component is one Git treats specially.
// The comparison folds case: a case-insensitive filesystem accepts ".GITIGNORE"
// as ".gitignore", so spelling alone must not decide whether the import is safe.
func gitControlName(name string) bool {
	for _, control := range []string{".git", ".gitignore", ".gitattributes"} {
		if strings.EqualFold(name, control) {
			return true
		}
	}
	return false
}

// Repository inspects one shallow clone and exports selected skill directories.
type Repository struct {
	source   string
	path     string
	commit   string
	revision string
	files    map[string]treeFile
	skills   map[string][]string
}

// New clones a repository without checking out a working tree. The clone is
// shallow, and the environment drops the inference credential so a hostile
// remote configuration cannot exfiltrate it.
func New(identifier, directory string) (*Repository, error) {
	return newRepository(identifier, directory, "")
}

func newRepository(identifier, directory, ref string) (*Repository, error) {
	source, err := Source(identifier)
	if err != nil {
		return nil, err
	}
	env := removeEnv(gitx.Environment("GIT_TERMINAL_PROMPT=0"), "OPENCODE_API_KEY")
	arguments := []string{"clone", "--quiet", "--depth", "1", "--no-checkout"}
	if ref != "" {
		if err := gitx.SafeRun("", "check-ref-format", "--branch", ref); err != nil {
			return nil, fmt.Errorf("invalid upstream ref")
		}
		arguments = append(arguments, "--branch", ref)
	}
	arguments = append(arguments, "--", "https://github.com/"+source+".git", directory)
	if err := gitx.SafeEnv("", env, arguments...); err != nil {
		return nil, err
	}
	head, err := gitx.Safe(directory, "rev-parse", "HEAD")
	if err != nil {
		return nil, err
	}
	return inspectRepository(source, directory, "HEAD", gitx.Trimmed(head))
}

func inspectRepository(source, directory, revision, commit string) (*Repository, error) {
	listing, err := gitx.Safe(directory, "ls-tree", "-rz", revision)
	if err != nil {
		return nil, err
	}
	files := map[string]treeFile{}
	for _, row := range strings.Split(string(listing), "\x00") {
		if row == "" {
			continue
		}
		meta, name, found := strings.Cut(row, "\t")
		if !found {
			return nil, fmt.Errorf("unreadable upstream tree")
		}
		fields := strings.Fields(meta)
		if len(fields) != 3 {
			return nil, fmt.Errorf("unreadable upstream tree")
		}
		files[name] = treeFile{mode: fields[0], kind: fields[1], oid: fields[2]}
	}
	repository := &Repository{source: source, path: directory, commit: commit, revision: revision, files: files}
	if err := repository.indexSkills(); err != nil {
		return nil, err
	}
	return repository, nil
}

// Commit returns the fetched commit, recorded in the lock as the origin of the
// imported trees.
func (r *Repository) Commit() string { return r.commit }

// Path returns the clone directory.
func (r *Repository) Path() string { return r.path }

// indexSkills maps every discoverable name to the manifests that provide it.
// Both the frontmatter name and the containing directory name are accepted, so
// a relocated skill can still be found after an upstream reorganization.
func (r *Repository) indexSkills() error {
	manifests := map[string]string{}
	for name, entry := range r.files {
		if containsSourceDirectory(name) {
			continue
		}
		if path.Base(name) != "SKILL.md" || entry.kind != "blob" {
			continue
		}
		if entry.mode != "100644" && entry.mode != "100755" {
			continue
		}
		manifests[name] = entry.oid
	}
	blobs, err := readBlobs(r.path, slices.Collect(maps.Values(manifests)))
	if err != nil {
		return err
	}
	r.skills = map[string][]string{}
	for name, oid := range manifests {
		label, found := manifestName(blobs[oid])
		if !found {
			continue
		}
		if Name(label) {
			r.skills[label] = append(r.skills[label], name)
		}
		alias := path.Base(path.Dir(name))
		if alias != label && Name(alias) {
			r.skills[alias] = append(r.skills[alias], name)
		}
	}
	for name := range r.skills {
		slices.Sort(r.skills[name])
	}
	return nil
}

func manifestName(content []byte) (string, bool) {
	text := strings.ReplaceAll(string(content), "\r\n", "\n")
	if !strings.HasPrefix(text, "---\n") {
		return "", false
	}
	frontmatter, _, found := strings.Cut(text, "\n---")
	if !found {
		return "", false
	}
	for _, line := range strings.Split(frontmatter, "\n") {
		if key, value, ok := strings.Cut(line, "name:"); ok && strings.TrimSpace(key) == "" {
			return strings.Trim(strings.TrimSpace(value), "\"'"), true
		}
	}
	return "", true
}

// Select keeps the recorded path when it is still valid, and otherwise requires
// a unique named manifest. An ambiguous or missing match is refused rather than
// resolved arbitrarily, because guessing would import the wrong skill.
func (r *Repository) Select(name, previous string) (string, error) {
	matches := r.skills[name]
	if slices.Contains(matches, previous) {
		return previous, nil
	}
	if len(matches) != 1 {
		return "", fmt.Errorf("upstream skill missing or ambiguous: %s", name)
	}
	return matches[0], nil
}

// Export copies tracked files only, rejecting links and filesystem escapes.
func (r *Repository) Export(destination, name, target string, previous map[string]any) (map[string]any, error) {
	return r.export(destination, name, name, target, previous)
}

func (r *Repository) export(destination, name, destinationName, target string, previous map[string]any) (map[string]any, error) {
	result, files, contents, err := r.inspectExport(destination, name, destinationName, previous)
	if err != nil {
		return nil, err
	}
	if files != nil {
		if err := writeExport(target, files, contents); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func (r *Repository) inspectExport(destination, name, destinationName string, previous map[string]any) (map[string]any, map[string]treeFile, map[string][]byte, error) {
	manifest, err := r.Select(name, field(previous, "skillPath"))
	if err != nil {
		return nil, nil, nil, err
	}
	folder := path.Dir(manifest)
	if folder == "" {
		folder = "."
	}
	prefix := ""
	revision := r.revision
	tree := revision + "^{tree}"
	if folder != "." {
		prefix = folder + "/"
		tree = revision + ":" + folder
	}
	out, err := gitx.Safe(r.path, "rev-parse", tree)
	if err != nil {
		return nil, nil, nil, err
	}
	oid := gitx.Trimmed(out)
	previousSource, _ := Source(field(previous, "source"))
	if strings.EqualFold(previousSource, r.source) && field(previous, "skillPath") == manifest &&
		field(previous, "skillFolderHash") == oid {
		// An unchanged original must not be re-imported: doing so would discard
		// the maintainer's adaptation and force a review of identical content.
		return previous, nil, nil, nil
	}
	files := map[string]treeFile{}
	for name, entry := range r.files {
		if strings.HasPrefix(name, prefix) {
			files[strings.TrimPrefix(name, prefix)] = entry
		}
	}
	for name, entry := range files {
		if name != path.Clean(name) || name == "" || strings.HasPrefix(name, "/") {
			return nil, nil, nil, fmt.Errorf("unsafe upstream file path")
		}
		for _, part := range strings.Split(name, "/") {
			if part == ".." || gitControlName(part) {
				return nil, nil, nil, fmt.Errorf("unsafe upstream file path")
			}
		}
		if gitControlName(path.Base(name)) {
			return nil, nil, nil, fmt.Errorf("upstream skill contains Git rules that could alter accepted content: %s", name)
		}
		if entry.kind != "blob" || (entry.mode != "100644" && entry.mode != "100755") {
			return nil, nil, nil, fmt.Errorf("upstream skill contains a symlink or submodule: %s", name)
		}
	}
	if err := rejectIgnored(destination, destinationName, files); err != nil {
		return nil, nil, nil, err
	}
	contents, err := readBlobs(r.path, values(files))
	if err != nil {
		return nil, nil, nil, err
	}
	computedHash := contentHash(files, contents)
	if strings.EqualFold(previousSource, r.source) &&
		(field(previous, "skillPath") == "" || field(previous, "skillPath") == manifest) &&
		field(previous, "skillFolderHash") == "" && field(previous, "computedHash") == computedHash {
		return previous, nil, nil, nil
	}
	now := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	result := maps.Clone(previous)
	if result == nil {
		result = map[string]any{}
	}
	delete(result, "nativeExport")
	delete(result, "nativeName")
	if !strings.EqualFold(previousSource, r.source) {
		delete(result, "ref")
	}
	result["source"] = r.source
	result["sourceType"] = "github"
	result["sourceUrl"] = "https://github.com/" + r.source + ".git"
	result["skillPath"] = manifest
	result["skillFolderHash"] = oid
	result["computedHash"] = computedHash
	result["sourceCommit"] = r.commit
	if existing, ok := result["installedAt"].(string); !ok || existing == "" {
		result["installedAt"] = now
	}
	result["updatedAt"] = now
	if declared, _ := manifestName(contents[files["SKILL.md"].oid]); Name(declared) && declared != destinationName {
		result["nativeName"] = declared
	}
	return result, files, contents, nil
}

func writeExport(target string, files map[string]treeFile, contents map[string][]byte) error {
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
		if entry.mode == "100755" {
			mode = 0o755
		}
		if err := os.WriteFile(destination, contents[entry.oid], mode); err != nil {
			return err
		}
		if err := os.Chmod(destination, mode); err != nil {
			return err
		}
	}
	return nil
}

func values(files map[string]treeFile) []string {
	oids := make([]string, 0, len(files))
	for _, entry := range files {
		oids = append(oids, entry.oid)
	}
	return oids
}

// rejectIgnored refuses content the destination repository would not track,
// which would otherwise produce a skill whose accepted hash cannot be recorded.
// The rules are read from the destination repository, not from the clone: it is
// the destination's ignore rules that would later hide the imported file.
func rejectIgnored(destination, name string, files map[string]treeFile) error {
	var request strings.Builder
	for path := range files {
		request.WriteString(".agents/skills/" + name + "/" + path + "\x00")
	}
	// check-ignore exits 1 when nothing matched; that is a result, not a failure.
	out, err := gitx.OutputErr(destination, []string{"check-ignore", "--no-index", "-z", "--stdin"},
		[]byte(request.String()), 1)
	if err != nil {
		return err
	}
	if len(out) > 0 {
		return fmt.Errorf("upstream skill contains files excluded by destination Git rules: %s", name)
	}
	return nil
}

// Install prepares all imports in a temporary directory before promoting any
// result to the caller's worktree. A failure anywhere leaves the caller's
// checkout untouched.
func Install(dir, command string, selected []string, identifier, directory string) (target, lock string, err error) {
	return InstallWithAdapter(dir, command, selected, identifier, directory, NewGitAdapter())
}

// Merge prepares a skill whose upstream registrations are stored as an array.
func Merge(dir, name string, inputs []Input, directory string) (target, lock string, err error) {
	return MergeWithAdapter(dir, name, inputs, directory, NewGitAdapter())
}

type importRequest struct {
	command    string
	selected   []string
	source     string
	inputs     []Input
	outputName string
}

func install(dir, directory string, request importRequest, adapter Adapter) (target, lock string, err error) {
	command, selected, identifier, inputs, outputName := request.command, request.selected, request.source, request.inputs, request.outputName
	value, err := Load(dir)
	if err != nil {
		return "", "", err
	}
	target = filepath.Join(directory, "skills")
	if err := copyTree(filepath.Join(dir, filepath.FromSlash(".agents/skills")), target); err != nil {
		return "", "", err
	}
	if len(selected) == 0 {
		selected = slices.Sorted(maps.Keys(value.ManagedSkills()))
	}
	for index, requested := range selected {
		name := requested
		if command == "add" && outputName != "" {
			name = outputName
		}
		if !Name(requested) || !Name(name) {
			return "", "", fmt.Errorf("skill names must be plain directory names")
		}
		previous, _ := value.Skills[name].(map[string]any)
		if previous == nil {
			previous = map[string]any{}
		}
		if len(previous) > 0 && previous["sourceType"] != "github" && previous["sources"] == nil && command != "remove" {
			return "", "", fmt.Errorf("only GitHub upstream sources are supported: %s", name)
		}
		_, multiple := previous["sources"]
		if command == "merge" || command == "update" && multiple {
			exported, mergeErr := mergeSkill(dir, name, filepath.Join(target, name), previous, inputs, adapter, directory)
			if mergeErr != nil {
				return "", "", mergeErr
			}
			value.Skills[name] = exported
			continue
		}
		switch command {
		case "remove":
			if info, statErr := os.Stat(filepath.Join(target, name)); statErr != nil || !info.IsDir() {
				return "", "", fmt.Errorf("unknown installed skill: %s", name)
			}
			if err := os.RemoveAll(filepath.Join(target, name)); err != nil {
				return "", "", err
			}
			delete(value.Skills, name)
			continue
		case "add":
			if multiple {
				return "", "", fmt.Errorf("skill has multiple upstream sources; use merge to replace them: %s", name)
			}
			if previous["source"] == nil {
				if _, statErr := os.Stat(filepath.Join(target, name)); statErr == nil {
					return "", "", fmt.Errorf("refusing to overwrite a handwritten skill: %s", name)
				}
			}
		case "update":
			if previous["source"] == nil {
				return "", "", fmt.Errorf("skill has no registered upstream: %s", name)
			}
		}
		origin := identifier
		if command == "add" && len(inputs) > 0 {
			origin = inputs[index].Source
		}
		if command != "add" {
			origin = field(previous, "source")
		}
		source, err := Source(origin)
		if err != nil {
			return "", "", err
		}
		skill := requested
		if command == "update" && field(previous, "skill") != "" {
			skill = field(previous, "skill")
		}
		exported, err := adapter.Export(ExportRequest{Destination: dir, Source: source, Skill: skill, Name: name, Target: filepath.Join(target, name), Directory: directory, Previous: previous})
		if err != nil {
			return "", "", err
		}
		if command == "add" && (outputName != "" || previous["skill"] != nil) {
			exported["skill"] = skill
		}
		value.Skills[name] = exported
	}
	lock = filepath.Join(directory, "upstream.json")
	if err := value.writePrepared(directory, lock); err != nil {
		return "", "", err
	}
	return target, lock, nil
}

// Result is one entry of the public skill index search.
type Result struct {
	Name     string `json:"name"`
	Source   string `json:"source"`
	Installs *int   `json:"installs"`
	URL      string `json:"url"`
}

// Search is the bounded answer returned by Find.
type Search struct {
	Query   string   `json:"query"`
	Skills  []Result `json:"skills"`
	Skipped int      `json:"skipped"`
}

// Find queries the public skills.sh index with bounded JSON output and no
// installation. Unsupported sources and malformed entries are counted as
// skipped instead of failing the whole search.
func Find(query, owner string) (Search, error) {
	if strings.TrimSpace(query) == "" || strings.ContainsFunc(query, func(r rune) bool { return r < 32 }) {
		return Search{}, fmt.Errorf("find requires a non-empty query without control characters")
	}
	if owner != "" && !ownerPattern.MatchString(owner) {
		return Search{}, fmt.Errorf("invalid GitHub owner")
	}
	parameters := url.Values{"q": {query}, "limit": {"20"}}
	if owner != "" {
		parameters.Set("owner", owner)
	}
	results, err := searchIndex(searchURL + parameters.Encode())
	if err != nil {
		return Search{}, err
	}
	results.Query = query
	return results, nil
}

// searchURL and searchClient are variables so the parsing rules can be tested
// against a fixed response without contacting the public index.
var (
	searchURL    = "https://skills.sh/api/search?"
	searchClient = &http.Client{Timeout: 30 * time.Second}
)

func searchIndex(request string) (Search, error) {
	client := searchClient
	response, err := client.Get(request)
	if err != nil {
		return Search{}, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1_000_001))
	if err != nil {
		return Search{}, err
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return Search{}, fmt.Errorf("invalid search response")
	}
	list, ok := payload["skills"].([]any)
	if !ok {
		return Search{}, fmt.Errorf("invalid search response")
	}
	answer := Search{Skills: []Result{}}
	for index, entry := range list {
		if index >= 20 {
			break
		}
		object, ok := entry.(map[string]any)
		if !ok {
			answer.Skipped++
			continue
		}
		name, ok := object["name"].(string)
		if !ok || !Name(name) {
			answer.Skipped++
			continue
		}
		identifier, _ := object["source"].(string)
		source, err := Source(identifier)
		if err != nil {
			answer.Skipped++
			continue
		}
		var installs *int
		if count, ok := object["installs"].(float64); ok && count >= 0 {
			value := int(count)
			installs = &value
		}
		answer.Skills = append(answer.Skills, Result{
			Name: name, Source: source, Installs: installs,
			URL: "https://skills.sh/" + source + "/" + name,
		})
	}
	return answer, nil
}

func removeEnv(env []string, key string) []string {
	prefix := key + "="
	result := make([]string, 0, len(env))
	for _, entry := range env {
		if !strings.HasPrefix(entry, prefix) {
			result = append(result, entry)
		}
	}
	return result
}
