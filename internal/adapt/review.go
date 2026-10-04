package adapt

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/wwwyo/skillctrl/internal/gitx"
	"github.com/wwwyo/skillctrl/internal/lock"
	"github.com/wwwyo/skillctrl/internal/toolchain"
)

// Reviewer defaults. The model is a deliberate exception to the usual model
// policy: adaptation is a bounded mechanical repair with a mechanically verified
// patch, so the cheapest sufficient model is used rather than a stronger one.
const (
	DefaultModel    = "opencode-go/space-bunny-free"
	DefaultThinking = "high"
)

// Env overrides for the reviewer invocation. They exist so a repository can
// point at its own reviewer without changing this tool.
const (
	ModelEnv    = "SKILLCTRL_ADAPT_MODEL"
	ThinkingEnv = "SKILLCTRL_ADAPT_THINKING"
	CommandEnv  = "SKILLCTRL_ADAPT_COMMAND"

	// DefaultCommand is the reviewing agent resolved from the trusted toolchain.
	DefaultCommand = "pi"
)

// Options configure a local review run.
type Options struct {
	// Dir is the repository checkout being reviewed.
	Dir string
	// Plan is the immutable selection.
	Plan lock.Plan
	// Directory holds the review artifacts.
	Directory string
	// Source is the trusted commit holding the checker, toolchain, and models.
	Source string
	// Prompt is the review contract.
	Prompt string
	// ConfigPath and ModelsPath locate trusted-source files.
	ConfigPath string
	ModelsPath string
}

// ReviewLocal launches local CLI adaptation with an isolated reviewer agent and
// the trusted toolchain. Isolation matters twice: the agent must not read the
// incoming repository's own mise configuration or agent settings, and its output
// is only ever a candidate patch.
func ReviewLocal(options Options) ([]string, error) {
	if err := ValidateHead(options.Dir, options.Plan); err != nil {
		return nil, err
	}
	before, err := lock.WorkingTree(options.Dir, ".")
	if err != nil {
		return nil, err
	}
	state := filepath.Join(options.Directory, "agent")
	if err := os.MkdirAll(state, 0o755); err != nil {
		return nil, err
	}
	models, err := toolchain.Models(options.Dir, options.Source, options.ModelsPath)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(state, "models.json"), models, 0o644); err != nil {
		return nil, err
	}
	configuration, err := toolchain.Trusted(options.Dir, options.Source, options.ConfigPath)
	if err != nil {
		return nil, err
	}
	// CI already ran Prepare, so this rewrite is a no-op there; writing it here
	// keeps the direct local path self-contained.
	rendered, err := toolchain.Render(configuration)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(options.Directory, "mise.toml"), rendered, 0o644); err != nil {
		return nil, err
	}
	prompt := options.Prompt +
		"\nSelection plan (input data):\n" + mustJSON(options.Plan) +
		"\nWrite completion JSON to: " + filepath.Join(options.Directory, ResultFile)

	environment, err := toolchainEnvironment(options.Directory)
	if err != nil {
		return nil, err
	}
	// The agent's own state directory is the only thing added on top of the
	// reduced environment.
	index := filepath.Join(state, "index")
	if err := gitx.RunEnv(options.Dir, gitx.Environment("GIT_INDEX_FILE="+index), "read-tree", "HEAD"); err != nil {
		return nil, err
	}
	environment = append(environment, "PI_CODING_AGENT_DIR="+state, "GIT_INDEX_FILE="+index)

	command := os.Getenv(CommandEnv)
	if command == "" {
		command = DefaultCommand
	}
	// The reviewer must be found on the trusted PATH, not on whatever the caller
	// happened to have: resolving against the inherited PATH would run a binary
	// the trusted configuration never pinned.
	resolved, err := resolveCommand(environment, command)
	if err != nil {
		return nil, err
	}
	model := os.Getenv(ModelEnv)
	if model == "" {
		model = DefaultModel
	}
	thinking := os.Getenv(ThinkingEnv)
	if thinking == "" {
		thinking = DefaultThinking
	}
	report, err := os.Create(filepath.Join(options.Directory, ReportFile))
	if err != nil {
		return nil, err
	}
	run := exec.Command(resolved,
		"--model", model, "--thinking", thinking,
		// No session, context files, skills, extensions, prompt templates, or
		// auto-approval: the reviewer sees only the trusted prompt and the
		// checkout it is told to repair.
		"--no-session", "--no-context-files", "--no-skills", "--no-extensions",
		"--no-prompt-templates", "--no-approve", "-p", prompt)
	// The reviewer must run against the checkout under review, not against
	// whichever directory the caller happened to invoke the tool from.
	run.Dir = options.Dir
	run.Env = environment
	run.Stdout = report
	run.Stderr = os.Stderr
	runErr := run.Run()
	_ = report.Close()
	if runErr != nil {
		return nil, runErr
	}
	if err := ValidateHead(options.Dir, options.Plan); err != nil {
		return nil, err
	}
	after, err := lock.WorkingTree(options.Dir, ".")
	if err != nil {
		return nil, err
	}
	prefixes := make([]string, 0, len(options.Plan.ReviewSkills))
	for _, name := range options.Plan.ReviewSkills {
		prefixes = append(prefixes, lock.Skills+name+"/")
	}
	out, err := gitx.Output(options.Dir, "diff", "--name-only", "-z", "--no-renames", before, after)
	if err != nil {
		return nil, err
	}
	for _, raw := range strings.Split(string(out), "\x00") {
		if raw == "" {
			continue
		}
		if !hasPrefix(raw, prefixes) {
			return nil, fmt.Errorf("reviewer changed a path outside reviewed skills: %s", raw)
		}
	}
	// The credential to screen for is the one the reviewer actually received,
	// which the trusted toolchain may have supplied.
	paths := make([]string, 0, len(options.Plan.Skills))
	for _, name := range options.Plan.Skills {
		paths = append(paths, lock.Skills+name)
	}
	tree, err := lock.WorkingTree(options.Dir, paths...)
	if err != nil {
		return nil, err
	}
	return writeRepairArtifactAtTree(options.Dir, options.Plan, options.Directory,
		environmentValue(environment, "OPENCODE_API_KEY"), tree)
}

