package cli

import "github.com/spf13/cobra"

func (a *app) newBuildCommand() *cobra.Command {
	command := &cobra.Command{Use: "build", Short: "Inspect and manage builds"}
	command.AddCommand(a.newBuildStatusCommand())
	command.AddCommand(a.newBuildWatchCommand())
	command.AddCommand(a.newBuildLogsCommand())
	command.AddCommand(a.newBuildArtifactTriggersCommand())
	command.AddCommand(a.newBuildArtifactsCommand())
	command.AddCommand(a.newBuildRetryCommand())
	return command
}
