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
func ReviewLocal(options Options) error {
	if err := ValidateHead(options.Dir, options.Plan); err != nil {
		return err
	}
	before, err := lock.WorkingTree(options.Dir, ".")
	if err != nil {
		return err
	}
	state := filepath.Join(options.Directory, "agent")
	if err := os.MkdirAll(state, 0o755); err != nil {
		return err
	}
	models, err := toolchain.Models(options.Dir, options.Source, options.ModelsPath)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(state, "models.json"), models, 0o644); err != nil {
		return err
	}
	configuration, err := toolchain.Trusted(options.Dir, options.Source, options.ConfigPath)
	if err != nil {
		return err
	}
	// CI already ran Prepare, so this rewrite is a no-op there; writing it here
	// keeps the direct local path self-contained.
	if err := os.WriteFile(filepath.Join(options.Directory, "mise.toml"), toolchain.Render(configuration), 0o644); err != nil {
		return err
	}
	prompt := options.Prompt +
		"\nSelection plan (input data):\n" + mustJSON(options.Plan) +
		"\nWrite completion JSON to: " + filepath.Join(options.Directory, ResultFile)

	environment, err := toolchainEnvironment(options.Directory)
	if err != nil {
		return err
	}
	environment = append(environment, "PI_CODING_AGENT_DIR="+state)

	command := os.Getenv(CommandEnv)
	if command == "" {
		command = DefaultCommand
	}
	// The reviewer must be found on the trusted PATH, not on whatever the caller
	// happened to have: resolving against the inherited PATH would run a binary
	// the trusted configuration never pinned.
	resolved, err := resolveCommand(environment, command)
	if err != nil {
		return err
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
		return err
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
		return runErr
	}
	if err := ValidateHead(options.Dir, options.Plan); err != nil {
		return err
	}
	after, err := lock.WorkingTree(options.Dir, ".")
	if err != nil {
		return err
	}
	prefixes := make([]string, 0, len(options.Plan.ReviewSkills))
	for _, name := range options.Plan.ReviewSkills {
		prefixes = append(prefixes, lock.Skills+name+"/")
	}
	out, err := gitx.Output(options.Dir, "diff", "--name-only", "-z", "--no-renames", before, after)
	if err != nil {
		return err
	}
	for _, raw := range strings.Split(string(out), "\x00") {
		if raw == "" {
			continue
		}
		if !hasPrefix(raw, prefixes) {
			return fmt.Errorf("reviewer changed a path outside reviewed skills: %s", raw)
		}
	}
	// The credential to screen for is the one the reviewer actually received,
	// which the trusted toolchain may have supplied.
	return WriteRepairArtifact(options.Dir, options.Plan, options.Directory,
		environmentValue(environment, "OPENCODE_API_KEY"))
}

// toolchainEnvironment resolves the trusted toolchain without evaluating the
// incoming repository's configuration. It runs in the directory holding the
// prepared trusted configuration, never in the caller's directory and never in
// the checkout under review: resolving in either of those would load an
// untrusted mise.toml and put whatever it names on the reviewer's PATH.
//
// Resolved values replace inherited ones. Leaving an inherited PATH in place
// would silently ignore the trusted pins and run the reviewer against whatever
// the caller had installed.
func toolchainEnvironment(directory string) ([]string, error) {
	resolver := exec.Command("mise", "env", "--json")
	resolver.Dir = directory
	out, err := resolver.Output()
	if err != nil {
		return nil, fmt.Errorf("trusted toolchain is unavailable: mise env failed")
	}
	var parsed map[string]string
	if err := json.Unmarshal(out, &parsed); err != nil {
		return nil, fmt.Errorf("trusted toolchain is unavailable: mise env returned invalid JSON")
	}
	merged := map[string]string{}
	for _, entry := range os.Environ() {
		if key, value, ok := strings.Cut(entry, "="); ok {
			merged[key] = value
		}
	}
	for key, value := range parsed {
		merged[key] = value
	}
	return renderEnvironment(merged), nil
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

// resolveCommand finds an executable using the given environment's PATH.
func resolveCommand(environment []string, command string) (string, error) {
	if strings.ContainsRune(command, os.PathSeparator) {
		return command, nil
	}
	for _, directory := range strings.Split(environmentValue(environment, "PATH"), string(os.PathListSeparator)) {
		if directory == "" {
			continue
		}
		candidate := filepath.Join(directory, command)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() && info.Mode().Perm()&0o111 != 0 {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("trusted toolchain does not provide %s", command)
}

func mustJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(data)
}