// processEnvironment is the whole of what a reviewer inherits from the caller:
// the variables a process needs to start at all, and nothing else.
//
// A reviewer is the least trusted process in the tool. It runs against a
// repository chosen by someone else, so anything carried in from the caller's
// environment - an injected PATH entry, a write token, a session-tracing secret
// - is an input the reviewer should not be handed. The toolchain it must run
// comes from the trusted configuration, and the inference credential is passed
// explicitly.
var processEnvironment = []string{
	"PATH", "HOME", "TMPDIR", "TMP", "TEMP", "SHELL", "USER", "LOGNAME",
	"TERMINFO", "LANG", "LC_ALL", "LC_CTYPE", "TZ",
}

// resolverEnvironment adds what the toolchain resolver itself needs. A global
// mise configuration may be encrypted, and decrypting it requires the user's age
// key. That key is for the resolver only: the reviewer never receives it, which
// is why the two environments are built separately instead of one being derived
// from the other by deleting keys.
var resolverEnvironment = []string{"MISE_AGE_KEY"}

// credentialName is the inference credential. It is the one secret a reviewer
// needs, and the one whose bytes must not appear in anything the reviewer
// produces.
const credentialName = "OPENCODE_API_KEY"

// toolchainEnvironment resolves the trusted toolchain and builds the reviewer's
// environment from it.
//
// The resolver runs in the directory holding the prepared trusted configuration,
// never in the caller's directory and never in the checkout under review:
// resolving in either of those would load an untrusted mise.toml. Its own
// environment is reduced first, so an untrusted configuration cannot inherit a
// credential, and its output is reduced again, so what the configuration injects
// does not reach the reviewer either.
func toolchainEnvironment(directory string) ([]string, error) {
	resolver := exec.Command("mise", "env", "--json")
	resolver.Dir = directory
	resolver.Env = renderEnvironment(reduced(os.Environ(), append(processEnvironment, resolverEnvironment...)))
	out, err := resolver.Output()
	if err != nil {
		return nil, fmt.Errorf("trusted toolchain is unavailable: mise env failed")
	}
	var resolved map[string]string
	if err := json.Unmarshal(out, &resolved); err != nil {
		return nil, fmt.Errorf("trusted toolchain is unavailable: mise env returned invalid JSON")
	}
	environment := reduced(os.Environ(), processEnvironment)
	// PATH comes from the trusted configuration and replaces the inherited one.
	// Leaving the inherited value in place would run the reviewer against
	// whatever the caller had installed, which is the opposite of pinning.
	if path, ok := resolved["PATH"]; ok && path != "" {
		environment["PATH"] = path
	}
	if environment["PATH"] == "" {
		return nil, fmt.Errorf("trusted toolchain does not provide PATH")
	}
	// The trusted configuration decides which credential the reviewer gets. The
	// caller's value is a fallback for CI, where the job holds the credential
	// itself and the trusted configuration has none to offer.
	if key := resolved[credentialName]; key != "" {
		environment[credentialName] = key
	} else if key := os.Getenv(credentialName); key != "" {
		environment[credentialName] = key
	}
	return renderEnvironment(environment), nil
}

