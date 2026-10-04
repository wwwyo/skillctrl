package cli

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"github.com/wwwyo/skillctrl/internal/adapt"
	"github.com/wwwyo/skillctrl/internal/install"
	"github.com/wwwyo/skillctrl/internal/lock"
	"github.com/wwwyo/skillctrl/internal/toolchain"
	"github.com/wwwyo/skillctrl/internal/upstream"
)

func newIntentCommand() *cobra.Command {
	command := &cobra.Command{Use: "intent", Short: "Set, remove, or explicitly apply saved intent"}
	set := &cobra.Command{
		Use: "set name", Short: "Save intent from a Markdown file without reviewing or accepting a skill", Args: cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			if err := rejectIntentDryRun(command); err != nil {
				return err
			}
			repo, err := repository(command)
			if err != nil {
				return fail(repo, err)
			}
			path, err := intentPath(repo, args[0])
			if err != nil {
				return fail(repo, err)
			}
			source, _ := command.Flags().GetString("file")
			data, err := os.ReadFile(source)
			if err != nil {
				return fail(repo, err)
			}
			if strings.TrimSpace(string(data)) == "" {
				return fail(repo, fmt.Errorf("intent must not be empty"))
			}
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return fail(repo, err)
			}
			file, err := os.CreateTemp(filepath.Dir(path), ".intent-*")
			if err != nil {
				return fail(repo, err)
			}
			defer os.Remove(file.Name())
			if _, err := file.Write(data); err != nil {
				file.Close()
				return fail(repo, err)
			}
			if err := file.Chmod(0o644); err != nil {
				file.Close()
				return fail(repo, err)
			}
			if err := file.Close(); err != nil {
				return fail(repo, err)
			}
			if err := os.Rename(file.Name(), path); err != nil {
				return fail(repo, err)
			}
			return emit(map[string]any{"repo": repo, "intent": args[0]})
		},
	}
	set.Flags().String("file", "", "Markdown file containing the intent (required)")
	_ = set.MarkFlagRequired("file")
	remove := &cobra.Command{
		Use: "remove name", Aliases: []string{"rm"}, Short: "Remove saved intent and its accepted hash, keeping the skill and upstream", Args: cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			if err := rejectIntentDryRun(command); err != nil {
				return err
			}
			repo, err := repository(command)
			if err != nil {
				return fail(repo, err)
			}
			path, err := intentPath(repo, args[0])
			if err != nil {
				return fail(repo, err)
			}
			if _, err := lock.Local(repo); err != nil {
				return fail(repo, err)
			}
			if _, err := upstream.Load(repo); err != nil {
				return fail(repo, err)
			}
			if _, err := install.Intents(repo); err != nil {
				return fail(repo, err)
			}
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return fail(repo, err)
			}
			if err := pruneAcceptance(repo); err != nil {
				return fail(repo, err)
			}
			return emit(map[string]any{"repo": repo, "removed_intent": args[0]})
		},
	}
	apply := &cobra.Command{
		Use: "apply names...", Short: "Review only the named skills against saved intent and record accepted hashes", Args: cobra.MinimumNArgs(1),
		RunE: func(command *cobra.Command, args []string) error { return applyIntent(command, args) },
	}
	command.AddCommand(set, remove, apply)
	return command
}

func rejectIntentDryRun(command *cobra.Command) error {
	if dry, _ := command.Flags().GetBool("dry-run"); dry {
		return fmt.Errorf("--dry-run is not supported by intent operations")
	}
	return nil
}

func intentPath(repo, name string) (string, error) {
	if !upstream.Name(name) || name == "README" {
		return "", fmt.Errorf("intent name must be a plain skill directory name other than README")
	}
	for _, relative := range []string{".agents", ".agents/skillctrl", strings.TrimSuffix(lock.Intents, "/")} {
		info, err := os.Lstat(filepath.Join(repo, relative))
		if os.IsNotExist(err) {
			break
		}
		if err != nil {
			return "", err
		}
		if !info.IsDir() {
			return "", fmt.Errorf("intent path must be a real directory: %s", relative)
		}
	}
	path := filepath.Join(repo, lock.Intents, name+".md")
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() {
			return "", fmt.Errorf("intent must be a regular file: %s", name)
		}
	} else if !os.IsNotExist(err) {
		return "", err
	}
	return path, nil
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

func applyIntent(command *cobra.Command, requested []string) error {
	if err := rejectIntentDryRun(command); err != nil {
		return err
	}
	repo, err := repository(command)
	if err != nil {
		return fail(repo, err)
	}
	names, err := install.Names(requested)
	if err != nil {
		return fail(repo, err)
	}
	intents, err := install.Intents(repo)
	if err != nil {
		return fail(repo, err)
	}
	registered, err := upstream.Load(repo)
	if err != nil {
		return fail(repo, err)
	}
	eligible := lock.IntentRegistered(registered.ManagedSkills(), intents)
	for _, name := range names {
		if _, ok := eligible[name]; !ok {
			return fail(repo, fmt.Errorf("skill requires a registered upstream and saved intent: %s", name))
		}
		if _, err := intentPath(repo, name); err != nil {
			return fail(repo, err)
		}
	}
	provider, _ := command.Flags().GetString("worktree-provider")
	working, err := install.Worktree(repo, provider)
	if err != nil {
		return fail(repo, err)
	}
	plan, err := install.Selection(working)
	if err != nil {
		return fail(working, err)
	}
	plan.Skills, plan.ReviewSkills, plan.NeedsReview = names, names, true
	maps.DeleteFunc(plan.InputTrees, func(name, _ string) bool {
		return !slices.Contains(names, name)
	})
	artifacts, err := os.MkdirTemp("", "skillctrl-intent-")
	if err != nil {
		return fail(working, err)
	}
	unresolved, err := install.Adapt(working, plan, artifacts, adapt.Prompt, toolchain.ConfigPath(), toolchain.ModelsPath())
	if err != nil {
		return fail(working, err)
	}
	if err := emit(map[string]any{"repo": working, "skills": names, "unresolved": unresolved, "report": filepath.Join(artifacts, adapt.ReportFile)}); err != nil {
		return silenceError{err}
	}
	if len(unresolved) > 0 {
		return exitError{ExitUnresolved}
	}
	return nil
}
