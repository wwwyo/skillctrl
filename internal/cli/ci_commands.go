package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/wwwyo/skillctrl/internal/adapt"
	"github.com/wwwyo/skillctrl/internal/lock"
	"github.com/wwwyo/skillctrl/internal/scheduled"
	"github.com/wwwyo/skillctrl/internal/toolchain"
)

// planFromEnv reads the immutable selection plan handed over through the job
// environment. CI writes it as JSON so the reviewing job and the validating job
// cannot disagree about which skills were selected.
func planFromEnv() (lock.Plan, error) {
	value := os.Getenv("SKILL_PLAN")
	if value == "" {
		return lock.Plan{}, fmt.Errorf("SKILL_PLAN is not set")
	}
	var plan lock.Plan
	if err := json.Unmarshal([]byte(value), &plan); err != nil {
		return lock.Plan{}, fmt.Errorf("SKILL_PLAN is not valid JSON")
	}
	return plan, nil
}

func newCICommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "ci",
		Short: "Select, verify, and publish skill changes in CI",
		Long: "ci exposes selection and the phases of intent verification:\n" +
			"  plan     select the skills from fixed commits\n" +
			"  prompt   print the instructions passed to the external reviewer\n" +
			"  prepare  bind the trusted toolchain to the selected head\n" +
			"  export   validate edits made inside the reviewer sandbox and export artifacts\n" +
			"  apply    validate the artifact and record accepted hashes\n" +
			"  publish  commit and report on the triggering pull request\n" +
			"Only apply and publish need write access; export runs where the reviewer ran.",
	}
	command.AddCommand(
		newPlanCommand(),
		newPromptCommand(),
		newCIStep("prepare", func(dir, directory string, plan lock.Plan) error {
			return adapt.Prepare(dir, plan, os.Getenv("CHECKER_SOURCE"), directory)
		}),
		newCIStep("export", func(dir, directory string, plan lock.Plan) error {
			return adapt.Export(dir, plan, directory, os.Getenv("OPENCODE_API_KEY"))
		}),
		newCIStep("apply", func(dir, directory string, plan lock.Plan) error {
			return adapt.Apply(dir, plan, directory)
		}),
		newCIStep("publish", func(dir, directory string, plan lock.Plan) error {
			_, err := adapt.Publish(adapt.PublishOptions{
				Dir: dir, Plan: plan, Directory: directory,
				Repo: os.Getenv("GITHUB_REPOSITORY"), Number: os.Getenv("PR_NUMBER"),
				GH: adapt.CLI{},
			})
			return err
		}),
		newCIConfigureCommand(),
	)
	return command
}

func newCIStep(name string, run func(dir, directory string, plan lock.Plan) error) *cobra.Command {
	step := &cobra.Command{
		Use:   name + " <directory>",
		Short: "CI phase: " + name,
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			if err := rejectDryRun(command); err != nil {
				return fail("", err)
			}
			dir, err := repository()
			if err != nil {
				return fail("", err)
			}
			directory, err := filepath.Abs(args[0])
			if err != nil {
				return fail(dir, err)
			}
			plan, err := planFromEnv()
			if err != nil {
				return fail(dir, err)
			}
			if err := run(dir, directory, plan); err != nil {
				return fail(dir, err)
			}
			return emit(map[string]any{"ok": true, "command": name})
		},
	}
	return step
}

// newCIConfigureCommand reports the trusted pins the workflow needs before it
// can start the reviewer. The Node pin must be exact, so it is validated here
// rather than resolved at run time.
func newCIConfigureCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "configure",
		Short: "Read the trusted tool pins from the selected source commit",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, args []string) error {
			if err := rejectDryRun(command); err != nil {
				return fail("", err)
			}
			dir, err := repository()
			if err != nil {
				return fail("", err)
			}
			configuration, err := toolchain.Trusted(dir, os.Getenv("CHECKER_SOURCE"), toolchain.ConfigPath())
			if err != nil {
				return fail(dir, err)
			}
			node, err := toolchain.NodeVersion(configuration)
			if err != nil {
				return fail(dir, err)
			}
			rendered, err := toolchain.Render(configuration)
			if err != nil {
				return fail(dir, err)
			}
			return emit(map[string]any{"node": node, "mise": string(rendered)})
		},
	}
}

func newScheduleCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "schedule",
		Short: "Run the scheduled upstream update phases",
		Long: "schedule exposes the three phases of a periodic update:\n" +
			"  prepare  fetch all originals and emit an immutable input commit and bundle\n" +
			"  restore  re-derive and verify the imported input in a separate job\n" +
			"  publish  test the repaired update and open one draft pull request",
	}
	command.AddCommand(
		newScheduleStep("prepare", func(dir, directory string, _ lock.Plan) error {
			adapter, err := selectedAdapter(command)
			if err != nil {
				return err
			}
			result, err := scheduled.PrepareWithAdapter(dir, directory, os.Getenv("GITHUB_REPOSITORY"), adapt.CLI{}, adapter)
			if err != nil {
				return err
			}
			return emit(result)
		}),
		newScheduleStep("restore", func(dir, directory string, _ lock.Plan) error {
			plan, err := scheduled.Restore(dir, directory, os.Getenv("CHECKER_SOURCE"))
			if err != nil {
				return err
			}
			return emit(plan)
		}),
		newScheduleStep("publish", func(dir, directory string, plan lock.Plan) error {
			result, err := scheduled.Publish(dir, directory, plan, map[string]string{
				"GITHUB_REPOSITORY":  os.Getenv("GITHUB_REPOSITORY"),
				"DEFAULT_BRANCH":     os.Getenv("DEFAULT_BRANCH"),
				"GITHUB_RUN_ID":      os.Getenv("GITHUB_RUN_ID"),
				"GITHUB_RUN_ATTEMPT": os.Getenv("GITHUB_RUN_ATTEMPT"),
				"OPENCODE_API_KEY":   os.Getenv("OPENCODE_API_KEY"),
			}, adapt.CLI{})
			if err != nil {
				return err
			}
			return emit(result)
		}),
	)
	return command
}

func newScheduleStep(name string, run func(dir, directory string, plan lock.Plan) error) *cobra.Command {
	return &cobra.Command{
		Use:   name + " <directory>",
		Short: "Scheduled phase: " + name,
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			// Refused before anything is created or read, so a rejected run leaves
			// no artifact directory behind.
			if err := rejectDryRun(command); err != nil {
				return fail("", err)
			}
			dir, err := repository()
			if err != nil {
				return fail("", err)
			}
			directory, err := filepath.Abs(args[0])
			if err != nil {
				return fail(dir, err)
			}
			if err := os.MkdirAll(directory, 0o755); err != nil {
				return fail(dir, err)
			}
			// Preparation and restoration derive their plans from immutable inputs;
			// only publication consumes the verified plan passed by the workflow.
			var plan lock.Plan
			if name == "publish" {
				plan, err = planFromEnv()
				if err != nil {
					return fail(dir, err)
				}
			}
			if err := run(dir, directory, plan); err != nil {
				return fail(dir, err)
			}
			return nil
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
			repo, err := repository()
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
			_, err := fmt.Fprint(os.Stdout, adapt.Prompt)
			return err
		},
	}
}
