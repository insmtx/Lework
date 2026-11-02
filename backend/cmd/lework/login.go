package main

import "github.com/spf13/cobra"

func newLoginCommand() *cobra.Command {
	return newPlaceholderCommand("login", "Log in to Lework", "")
}
