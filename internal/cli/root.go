// Package cli builds the command tree.
//
// The command surface is part of the tool's contract: existing scripts call
// these names with these flags, so the tree, the flags, and the JSON on stdout
// stay stable. Diagnostics go to stderr and never to stdout, because stdout is
// parsed as JSON by callers.
package cli

import (
	"errors"
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

// dryRunRejection explains why the CI phases refuse --dry-run. Those phases
// write the Git index, a lock file, and a remote pull request; there is no
// partial execution to offer, and silently ignoring the flag would let a caller
// believe nothing was published.
const dryRunRejection = "--dry-run applies to find, check, add, merge, update, remove, and record; " +
	"this phase writes the index, the lock, and the remote repository"

// rejectDryRun refuses a phase that has no defined dry-run semantics before it
// can perform any side effect.
func rejectDryRun(command *cobra.Command) error {
	if dry, _ := command.Flags().GetBool("dry-run"); dry {
		return fmt.Errorf(dryRunRejection)
	}
	return nil
}

// ExitUnresolved reports adaptation that could not be completed. It is a
// distinct exit code because the work is not lost: the change stays in the
// worktree and the old hash is retained so the next run retries.
const ExitUnresolved = 2

// exitError carries a chosen exit code out of a command.
type exitError struct {
	code int
}

func (e exitError) Error() string { return fmt.Sprintf("exit status %d", e.code) }

// Execute runs the command tree and returns the process exit code.
func Execute() int {
	root := New()
	if err := root.Execute(); err != nil {
		var code exitError
		if errors.As(err, &code) {
			return code.code
		}
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
		Long: "skillctrl installs and updates skills in a Git repository and re-adapts them\n" +
			"to the intent recorded in .agents/skillctrl/intents/. Upstream originals are\n" +
			"prepared by the selected skills adapter before review and import.",
		Version:       BuildVersion(),
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetOut(os.Stdout)
	root.SetErr(os.Stderr)
	root.CompletionOptions.DisableDefaultCmd = true

	root.PersistentFlags().String("repo", "", "repository to operate on (default: the current repository)")
	root.PersistentFlags().Bool("dry-run", false,
		"report what would happen without changing anything; applies to find, check, add, merge, update, remove, and record")
	root.PersistentFlags().String("worktree-provider", "",
		"worktree isolation backend for add, merge, update and remove: git or orca")

	root.PersistentFlags().String("adapter", "", "skills backend: skills (default), gh, or git; also SKILLCTRL_ADAPTER")
	root.AddGroup(&cobra.Group{ID: "skills", Title: "Skill management:"}, &cobra.Group{ID: "intent", Title: "Intent management:"}, &cobra.Group{ID: "automation", Title: "Automation:"})
	root.SetHelpCommandGroupID("automation")
	root.AddCommand(
		newListCommand(),
		newCheckCommand(),
		newStatusCommand(),
		newSchemaCommand(),
		newFindCommand(),
		newAddCommand(),
		newMergeCommand(),
		newUpdateCommand(),
		newRemoveCommand(),
		newRecordCommand(),
		newPlanCommand(),
		newCICommand(),
		newScheduleCommand(),
		newPromptCommand(),
	)
	for _, command := range root.Commands() {
		switch command.Name() {
		case "find", "add", "list", "check", "update", "remove":
			command.GroupID = "skills"
		case "status", "record", "merge":
			command.GroupID = "intent"
		default:
			command.GroupID = "automation"
		}
	}
	return root
}
