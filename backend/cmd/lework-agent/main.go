// Command lework-agent is the Lework node agent. It registers with the control
// plane over an outbound WebSocket, supervises a local OpenCode service, and
// proxies requests, events, and permissions between the two.
package main

import (
	"os"
)

func main() {
	if err := newRootCommand().Execute(); err != nil {
		// cobra already prints the error; the exit status is the only thing the
		// process entrypoint owns. Library code never terminates the process.
		os.Exit(1)
	}
}
