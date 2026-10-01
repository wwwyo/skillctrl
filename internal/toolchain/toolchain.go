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
	"encoding/json"
	"fmt"
	"os"
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
func Trusted(dir, source, path string) (Configuration, error) {
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

// Render writes the minimal CI toolchain configuration.
func Render(configuration Configuration) []byte {
	lines := []string{"[tools]"}
	for _, name := range AgentTools {
		lines = append(lines, quote(name)+" = "+literal(configuration.Tools[name]))
	}
	lines = append(lines, "", "[settings]")
	for _, name := range PolicySettings {
		lines = append(lines, name+" = "+literal(configuration.Settings[name]))
	}
	return []byte(strings.Join(lines, "\n") + "\n")
}

func quote(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

func literal(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "null"
	}
	return string(encoded)
}

// Models reads the trusted agent model definitions. These let the reviewer run
// without credentials from the pull request's own configuration.
func Models(dir, source, path string) ([]byte, error) {
	data, err := gitx.Output(dir, "show", source+":"+path)
	if err != nil {
		return nil, fmt.Errorf("trusted agent model definitions are unavailable: %s", path)
	}
	return data, nil
}
