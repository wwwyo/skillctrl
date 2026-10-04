package upstream

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/wwwyo/skillctrl/internal/gitx"
)

// ExportRequest names the installation boundary and the immutable input identity.
type ExportRequest struct {
	Destination string
	Source      string
	Skill       string
	Name        string
	Target      string
	Directory   string
	Previous    map[string]any
}

// Adapter prepares originals in isolation; import protection and intent review
// remain independent of the acquisition backend.
type Adapter interface {
	Export(request ExportRequest) (map[string]any, error)
}

// NewAdapter selects an explicit backend; a missing dependency is an error,
// never a silent change of installation semantics.
func NewAdapter(name string) (Adapter, error) {
	switch name {
	case "skills", "gh":
		return &commandAdapter{name: name}, nil
	case "git":
		return NewGitAdapter(), nil
	default:
		return nil, fmt.Errorf("unknown adapter: %s (expected skills, gh, or git)", name)
	}
}

type gitAdapter struct{ repositories map[string]*Repository }

// NewGitAdapter retains the direct Git backend for existing integrations.
func NewGitAdapter() Adapter { return &gitAdapter{repositories: map[string]*Repository{}} }

func (adapter *gitAdapter) Export(request ExportRequest) (map[string]any, error) {
	destination, source, skill, destinationName, target, directory, previous := request.Destination, request.Source, request.Skill, request.Name, request.Target, request.Directory, request.Previous
	ref := field(previous, "ref")
	priorSource, _ := Source(field(previous, "source"))
	if !strings.EqualFold(priorSource, source) {
		ref = ""
	}
	key := source + "\x00" + ref
	repository := adapter.repositories[key]
	if repository == nil {
		var err error
		repository, err = newRepository(source, filepath.Join(directory, fmt.Sprintf("repo-%d", len(adapter.repositories))), ref)
		if err != nil {
			return nil, err
		}
		adapter.repositories[key] = repository
	}
	return repository.export(destination, skill, destinationName, target, previous)
}

// InstallWithAdapter prepares a regular import with the selected backend.
func InstallWithAdapter(dir, command string, selected []string, source, directory string, adapter Adapter) (string, string, error) {
	return install(dir, command, selected, source, directory, nil, adapter)
}

// MergeWithAdapter prepares each merged input through the same backend.
func MergeWithAdapter(dir, name string, inputs []Input, directory string, adapter Adapter) (string, string, error) {
	if err := validateInputs(inputs); err != nil {
		return "", "", err
	}
	return install(dir, "merge", []string{name}, "", directory, inputs, adapter)
}

type commandAdapter struct{ name string }

