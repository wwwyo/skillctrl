// Package toolchain extracts the pinned reviewer toolchain from a trusted
// source tree.
//
// The pins live in the repository that owns the workflow, not in the incoming
// pull request. Only the Node runtime pin, the reviewer agent pin, and the two
// release-policy settings are carried across: copying the whole configuration
// would drag in unrelated tools and the maintainer's encrypted environment into
// an unauthenticated CI job.
package toolchain

import (
	"bytes"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/wwwyo/skillctrl/internal/gitx"
)

// Default trusted-source locations. They match the historical layout and are
// overridable so any repository can keep its toolchain configuration elsewhere.
const (
	DefaultConfig = "home/dot_config/mise/config.toml"
	DefaultModels = "home/dot_pi/agent/models.json"
)

// ConfigEnv and ModelsEnv override the trusted-source paths.
const (
	ConfigEnv = "SKILLCTRL_TOOLCHAIN_CONFIG"
	ModelsEnv = "SKILLCTRL_AGENT_MODELS"
)

// AgentTools are the tools the reviewer needs, in the order they are written.
var AgentTools = []string{"node", "npm:@earendil-works/pi-coding-agent"}

// PolicySettings are the release-policy values carried into the CI
// configuration. Without them the pinned versions would still be honored, but
// the cooldown that protects against supply-chain surprises would not.
var PolicySettings = []string{"pin", "minimum_release_age"}

// Configuration holds the trusted tool pins.
type Configuration struct {
	Tools    map[string]any
	Settings map[string]any
}

// commitish accepts only a full commit ID. Refs and short IDs are refused so a
// caller cannot pass something that later resolves to something else.
var commitish = regexp.MustCompile(`^[0-9a-f]{40}$`)

// ConfigPath returns the trusted configuration path, honoring the override.
func ConfigPath() string { return pathFromEnv(ConfigEnv, DefaultConfig) }

// ModelsPath returns the trusted agent model definition path.
func ModelsPath() string { return pathFromEnv(ModelsEnv, DefaultModels) }

func pathFromEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

// Trusted reads and validates the pinned toolchain from a trusted commit.
//
// The source is validated rather than interpolated: an empty value would make
// the revision parse as the index, which the pull request under review controls.
// That would turn a missing configuration variable into "trust the incoming
// code".
func Trusted(dir, source, path string) (Configuration, error) {
	if !commitish.MatchString(source) {
		return Configuration{}, fmt.Errorf("trusted source is not a commit: %q", source)
	}
	data, err := gitx.Output(dir, "show", source+":"+path)
	if err != nil {
		return Configuration{}, fmt.Errorf("trusted toolchain configuration is unavailable: %s", path)
	}
	var document struct {
		Tools    map[string]any `toml:"tools"`
		Settings map[string]any `toml:"settings"`
	}
	if _, err := toml.Decode(string(data), &document); err != nil {
		return Configuration{}, fmt.Errorf("trusted toolchain configuration is not valid TOML")
	}
	configuration := Configuration{Tools: document.Tools, Settings: document.Settings}
	for _, name := range AgentTools {
		if _, ok := configuration.Tools[name]; !ok {
			return Configuration{}, fmt.Errorf("trusted toolchain does not pin %s", name)
		}
	}
	for _, name := range PolicySettings {
		if _, ok := configuration.Settings[name]; !ok {
			return Configuration{}, fmt.Errorf("trusted toolchain does not set %s", name)
		}
	}
	return configuration, nil
}

// NodeVersion returns the trusted Node pin, which must be an exact version. A
// range or a missing pin would make the CI runtime unreproducible, so it is
// refused rather than resolved at run time.
func NodeVersion(configuration Configuration) (string, error) {
	value, ok := configuration.Tools["node"].(string)
	if !ok || !exactVersion(value) {
		return "", fmt.Errorf("trusted Node version must be an exact pin")
	}
	return value, nil
}

func exactVersion(value string) bool {
	parts := strings.Split(value, ".")
	if len(parts) != 3 {
		return false
	}
	for _, part := range parts {
		if part == "" || strings.Trim(part, "0123456789") != "" {
			return false
		}
	}
	return true
}

// document is the minimal configuration the reviewer runs under.
type document struct {
	Tools    map[string]any `toml:"tools"`
	Settings map[string]any `toml:"settings"`
}

// Render writes the minimal CI toolchain configuration.
//
// It goes through a TOML encoder rather than being formatted by hand: a pin can
// be a table, as in `pi = { version = "1.2.3" }`, and rendering that as a JSON
// literal would produce a file the resolver cannot read. Emitting a value the
// resolver later rejects turns a pinned runtime into a broken one, so the
// encoding has to be the real one.
func Render(configuration Configuration) ([]byte, error) {
	result := document{
		Tools:    make(map[string]any, len(AgentTools)),
		Settings: make(map[string]any, len(PolicySettings)),
	}
	for _, name := range AgentTools {
		result.Tools[name] = configuration.Tools[name]
	}
	for _, name := range PolicySettings {
		result.Settings[name] = configuration.Settings[name]
	}
	var buffer bytes.Buffer
	if err := toml.NewEncoder(&buffer).Encode(result); err != nil {
		return nil, fmt.Errorf("trusted toolchain cannot be rendered: %w", err)
	}
	return buffer.Bytes(), nil
}

// Models reads the trusted agent model definitions. These let the reviewer run
// without credentials from the pull request's own configuration.
func Models(dir, source, path string) ([]byte, error) {
	if !commitish.MatchString(source) {
		return nil, fmt.Errorf("trusted source is not a commit: %q", source)
	}
	data, err := gitx.Output(dir, "show", source+":"+path)
	if err != nil {
		return nil, fmt.Errorf("trusted agent model definitions are unavailable: %s", path)
	}
	return data, nil
}
