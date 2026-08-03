package main

import "github.com/spf13/cobra"

func registerCommands(root *cobra.Command) {
	if root == nil {
		return
	}
	root.AddCommand(newServerCommand())
	root.AddCommand(newWorkerCommand())

	root.AddCommand(newLoginCommand())
	root.AddCommand(newLogoutCommand())

	root.AddCommand(newProjectCommand())
	root.AddCommand(newTaskCommand())

	root.AddCommand(newSessionCommand())
	root.AddCommand(newChatCommand())

	addCLIConfigFlag(root)
}

func addCLIConfigFlag(cmd *cobra.Command) {
	for _, sub := range cmd.Commands() {
		if sub.Name() == "server" {
			continue
		}
		sub.Flags().Var(&configPathValue{&cliConfigPath}, "config", "CLI config file path")
		addCLIConfigFlag(sub)
	}
}
