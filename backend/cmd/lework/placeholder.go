package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newPlaceholderCommand(use string, short string, extra string) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		RunE: func(cmd *cobra.Command, args []string) error {
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "lework %s is not implemented yet\n", use); err != nil {
				return err
			}
			if extra != "" {
				_, err := fmt.Fprintln(cmd.OutOrStdout(), extra)
				return err
			}
			return nil
		},
	}
}
