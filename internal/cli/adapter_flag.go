package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/wwwyo/skillctrl/internal/upstream"
)

type adapterValue string

func (value *adapterValue) String() string { return string(*value) }

func (value *adapterValue) Type() string { return "skills|gh|git" }

func (value *adapterValue) Set(name string) error {
	if err := upstream.ValidateAdapter(name); err != nil {
		return err
	}
	*value = adapterValue(name)
	return nil
}

func registerAdapterFlag(root *cobra.Command) {
	value := adapterValue("skills")
	root.PersistentFlags().Var(&value, "adapter", "skills backend; also SKILLCTRL_ADAPTER")
	root.PersistentPreRunE = func(command *cobra.Command, args []string) error {
		if root.PersistentFlags().Changed("adapter") {
			return nil
		}
		name := os.Getenv("SKILLCTRL_ADAPTER")
		if name == "" {
			name = "skills"
		}
		if err := value.Set(name); err != nil {
			return fmt.Errorf("SKILLCTRL_ADAPTER: %w", err)
		}
		return nil
	}
	_ = root.RegisterFlagCompletionFunc("adapter", func(command *cobra.Command, args []string, prefix string) ([]string, cobra.ShellCompDirective) {
		return strings.Split(value.Type(), "|"), cobra.ShellCompDirectiveNoFileComp
	})
}
