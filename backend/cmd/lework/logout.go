package main

import "github.com/spf13/cobra"

func newLogoutCommand() *cobra.Command {
	return newPlaceholderCommand("logout", "Log out of Lework", "")
}
