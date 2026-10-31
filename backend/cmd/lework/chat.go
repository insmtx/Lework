package main

import (
	"github.com/spf13/cobra"
	"github.com/ygpkg/yg-go/lifecycle"
	"github.com/ygpkg/yg-go/logs"

	"github.com/insmtx/Lework/backend/internal/cli"
)

func newChatCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "chat [message]",
		Short: "Start an interactive chat session with Lework",
		Long: `Start an interactive chat session with a running Lework server.
Streams assistant responses in real-time via SSE.

If a message is provided as a positional argument, it will be sent as the
first message. After that, you can continue typing messages interactively.
Type /exit or /quit to end the session.

The server must be running.`,
		Run: func(cmd *cobra.Command, args []string) {
			var initialMessage string
			if len(args) > 0 {
				initialMessage = args[0]
			}

			go func() {
				if err := cli.Chat(lifecycle.Std().Context(), cliServerAddr(), cliAuthToken(), initialMessage); err != nil {
					logs.Errorf("chat: %v", err)
				}
				lifecycle.Std().Exit()
			}()
			lifecycle.Std().WaitExit()
		},
	}
	return cmd
}
