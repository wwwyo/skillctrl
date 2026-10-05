// Package cli builds the command tree.
//
// The command surface is part of the tool's contract: existing scripts call
// these names with these flags, so the tree, the flags, and the JSON on stdout
// stay stable. Diagnostics go to stderr and never to stdout, because stdout is
// parsed as JSON by callers.
package cli

import (
	"fmt"
	"os"
	"runtime/debug"

	"github.com/spf13/cobra"
)

// Version is the released version, overridden at build time with
// -ldflags "-X github.com/wwwyo/skillctrl/internal/cli.Version=...".
//
// A plain `go install github.com/wwwyo/skillctrl@v0.1.0` carries no ldflags, so
// the module version recorded in the binary is used instead. Without that
// fallback the documented install path would report "dev".
var Version = "dev"

// BuildVersion reports the version a user should see: the linker value when one
// was injected, otherwise the module version the binary was built from.
func BuildVersion() string {
	if Version != "dev" {
		return Version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return Version
}

// Execute runs the command tree and returns the process exit code.
func Execute() int {
	root := New()
	if err := root.Execute(); err != nil {
		if silent, ok := err.(silenceError); ok {
			// The failure was already reported as JSON on stderr.
			_ = silent
			return 1
		}
		// SilenceErrors keeps Cobra from reporting argument and flag problems, so
		// they are reported here instead. Silently exiting would leave a caller
		// with a non-zero status and no explanation.
		fmt.Fprintf(os.Stderr, "skillctrl: %v\nRun 'skillctrl --help' for usage.\n", err)
		return 1
	}
	return 0
}

// silenceError marks a failure whose message was already reported as JSON.
type silenceError struct{ error }

// New builds the root command.
func New() *cobra.Command {
	root := &cobra.Command{
		Use:   "skillctrl",
		Short: "Manage agent skills while preserving locally recorded intent",
		Long: "skillctrl imports and registers skills through the selected acquisition adapter.\n" +
			"Edit skills and intent files directly, then use record to compute and save verified skill hashes.\n" +
			"Use the same commands locally and in external automation.",
		Version:       BuildVersion(),
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetOut(os.Stdout)
	root.SetErr(os.Stderr)
	root.CompletionOptions.DisableDefaultCmd = true

	root.PersistentFlags().Bool("dry-run", false,
		"report what would happen without changing anything; applies to find, check, add, merge, update, remove, and record")

	registerAdapterFlag(root)
	root.AddGroup(&cobra.Group{ID: "skills", Title: "Skill management:"}, &cobra.Group{ID: "intent", Title: "Local acceptance:"}, &cobra.Group{ID: "help", Title: "Help:"})
	root.SetHelpCommandGroupID("help")
	root.AddCommand(
		newListCommand(),
		newCheckCommand(),
		newFindCommand(),
		newAddCommand(),
		newMergeCommand(),
		newUpdateCommand(),
		newRemoveCommand(),
		newRecordCommand(),
	)
	for _, command := range root.Commands() {
		switch command.Name() {
		case "find", "add", "merge", "list", "check", "update", "remove":
			command.GroupID = "skills"
		case "record":
			command.GroupID = "intent"
		default:
			command.GroupID = "help"
		}
	}
	return root
}