// reduced keeps only the named variables. The caller's environment is treated as
// untrusted input even though the caller started the process: the point of the
// boundary is that the reviewer sees the trusted toolchain and nothing else.
func reduced(environment []string, allowed []string) map[string]string {
	permitted := make(map[string]bool, len(allowed))
	for _, name := range allowed {
		permitted[name] = true
	}
	result := make(map[string]string, len(allowed))
	for _, entry := range environment {
		key, value, ok := strings.Cut(entry, "=")
		if ok && permitted[key] {
			result[key] = value
		}
	}
	return result
}

func renderEnvironment(values map[string]string) []string {
	keys := slices.Sorted(maps.Keys(values))
	environment := make([]string, 0, len(keys))
	for _, key := range keys {
		environment = append(environment, key+"="+values[key])
	}
	return environment
}

func environmentValue(environment []string, key string) string {
	for _, entry := range environment {
		if name, value, ok := strings.Cut(entry, "="); ok && name == key {
			return value
		}
	}
	return ""
}

// environmentNames lists the variables an environment carries, for diagnostics
// and for tests that assert what did not survive.
func environmentNames(environment []string) map[string]bool {
	names := make(map[string]bool, len(environment))
	for _, entry := range environment {
		if name, _, ok := strings.Cut(entry, "="); ok {
			names[name] = true
		}
	}
	return names
}

// resolveCommand finds an executable using the given environment's PATH.
//
// Only absolute PATH entries are searched. A relative entry would be resolved
// against the reviewer's working directory - the repository under review - so a
// name in such an entry would be looked up in files the pull request chose.
func resolveCommand(environment []string, command string) (string, error) {
	if command == "" {
		return "", fmt.Errorf("no reviewer command is configured")
	}
	if strings.ContainsRune(command, os.PathSeparator) || command == "." || command == ".." {
		// A path is only honored inside the trusted toolchain. Allowing an
		// arbitrary absolute path would hand the reviewer whatever the caller's
		// environment named, which is the boundary this package maintains.
		resolved, err := executable(command)
		if err != nil {
			return "", err
		}
		if !underTrustedPath(resolved, environment) {
			return "", fmt.Errorf("reviewer command is outside the trusted toolchain: %q", command)
		}
		return resolved, nil
	}
	for _, directory := range strings.Split(environmentValue(environment, "PATH"), string(os.PathListSeparator)) {
		if directory == "" || !filepath.IsAbs(directory) {
			continue
		}
		candidate, err := executable(filepath.Join(directory, command))
		if err == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("trusted toolchain does not provide %s", command)
}

// underTrustedPath reports whether a path lives inside one of the absolute
// entries of the trusted PATH.
func underTrustedPath(path string, environment []string) bool {
	for _, directory := range strings.Split(environmentValue(environment, "PATH"), string(os.PathListSeparator)) {
		if directory == "" || !filepath.IsAbs(directory) {
			continue
		}
		prefix := strings.TrimSuffix(directory, string(os.PathSeparator)) + string(os.PathSeparator)
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

func executable(path string) (string, error) {
	resolved, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil || info.IsDir() || info.Mode().Perm()&0o111 == 0 {
		return "", fmt.Errorf("%s is not an executable file", path)
	}
	return resolved, nil
}

func mustJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(data)
}
