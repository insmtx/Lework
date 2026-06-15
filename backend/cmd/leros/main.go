package main

import (
	"bufio"
	"encoding/json"
	"os"
)

func main() {
	redirectLogsToStderr()
	if err := newRootCommand().Execute(); err != nil {
		os.Exit(1)
	}
}

// redirectLogsToStderr ensures that structured zap log lines emitted by the
// yg-go/logs package are written to stderr instead of stdout. This keeps
// machine-readable --json output on stdout clean for pipes and subprocess
// callers.
//
// The redirect only activates when a --json flag is present in the command
// arguments (e.g. session messages, session ls). Server, worker, chat and
// other interactive commands are left untouched.
func redirectLogsToStderr() {
	if !hasJSONFlag() {
		return
	}

	realStdout := os.Stdout
	realStderr := os.Stderr

	r, w, err := os.Pipe()
	if err != nil {
		return
	}

	os.Stdout = w

	go func() {
		defer r.Close()
		defer w.Close()

		scanner := bufio.NewScanner(r)
		// Large JSON payloads may exceed the default 64 KiB buffer.
		scanner.Buffer(make([]byte, 0, 256*1024), 16*1024*1024)

		for scanner.Scan() {
			line := scanner.Bytes()
			if isZapLogLine(line) {
				realStderr.Write(line)
				realStderr.Write([]byte{'\n'})
			} else {
				realStdout.Write(line)
				realStdout.Write([]byte{'\n'})
			}
		}
		_ = scanner.Err()
	}()
}

// hasJSONFlag reports whether the --json flag is present in the command
// line arguments.
func hasJSONFlag() bool {
	for _, arg := range os.Args {
		if arg == "--json" {
			return true
		}
	}
	return false
}

// isZapLogLine reports whether b is a structured zap JSON log entry by
// probing for the "lvl" key that the zapcore.JSONEncoder always emits.
func isZapLogLine(b []byte) bool {
	var probe struct {
		Level string `json:"lvl"`
	}
	return json.Unmarshal(b, &probe) == nil && probe.Level != ""
}
