package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/a-novel-kit/stack/cli/internal/client/rpc"
	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
)

func newServiceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   cmdNameService,
		Short: "Operate on a service's infrastructure",
	}
	cmd.AddCommand(newServiceStatusCmd())
	cmd.AddCommand(newServiceInfraCmd())
	return cmd
}

func newServiceStatusCmd() *cobra.Command {
	var ss stackScope
	cmd := &cobra.Command{
		Use:        "status <service>",
		Short:      "Show one service's infrastructure and targets",
		Deprecated: "use 'a-novel run ps --service=<service>' instead",
		Args:       cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return printService(cmd, ss.stack, args[0])
		},
	}
	ss.bind(cmd)
	return cmd
}

// printService prints one service as `run ps` does.
func printService(cmd *cobra.Command, stack, service string) error {
	resp, err := rpc.New("").DescribeService(cmd.Context(), stack, service)
	if err != nil {
		return err
	}
	renderPs(cmd.OutOrStdout(), []*anovelv1.Service{resp.GetService()}, false)
	return nil
}

func newServiceInfraCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   kindInfra,
		Short: "Manage a service's infrastructure (containers without Go counterparts)",
		Long: `Start or stop a service's infrastructure: the compose services with no cmd/
target, such as its database. 'infra start' also runs every one-shot target of
the service, such as migrations, so the database is ready to query without
starting a server.`,
	}
	cmd.AddCommand(newServiceInfraStartCmd())
	cmd.AddCommand(newServiceInfraKillCmd())
	return cmd
}

func newServiceInfraStartCmd() *cobra.Command {
	var ss stackScope
	var oneShotsMode string
	cmd := &cobra.Command{
		Use:   "start <service>",
		Short: "Bring up a service's infrastructure and run its one-shots",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return startInfra(cmd, ss.stack, args[0], oneShotsMode)
		},
	}
	ss.bind(cmd)
	cmd.Flags().StringVar(&oneShotsMode, "one-shots", modeGoExec,
		"mode for one-shot targets auto-run with infra (go-exec | container)")
	return cmd
}

// startInfra brings up a service's infra and runs its one-shots in the mode
// the --one-shots token names.
func startInfra(cmd *cobra.Command, stack, service, oneShotsMode string) error {
	mode := parseMode(oneShotsMode, anovelv1.Mode_MODE_GO_EXEC)
	resp, err := rpc.New("").StartInfra(cmd.Context(), stack, service, mode)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "infra-up for %s (one-shots ran in %s mode)\n",
		resp.GetService().GetName(), modeLabel(mode))
	return nil
}

func newServiceInfraKillCmd() *cobra.Command {
	var ss stackScope
	var force bool
	cmd := &cobra.Command{
		Use:   "kill <service>",
		Short: "Tear down a service's infrastructure",
		Long: `Refuses while a target of the service runs. Stop it first with
'a-novel run kill', or pass --force to stop the targets with the infra.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return killInfra(cmd, ss.stack, args[0], force)
		},
	}
	ss.bind(cmd)
	cmd.Flags().BoolVarP(&force, "force", "f", false, "cascade-kill all running targets first")
	return cmd
}

// killInfra tears down a service's infra, stopping its targets first with
// force.
func killInfra(cmd *cobra.Command, stack, service string, force bool) error {
	resp, err := rpc.New("").KillInfra(cmd.Context(), stack, service, force)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "infra-down for %s\n", resp.GetService().GetName())
	return nil
}
