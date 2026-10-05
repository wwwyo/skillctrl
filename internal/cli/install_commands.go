package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/wwwyo/skillctrl/internal/gitx"
	"github.com/wwwyo/skillctrl/internal/install"
	"github.com/wwwyo/skillctrl/internal/jsonfmt"
	"github.com/wwwyo/skillctrl/internal/lock"
	"github.com/wwwyo/skillctrl/internal/upstream"
)

// repository resolves the checkout containing the current working directory.
func repository() (string, error) {
	out, err := gitx.Output("", "rev-parse", "--show-toplevel")
	if err != nil {
		return os.Getwd()
	}
	return gitx.Trimmed(out), nil
}

// emit writes one JSON document to stdout.
func emit(value any) error { return jsonfmt.Print(value) }

// fail reports a failure as a single JSON object on stderr and exits 1.
func fail(repository string, err error) error {
	payload, _ := jsonfmt.Compact(map[string]any{"ok": false, "repo": repository, "error": err.Error()})
	fmt.Fprint(os.Stderr, string(payload))
	return silenceError{err}
}

func newFindCommand() *cobra.Command {
	command := &cobra.Command{
		Use:     "find query...",
		Aliases: []string{"search"},
		Short:   "Search the public skills index without installing anything",
		Args:    cobra.MinimumNArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			owner, _ := command.Flags().GetString("owner")
			dry, _ := command.Flags().GetBool("dry-run")
			query := strings.Join(args, " ")
			if dry {
				return emit(map[string]any{"dry_run": true, "query": query, "owner": owner})
			}
			if _, err := selectedAdapter(command); err != nil {
				return fail("", err)
			}
			result, err := upstream.FindWithAdapter(adapterName(command), query, owner)
			if err != nil {
				return fail("", err)
			}
			return emit(result)
		},
	}
	command.Flags().String("owner", "", "restrict results to one GitHub owner")
	return command
}

func newAddCommand() *cobra.Command {
	command := &cobra.Command{
		Use:     "add owner/repo:skill...",
		Aliases: []string{"install", "a"},
		Short:   "Import and register upstream skills without intent review",
		Long: "Import separate skills using positional owner/repo:skill inputs,\n" +
			"the same input syntax as merge. Alternatively, use source --skill names\n" +
			"for the acquisition CLI's syntax. Use --name only with one selected skill.",
		Args: cobra.MinimumNArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			if !command.Flags().Changed("skill") {
				inputs, err := upstream.ParseInputs(args)
				if err != nil {
					return fail("", err)
				}
				names := make([]string, len(inputs))
				for index, input := range inputs {
					names[index] = input.Skill
				}
				return runInstall(command, "add", "", names, inputs...)
			}
			if len(args) != 1 {
				return fmt.Errorf("--skill requires exactly one repository source; otherwise use owner/repo:skill inputs")
			}
			if _, err := upstream.Source(args[0]); err != nil {
				return fail("", err)
			}
			skills, _ := command.Flags().GetStringSlice("skill")
			if len(skills) == 0 {
				return fmt.Errorf("at least one --skill is required")
			}
			return runInstall(command, "add", args[0], skills)
		},
	}
	command.Flags().String("name", "", "local directory name; requires exactly one selected skill")
	command.Flags().StringSlice("skill", nil, "skill to import from the positional source; repeatable")
	return command
}

func newUpdateCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "update [names...]",
		Short: "Refresh registered originals without intent review",
		RunE: func(command *cobra.Command, args []string) error {
			return runInstall(command, "update", "", args)
		},
	}
}

func newMergeCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "merge owner/repo:skill... --name NAME",
		Short: "Register upstream skills under one routing skill without intent review",
		Long: "Register named GitHub originals under one repository-local routing skill.\n" +
			"Use positional owner/repo:skill inputs and --name for the routing skill.\n" +
			"The inputs replace the target's sources array;\n" +
			"update subsequently refreshes every source. No intent or reviewer is required.",
		Args: cobra.MinimumNArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			name, _ := command.Flags().GetString("name")
			if !upstream.Name(name) {
				return fmt.Errorf("merge requires a valid --name")
			}
			inputs, err := upstream.ParseInputs(args)
			if err != nil {
				return fail("", err)
			}
			return runInstall(command, "merge", "", []string{name}, inputs...)
		},
	}
	command.Flags().String("name", "", "local routing skill name")
	_ = command.MarkFlagRequired("name")
	return command
}

func newRemoveCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "remove names...",
		Aliases: []string{"rm"},
		Short:   "Remove skills, saved intents, and upstream registrations",
		Args:    cobra.MinimumNArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			return runInstall(command, "remove", "", args)
		},
	}
}

