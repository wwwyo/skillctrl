package cli

import (
	"os"
	"path/filepath"
	"slices"

	"github.com/spf13/cobra"
	"github.com/wwwyo/skillctrl/internal/install"
	"github.com/wwwyo/skillctrl/internal/upstream"
)

func adapterName(command *cobra.Command) string {
	return command.Root().PersistentFlags().Lookup("adapter").Value.String()
}

func selectedAdapter(command *cobra.Command) (upstream.Adapter, error) {
	return upstream.NewAdapter(adapterName(command))
}

func newListCommand() *cobra.Command {
	return &cobra.Command{
		Use: "list", Aliases: []string{"ls"}, Short: "List installed project skills", Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, args []string) error {
			repo, err := repository(command)
			if err != nil {
				return fail(repo, err)
			}
			directory := filepath.Join(repo, install.SkillsDir)
			entries, err := os.ReadDir(directory)
			if err != nil && !os.IsNotExist(err) {
				return fail(repo, err)
			}
			names := []string{}
			for _, entry := range entries {
				if !entry.IsDir() || !upstream.Name(entry.Name()) {
					continue
				}
				if info, err := os.Lstat(filepath.Join(directory, entry.Name(), "SKILL.md")); err == nil && info.Mode().IsRegular() {
					names = append(names, entry.Name())
				}
			}
			return emit(map[string]any{"repo": repo, "skills": names})
		},
	}
}

func newCheckCommand() *cobra.Command {
	return &cobra.Command{
		Use: "check [names...]", Short: "Check upstream updates and local accepted hashes without changing files", Args: cobra.ArbitraryArgs,
		RunE: func(command *cobra.Command, args []string) error {
			repo, err := repository(command)
			if err != nil {
				return fail(repo, err)
			}
			names, err := install.Names(args)
			if err != nil {
				return fail(repo, err)
			}
			adapter, err := selectedAdapter(command)
			if err != nil {
				return fail(repo, err)
			}
			dry, _ := command.Flags().GetBool("dry-run")
			if dry {
				return emit(map[string]any{"repo": repo, "command": "check", "skills": names, "dry_run": true})
			}
			local, err := install.Selection(repo)
			if err != nil {
				return fail(repo, err)
			}
			if len(names) > 0 {
				outside := func(name string) bool { return !slices.Contains(names, name) }
				local.Skills = slices.DeleteFunc(local.Skills, outside)
				local.ReviewSkills = slices.DeleteFunc(local.ReviewSkills, outside)
				local.NeedsReview = len(local.ReviewSkills) > 0
				local.LockChanged = len(local.Skills) > 0
			}
			directory, err := os.MkdirTemp("", "skillctrl-check-")
			if err != nil {
				return fail(repo, err)
			}
			defer os.RemoveAll(directory)
			updates, err := upstream.Check(repo, names, directory, adapter)
			if err != nil {
				return fail(repo, err)
			}
			slices.Sort(updates)
			return emit(map[string]any{"repo": repo, "updates": updates, "local": map[string]any{
				"skills": local.Skills, "review_skills": local.ReviewSkills,
				"needs_review": local.NeedsReview, "lock_changed": local.LockChanged,
			}})
		},
	}
}
