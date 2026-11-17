package cli

import (
	"github.com/spf13/cobra"

	"github.com/a-novel-kit/stack/cli/internal/dashboard"
)

func newUICmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ui",
		Short: "Open the terminal UI",
		Long: `Open the full-screen view of every service: the service list, each service's
targets and infra as tabs, and the selected tab's live log. Esc opens the
command palette, ? lists every key, and q quits without stopping anything.
The UI talks to the same daemon as the CLI, so each sees what the other
starts.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return dashboard.Run()
		},
	}
}
