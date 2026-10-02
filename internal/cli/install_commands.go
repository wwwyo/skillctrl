package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/wwwyo/skillctrl/internal/adapt"
	"github.com/wwwyo/skillctrl/internal/gitx"
	"github.com/wwwyo/skillctrl/internal/install"
	"github.com/wwwyo/skillctrl/internal/jsonfmt"
	"github.com/wwwyo/skillctrl/internal/lock"
	"github.com/wwwyo/skillctrl/internal/toolchain"
	"github.com/wwwyo/skillctrl/internal/upstream"
)

// repository resolves the checkout a command operates on. An explicit --repo
// always wins. Otherwise the current repository is used, which is what makes the
// tool usable outside the repository it was originally written for.
func repository(command *cobra.Command) (string, error) {
	if value, _ := command.Flags().GetString("repo"); value != "" {
		absolute, err := filepath.Abs(value)
		if err != nil {
			return "", err
		}
		return absolute, nil
	}
	out, err := gitx.Output("", "rev-parse", "--show-toplevel")
	if err != nil {
		working, getwdErr := os.Getwd()
		if getwdErr != nil {
			return "", getwdErr
		}
		return working, nil
	}
	candidate := gitx.Trimmed(out)
	if _, err := os.Stat(filepath.Join(candidate, filepath.FromSlash(install.SkillsDir))); err == nil {
		return candidate, nil
	}
	working, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return working, nil
}

// emit writes one JSON document to stdout.
func emit(value any) error { return jsonfmt.Print(value) }

// fail reports a failure as a single JSON object on stderr and exits 1.
func fail(repository string, err error) error {
	payload, _ := jsonfmt.Compact(map[string]any{"ok": false, "repo": repository, "error": err.Error()})
	fmt.Fprint(os.Stderr, string(payload))
	return silenceError{err}
}

func newStatusCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show which installed skills differ from the accepted lock",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, args []string) error {
			repo, err := repository(command)
			if err != nil {
				return fail("", err)
			}
			plan, err := install.Selection(repo)
			if err != nil {
				return fail(repo, err)
			}
			// The input trees are an internal review input; the status report
			// only needs the differences.
			plan.InputTrees = nil
			return emit(plan)
		},
	}
}

func newSchemaCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "schema",
		Short: "Describe the command surface and output contract",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, args []string) error {
			return emit(map[string]any{
				"commands": map[string]string{
					"add":      "source --skill name [--skill name]",
					"merge":    "name --from owner/repo:skill [--from owner/repo:skill]",
					"update":   "[names...]",
					"remove":   "names...",
					"record":   "names...",
					"status":   "",
					"find":     "query... [--owner owner]",
					"plan":     "--base <commit> [--head <commit>] [--since <commit>]",
					"ci":       "prepare|export|apply|publish <directory>",
					"schedule": "prepare|restore|publish <directory>",
				},
				"sources": "GitHub owner/repo or HTTPS repository URL",
				"options": []string{"--repo", "--dry-run", "--worktree-provider"},
				"output":  "JSON; logs on stderr; exit 2 means unresolved adaptation",
				"merge": map[string]any{
					"requires":  "non-empty .agents/skillctrl/intents/<name>.md",
					"sources":   "repeatable --from; replaces the target's sources array",
					"originals": upstream.SourceDirectory + "/<index>/ inside the target skill; immutable during review",
				},
			})
		},
	}
}

func newFindCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "find query...",
		Short: "Search the public skills index without installing anything",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			owner, _ := command.Flags().GetString("owner")
			dry, _ := command.Flags().GetBool("dry-run")
			query := strings.Join(args, " ")
			if dry {
				return emit(map[string]any{"dry_run": true, "query": query, "owner": owner})
			}
			result, err := upstream.Find(query, owner)
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
		Use:   "add source",
		Short: "Import skills from a GitHub repository",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			skills, _ := command.Flags().GetStringSlice("skill")
			return runInstall(command, "add", args[0], skills)
		},
	}
	command.Flags().StringSlice("skill", nil, "skill to import; repeatable and required")
	_ = command.MarkFlagRequired("skill")
	return command
}

func newUpdateCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "update [names...]",
		Short: "Import the current upstream original of registered skills",
		RunE: func(command *cobra.Command, args []string) error {
			return runInstall(command, "update", "", args)
		},
	}
}

func newMergeCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "merge name",
		Short: "Merge tracked upstream skills using saved intent",
		Long: "Merge named GitHub skills into one repository-local skill. Save the integration\n" +
			"policy in .agents/skillctrl/intents/<name>.md first. --from is repeatable and\n" +
			"replaces the target's sources array; update subsequently refreshes every source.",
		Args: cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			values, _ := command.Flags().GetStringArray("from")
			inputs, err := upstream.ParseInputs(values)
			if err != nil {
				return fail("", err)
			}
			return runInstall(command, "merge", "", args, inputs...)
		},
	}
	command.Flags().StringArray("from", nil, "upstream owner/repo:skill; repeatable and required")
	_ = command.MarkFlagRequired("from")
	return command
}

func newRemoveCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "remove names...",
		Short: "Remove imported skills and their upstream registration, keeping intent files",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			return runInstall(command, "remove", "", args)
		},
	}
}

