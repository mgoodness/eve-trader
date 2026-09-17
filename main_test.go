package main

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"
)

// setupLogging is the process-wide logging seam: the app's structured lines
// must land as JSON on the writer (os.Stdout in production) so Docker's
// default json-file log driver captures them for `docker logs`.
func TestSetupLoggingWritesJSONToWriter(t *testing.T) {
	previous := slog.Default()
	t.Cleanup(func() { slog.SetDefault(previous) })

	var buf bytes.Buffer
	setupLogging(&buf)
	slog.Info("eve-trader listening", "addr", ":8080")

	var line map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &line); err != nil {
		t.Fatalf("log output is not one JSON object: %v\nraw: %q", err, buf.String())
	}
	if got := line["msg"]; got != "eve-trader listening" {
		t.Errorf("msg = %v, want %q", got, "eve-trader listening")
	}
	if got := line["addr"]; got != ":8080" {
		t.Errorf("addr = %v, want %q", got, ":8080")
	}
	if got := line["level"]; got != "INFO" {
		t.Errorf("level = %v, want INFO", got)
	}
}
