package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/a-novel-kit/stack/cli/internal/rpc"
	"github.com/a-novel-kit/stack/cli/internal/shared/enumword"
	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
)

func newServiceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   cmdNameService,
		Short: "Operate on a service's infrastructure",
	}
	cmd.AddCommand(newServiceInfraCmd())
	return cmd
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
	req := &anovelv1.StartInfraRequest{}
	oneShots := enumword.Word(anovelv1.Mode_MODE_GO_EXEC)
	cmd := &cobra.Command{
		Use:   "start <service>",
		Short: "Bring up a service's infrastructure and run its one-shots",
		Long: `Start the service's infra containers and wait until they are healthy, then run
each one-shot target of the service, such as migrations, to success. The
one-shots run from source, or from their images with --one-shots=container.
Infra already up is left as it is.`,
		Example: `  a-novel run service infra start service-json-keys
  a-novel run service infra start service-json-keys --one-shots=container`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			req.Service, req.OneShotsMode = args[0], enumword.Parse[anovelv1.Mode](oneShots)
			resp, err := rpc.Call(cmd.Context(), rpc.New().StartInfra, req)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "infra-up for %s (one-shots ran in %s mode)\n",
				resp.GetService().GetName(), oneShots)
			return nil
		},
	}
	addStackFlag(cmd, &req.Stack)
	addEnumFlag(cmd, &enumFlag{value: &oneShots, allowed: modeWords}, "one-shots", "",
		"mode for one-shot targets auto-run with infra (go-exec | container)")
	return cmd
}

func newServiceInfraKillCmd() *cobra.Command {
	req := &anovelv1.KillInfraRequest{}
	cmd := &cobra.Command{
		Use:   "kill <service>",
		Short: "Tear down a service's infrastructure",
		Long: `Refuses while a target of the service runs. Stop it first with
'a-novel run kill', or pass --force to stop the targets with the infra.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			req.Service = args[0]
			resp, err := rpc.Call(cmd.Context(), rpc.New().KillInfra, req)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "infra-down for %s\n", resp.GetService().GetName())
			return nil
		},
	}
	addStackFlag(cmd, &req.Stack)
	cmd.Flags().BoolVarP(&req.Force, "force", "f", false, "cascade-kill all running targets first")
	return cmd
}