// runInstall performs the shared add, merge, update, remove, and record flow.
func runInstall(command *cobra.Command, kind, source string, requested []string, inputs ...upstream.Input) error {
	repo, err := repository(command)
	if err != nil {
		return fail("", err)
	}
	if _, err := os.Stat(filepath.Join(repo, filepath.FromSlash(install.SkillsDir))); err != nil {
		return fail(repo, fmt.Errorf("--repo must point to a repository containing %s", install.SkillsDir))
	}
	values, err := install.Names(requested)
	if err != nil {
		return fail(repo, err)
	}
	identifier := ""
	if kind == "add" {
		identifier, err = upstream.Source(source)
		if err != nil {
			return fail(repo, err)
		}
	}
	dry, _ := command.Flags().GetBool("dry-run")
	if dry {
		result := map[string]any{"dry_run": true, "repo": repo, "command": kind,
			"source": source, "skills": values}
		if kind == "merge" {
			result["sources"] = inputs
		}
		return emit(result)
	}
	if kind == "record" {
		return runRecord(repo, values)
	}
	provider, _ := command.Flags().GetString("worktree-provider")
	worktree, err := install.Worktree(repo, provider)
	if err != nil {
		return fail(repo, err)
	}
	status, err := gitx.Output(worktree, "status", "--porcelain")
	if err != nil {
		return fail(worktree, err)
	}
	if strings.TrimSpace(string(status)) != "" {
		return fail(worktree, fmt.Errorf("installer requires a clean worktree; commit intentional edits first"))
	}
	artifacts, err := os.MkdirTemp("", "skillctrl-")
	if err != nil {
		return fail(worktree, err)
	}
	isolated, err := os.MkdirTemp("", "skillctrl-install-")
	if err != nil {
		return fail(worktree, err)
	}
	defer os.RemoveAll(isolated)
	// An empty skills directory in the main checkout has no Git tree entry, so
	// a freshly isolated checkout needs it recreated before its first import.
	if err := install.PrepareSkills(worktree); err != nil {
		return fail(worktree, err)
	}
	if err := install.CheckSkills(filepath.Join(worktree, filepath.FromSlash(install.SkillsDir))); err != nil {
		return fail(worktree, err)
	}
	var target, upstreamLock string
	if kind == "merge" {
		target, upstreamLock, err = upstream.Merge(worktree, values[0], inputs, isolated)
	} else {
		target, upstreamLock, err = upstream.Install(worktree, kind, values, identifier, isolated)
	}
	if err != nil {
		return fail(worktree, err)
	}
	if err := install.CheckSkills(target); err != nil {
		return fail(worktree, err)
	}
	if err := install.Import(worktree, target, upstreamLock); err != nil {
		return fail(worktree, err)
	}
	plan, err := install.Selection(worktree)
	if err != nil {
		return fail(worktree, err)
	}
	unresolved, err := install.Adapt(worktree, plan, artifacts, adapt.Prompt, toolchain.ConfigPath(), toolchain.ModelsPath())
	if err != nil {
		return fail(worktree, err)
	}
	report := any(nil)
	if plan.NeedsReview {
		report = filepath.Join(artifacts, adapt.ReportFile)
	}
	if err := emit(map[string]any{"repo": worktree, "skills": plan.Skills,
		"unresolved": unresolved, "report": report}); err != nil {
		return silenceError{err}
	}
	if len(unresolved) > 0 {
		return exitError{ExitUnresolved}
	}
	return nil
}

// runRecord accepts deliberate manual edits in place. It must not create a
// worktree and must not touch staging: the caller keeps ownership of its index
// and commits, and a pending edit is exactly what is being recorded.
func runRecord(repo string, values []string) error {
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
	unknown := []string{}
	for _, name := range values {
		if _, ok := current.Skills[name]; !ok {
			if _, ok := recorded.Skills[name]; !ok {
				unknown = append(unknown, name)
			}
		}
	}
	if len(unknown) > 0 {
		return fail(repo, fmt.Errorf("unknown skills: %s", strings.Join(unknown, ", ")))
	}
	if _, err := lock.Record(repo, current, recorded, values); err != nil {
		return fail(repo, err)
	}
	return emit(map[string]any{"repo": repo, "recorded": values})
}

func newRecordCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "record names...",
		Short: "Accept deliberate manual skill edits without creating a worktree",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			return runInstall(command, "record", "", args)
		},
	}
}

func newPlanCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "plan",
		Short: "Compute the skill selection for a base and head commit",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, args []string) error {
			base, _ := command.Flags().GetString("base")
			head, _ := command.Flags().GetString("head")
			since, _ := command.Flags().GetString("since")
			if base == "" {
				return fail("", fmt.Errorf("plan requires --base"))
			}
			repo, err := repository(command)
			if err != nil {
				return fail("", err)
			}
			if head == "" {
				head = "HEAD"
			}
			plan, err := lock.Compare(repo, base, head, since)
			if err != nil {
				return fail(repo, err)
			}
			return emit(plan)
		},
	}
	command.Flags().String("base", "", "base commit to compare from (required)")
	command.Flags().String("head", "HEAD", "commit to compare to")
	command.Flags().String("since", "", "commit to read commit messages from")
	return command
}

func newPromptCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "prompt",
		Short: "Print the intent review contract embedded in this binary",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, args []string) error {
			fmt.Fprint(os.Stdout, adapt.Prompt)
			return nil
		},
	}
}
