package toolchain_test

import (
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/wwwyo/skillctrl/internal/toolchain"
)

// TestRenderKeepsTablePinsReadable guards a runtime that resolves to nothing. A
// pin written as a table, as in `pi = { version = "1.2.3" }`, must survive being
// carried into the isolated configuration: rendering it as a literal would
// produce a file the resolver cannot read, and a reviewer would then run against
// whatever happens to be installed.
func TestRenderKeepsTablePinsReadable(t *testing.T) {
	document := `[tools]
node = "22.11.0"
"npm:@earendil-works/pi-coding-agent" = { version = "0.55.1" }
[settings]
pin = true
minimum_release_age = "7d"
`
	configuration := decode(t, document)
	output, err := toolchain.Render(configuration)
	if err != nil {
		t.Fatal(err)
	}
	// What comes out must be readable as TOML and mean the same thing.
	again := decode(t, string(output))
	for _, name := range toolchain.AgentTools {
		if _, ok := again.Tools[name]; !ok {
			t.Fatalf("rendered configuration lost %s:\n%s", name, output)
		}
	}
	if _, ok := again.Tools["npm:@earendil-works/pi-coding-agent"].(map[string]any); !ok {
		t.Fatalf("a table pin became a literal:\n%s", output)
	}
	if _, ok := again.Tools["node"].(string); !ok {
		t.Fatalf("a string pin became something else:\n%s", output)
	}
	for _, name := range toolchain.PolicySettings {
		if _, ok := again.Settings[name]; !ok {
			t.Fatalf("rendered configuration lost the %s policy:\n%s", name, output)
		}
	}
	if !strings.Contains(string(output), "[tools]") || !strings.Contains(string(output), "[settings]") {
		t.Fatalf("unexpected layout:\n%s", output)
	}
}

func decode(t *testing.T, document string) toolchain.Configuration {
	t.Helper()
	var parsed struct {
		Tools    map[string]any `toml:"tools"`
		Settings map[string]any `toml:"settings"`
	}
	if _, err := toml.Decode(document, &parsed); err != nil {
		t.Fatalf("the document is not valid TOML: %v\n%s", err, document)
	}
	return toolchain.Configuration{Tools: parsed.Tools, Settings: parsed.Settings}
}