// FindWithAdapter delegates GitHub search to gh and uses the public index for
// skills, whose find command does not expose a JSON result in version 1.7.0.
func FindWithAdapter(name, query, owner string) (Search, error) {
	if name != "gh" {
		return Find(query, owner)
	}
	if strings.TrimSpace(query) == "" || strings.ContainsFunc(query, func(r rune) bool { return r < 32 }) {
		return Search{}, fmt.Errorf("invalid search query")
	}
	if owner != "" && !ownerPattern.MatchString(owner) {
		return Search{}, fmt.Errorf("invalid GitHub owner")
	}
	args := []string{"skill", "search", query, "--json", "skillName,repo,path", "--limit", "20"}
	if owner != "" {
		args = append(args, "--owner", owner)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "gh", args...)
	command.Env = removeEnv(gitx.Environment(), "OPENCODE_API_KEY")
	var stderr bytes.Buffer
	command.Stderr = &stderr
	out, err := command.Output()
	if err != nil {
		return Search{}, fmt.Errorf("gh search failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	var rows []struct {
		Name string `json:"skillName"`
		Repo string `json:"repo"`
		Path string `json:"path"`
	}
	if err := json.Unmarshal(out, &rows); err != nil {
		return Search{}, fmt.Errorf("invalid gh search JSON: %w", err)
	}
	result := Search{Query: query, Skills: []Result{}}
	for _, row := range rows {
		source, err := Source(row.Repo)
		if err != nil || !Name(row.Name) {
			result.Skipped++
			continue
		}
		result.Skills = append(result.Skills, Result{Name: row.Name, Source: source, URL: "https://github.com/" + source})
	}
	return result, nil
}

func (adapter *commandAdapter) Export(request ExportRequest) (map[string]any, error) {
	destination, source, skill, destinationName, target, directory, previous := request.Destination, request.Source, request.Skill, request.Name, request.Target, request.Directory, request.Previous
	if _, err := Source(source); err != nil {
		return nil, err
	}
	if !Name(skill) {
		return nil, fmt.Errorf("invalid skill name")
	}
	stage, err := os.MkdirTemp(directory, "adapter-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(stage)
	home := filepath.Join(stage, "home")
	if err := os.Mkdir(home, 0o700); err != nil {
		return nil, err
	}
	ref := field(previous, "ref")
	priorSource, _ := Source(field(previous, "source"))
	if !strings.EqualFold(priorSource, source) {
		ref = ""
	}
	if ref != "" {
		if err := gitx.SafeRun("", "check-ref-format", "--branch", ref); err != nil {
			return nil, fmt.Errorf("invalid upstream ref")
		}
	}
	var args []string
	switch adapter.name {
	case "skills":
		// codex selects the shared .agents/skills destination, independent of
		// installed hosts; full-depth prevents a root manifest hiding nested skills.
		identifier := "https://github.com/" + source
		if ref != "" {
			identifier += "/tree/" + ref
		}
		args = []string{"add", identifier, "--skill", skill, "--agent", "codex", "--copy", "--yes", "--full-depth"}
	case "gh":
		args = []string{"skill", "install", source, skill, "--dir", filepath.Join(stage, ".agents/skills"), "--force", "--allow-hidden-dirs"}
		if ref != "" {
			args = append(args, "--pin", ref)
		}
	}
	if err := runAdapter(adapter.name, stage, home, args); err != nil {
		return nil, err
	}
	// Native installers may use the frontmatter name rather than the requested
	// directory alias. Exactly one result must have been installed.
	entries, err := os.ReadDir(filepath.Join(stage, ".agents/skills"))
	if err != nil || len(entries) != 1 || !entries[0].IsDir() {
		return nil, fmt.Errorf("%s adapter must install exactly one real skill directory", adapter.name)
	}
	installed := entries[0].Name()
	if !Name(installed) {
		return nil, fmt.Errorf("invalid installed skill name")
	}
	metadataPath := filepath.Join(stage, Lock)
	if adapter.name == "gh" {
		metadataPath = filepath.Join(home, ".agents/.skill-lock.json")
	}
	data, err := os.ReadFile(metadataPath)
	if err != nil {
		return nil, fmt.Errorf("%s adapter did not produce source tracking: %w", adapter.name, err)
	}
	record, err := parseLock(data)
	if err != nil {
		return nil, err
	}
	metadata, ok := record.Skills[installed].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("adapter source tracking does not include installed skill")
	}
	skillPath := field(metadata, "skillPath")
	if strings.TrimSpace(skillPath) == "" {
		return nil, fmt.Errorf("%s adapter source tracking requires a non-empty skillPath", adapter.name)
	}
	actualSource, err := Source(field(metadata, "source"))
	if err != nil || !strings.EqualFold(actualSource, source) || metadata["sourceType"] != "github" {
		return nil, fmt.Errorf("adapter changed upstream source identity")
	}
	// Inspect downloaded files as Git objects, reusing the same mode, link,
	// ignored-file, and Git-control-file checks as the direct backend. No
	// upstream installer is run inside the caller's repository.
	skillDir := filepath.Join(stage, ".agents/skills", installed)
	if err := filepath.WalkDir(skillDir, func(current string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 || gitControlName(entry.Name()) {
			return fmt.Errorf("unsafe adapter skill file: %s", entry.Name())
		}
		if !entry.IsDir() && !entry.Type().IsRegular() {
			return fmt.Errorf("unexpected adapter skill file")
		}
		return nil
	}); err != nil {
		return nil, err
	}
	// Global attributes and filters must not rewrite the downloaded original.
	objectEnv := gitx.Environment("GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "GIT_ATTR_NOSYSTEM=1")
	if err := gitx.SafeEnv(stage, objectEnv, "init", "--quiet", "--template="); err != nil {
		return nil, err
	}
	if err := gitx.SafeEnv(stage, objectEnv, "-c", "core.autocrlf=false", "-c", "core.attributesFile=/dev/null", "add", "--force", "--", ".agents/skills"); err != nil {
		return nil, err
	}
	tree, err := gitx.Safe(stage, "write-tree")
	if err != nil {
		return nil, err
	}
	repository, err := inspectRepository(source, stage, gitx.Trimmed(tree), "")
	if err != nil {
		return nil, err
	}
	force := maps.Clone(previous)
	delete(force, "skillFolderHash")
	delete(force, "computedHash")
	result, files, contents, err := repository.inspectExport(destination, installed, destinationName, force)
	if err != nil {
		return nil, err
	}
	// Project locks remain at the caller's root. Backend-only global tracking
	// stays in the disposable home; metadata embedded by gh remains in SKILL.md.
	result["skillPath"] = skillPath
	delete(result, "sourceCommit")
	if ref != "" {
		result["ref"] = ref
	}
	if adapter.name == "skills" && field(metadata, "computedHash") != field(result, "computedHash") {
		return nil, fmt.Errorf("adapter content does not match its project lock")
	}
	if strings.EqualFold(priorSource, source) && (field(previous, "skillPath") == "" || field(previous, "skillPath") == field(result, "skillPath")) &&
		((field(previous, "skillFolderHash") != "" && field(previous, "skillFolderHash") == field(result, "skillFolderHash")) ||
			(field(previous, "skillFolderHash") == "" && field(previous, "computedHash") != "" && field(previous, "computedHash") == field(result, "computedHash"))) {
		return previous, nil
	}
	if err := writeExport(target, files, contents); err != nil {
		return nil, err
	}
	return result, nil
}

func runAdapter(name, directory, home string, args []string) error {
	executable, err := exec.LookPath(name)
	if err != nil {
		return fmt.Errorf("%s adapter requires '%s' on PATH; install it with mise", name, name)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, executable, args...)
	command.Dir = directory
	env := removeEnv(gitx.Environment(), "OPENCODE_API_KEY")
	if name == "gh" && os.Getenv("GH_TOKEN") == "" && os.Getenv("GITHUB_TOKEN") == "" {
		// Resolve Keychain-backed authentication before changing HOME. The token
		// stays in process memory/environment, never an artifact or diagnostic.
		auth := exec.CommandContext(ctx, executable, "auth", "token", "--hostname", "github.com")
		auth.Env = env
		if token, err := auth.Output(); err == nil && strings.TrimSpace(string(token)) != "" {
			env = append(removeEnv(env, "GH_TOKEN"), "GH_TOKEN="+strings.TrimSpace(string(token)))
		}
	}
	// Keep gh authentication available while isolating installer-owned state.
	config := os.Getenv("GH_CONFIG_DIR")
	if config == "" {
		if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
			config = filepath.Join(xdg, "gh")
		} else {
			callerHome, _ := os.UserHomeDir()
			config = filepath.Join(callerHome, ".config/gh")
		}
	}
	for _, key := range []string{"HOME", "XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_STATE_HOME", "GH_CONFIG_DIR"} {
		env = removeEnv(env, key)
	}
	command.Env = append(env, "HOME="+home, "XDG_CONFIG_HOME="+filepath.Join(home, ".config"), "XDG_CACHE_HOME="+filepath.Join(home, ".cache"), "XDG_STATE_HOME="+filepath.Join(home, ".state"), "GH_CONFIG_DIR="+config, "DO_NOT_TRACK=1", "CI=1", "GIT_TERMINAL_PROMPT=0")
	stderr := diagnosticTail{limit: 16 * 1024}
	command.Stdout = &stderr
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("%s adapter failed: %w\n%s", name, err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// Check prepares current originals without importing them or running a reviewer.
func Check(dir string, selected []string, directory string, adapter Adapter) ([]string, error) {
	before, err := Load(dir)
	if err != nil {
		return nil, err
	}
	_, lockPath, err := InstallWithAdapter(dir, "update", selected, "", directory, adapter)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(lockPath)
	if err != nil {
		return nil, err
	}
	after, err := parseLock(data)
	if err != nil {
		return nil, err
	}
	updates := []string{}
	for name, entry := range after.ManagedSkills() {
		previous, _ := json.Marshal(before.Skills[name])
		current, _ := json.Marshal(entry)
		if !bytes.Equal(previous, current) {
			updates = append(updates, name)
		}
	}
	return updates, nil
}

// diagnosticTail drains both streams while retaining only bounded failure context.
type diagnosticTail struct {
	data  []byte
	limit int
}

func (tail *diagnosticTail) Write(value []byte) (int, error) {
	count := len(value)
	if count >= tail.limit {
		tail.data = append(tail.data[:0], value[count-tail.limit:]...)
	} else {
		if len(tail.data)+count > tail.limit {
			tail.data = tail.data[len(tail.data)+count-tail.limit:]
		}
		tail.data = append(tail.data, value...)
	}
	return count, nil
}
func (tail *diagnosticTail) String() string { return string(tail.data) }