// runInstall performs the shared add, merge, update, remove, and record flow.
func runInstall(command *cobra.Command, kind, source string, requested []string, inputs ...upstream.Input) error {
	repo, err := repository()
	if err != nil {
		return fail("", err)
	}
	if _, err := os.Stat(filepath.Join(repo, filepath.FromSlash(install.SkillsDir))); err != nil {
		return fail(repo, fmt.Errorf("the current repository must contain %s", install.SkillsDir))
	}
	values, err := install.Names(requested)
	if err != nil {
		return fail(repo, err)
	}
	outputName := ""
	if kind == "add" {
		outputName, _ = command.Flags().GetString("name")
		if command.Flags().Changed("name") && (len(values) != 1 || !upstream.Name(outputName)) {
			return fail(repo, fmt.Errorf("--name requires exactly one selected skill and a plain directory name"))
		}
	}
	identifier := ""
	if kind == "add" && len(inputs) == 0 {
		identifier, err = upstream.Source(source)
		if err != nil {
			return fail(repo, err)
		}
	}
	dry, _ := command.Flags().GetBool("dry-run")
	if dry {
		result := map[string]any{"dry_run": true, "repo": repo, "command": kind,
			"source": source, "skills": values}
		if len(inputs) > 0 {
			result["sources"] = inputs
		}
		if outputName != "" {
			result["name"] = outputName
		}
		return emit(result)
	}
	if kind == "record" {
		return runRecord(repo, values)
	}
	adapter, err := selectedAdapter(command)
	if err != nil {
		return fail(repo, err)
	}
	isolated, err := os.MkdirTemp("", "skillctrl-install-")
	if err != nil {
		return fail(repo, err)
	}
	defer os.RemoveAll(isolated)
	if err := install.PrepareSkills(repo); err != nil {
		return fail(repo, err)
	}
	if err := install.CheckSkills(filepath.Join(repo, filepath.FromSlash(install.SkillsDir))); err != nil {
		return fail(repo, err)
	}
	var target, upstreamLock string
	if kind == "merge" {
		target, upstreamLock, err = upstream.MergeWithAdapter(repo, values[0], inputs, isolated, adapter)
	} else if kind == "add" && len(inputs) > 0 {
		target, upstreamLock, err = upstream.AddInputsWithAdapter(repo, inputs, outputName, isolated, adapter)
	} else if outputName != "" {
		target, upstreamLock, err = upstream.AddNamedWithAdapter(repo, values[0], outputName, identifier, isolated, adapter)
	} else {
		target, upstreamLock, err = upstream.InstallWithAdapter(repo, kind, values, identifier, isolated, adapter)
	}
	if err != nil {
		return fail(repo, err)
	}
	if outputName != "" {
		values = []string{outputName}
	}
	if err := install.CheckSkills(target); err != nil {
		return fail(repo, err)
	}
	if err := install.Import(repo, target, upstreamLock); err != nil {
		return fail(repo, err)
	}
	if kind == "remove" {
		if err := pruneAcceptance(repo); err != nil {
			return fail(repo, err)
		}
	}
	return emit(map[string]any{"repo": repo, "skills": values})
}

// runRecord accepts deliberate manual edits in place. It must not create a
// worktree and must not touch staging: the caller keeps ownership of its index
// and commits, and a pending edit is exactly what is being recorded.
func runRecord(repo string, values []string) error {
	if len(values) == 0 {
		if err := pruneAcceptance(repo); err != nil {
			return fail(repo, err)
		}
		return emit(map[string]any{"repo": repo, "recorded": []string{}})
	}
	tree, err := lock.WorkingTree(repo)
	if err != nil {
		return fail(repo, err)
	}
	current, err := lock.Snapshot(repo, tree)
	if err != nil {
		return fail(repo, err)
	}
	recorded, err := lock.Local(repo)
	if err != nil {
		return fail(repo, err)
	}
	registered, err := upstream.Read(repo, tree)
	if err != nil {
		return fail(repo, err)
	}
	unknown := []string{}
	intents, err := install.Intents(repo)
	if err != nil {
		return fail(repo, err)
	}
	managed := lock.IntentRegistered(registered.ManagedSkills(), intents)
	for _, name := range values {
		if _, ok := managed[name]; !ok {
			unknown = append(unknown, name)
		}
	}
	if len(unknown) > 0 {
		return fail(repo, fmt.Errorf("skills require a registered upstream and saved intent: %s", strings.Join(unknown, ", ")))
	}
	if _, err := lock.Record(repo, current, recorded, values, managed); err != nil {
		return fail(repo, err)
	}
	return emit(map[string]any{"repo": repo, "recorded": values})
}

func newRecordCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "record [names...]",
		Short: "Compute and write current skill-directory hashes to the lock",
		Long: "Compute the current whole-directory hash for each named skill and create or\n" +
			"replace its lock entry. Names select skills, not hashes. Registered upstreams\n" +
			"and saved intent are required. Verify content first; no reviewer runs.\n" +
			"With no names, only remove ineligible lock entries; eligible hashes stay unchanged.",
		Args: cobra.ArbitraryArgs,
		RunE: func(command *cobra.Command, args []string) error {
			return runInstall(command, "record", "", args)
		},
	}
}

func pruneAcceptance(repo string) error {
	if _, err := os.Lstat(filepath.Join(repo, lock.Lock)); os.IsNotExist(err) {
		return nil
	}
	recorded, err := lock.Local(repo)
	if err != nil {
		return err
	}
	registered, err := upstream.Load(repo)
	if err != nil {
		return err
	}
	intents, err := install.Intents(repo)
	if err != nil {
		return err
	}
	_, err = lock.Record(repo, lock.Empty(), recorded, nil, lock.IntentRegistered(registered.ManagedSkills(), intents))
	return err
}
