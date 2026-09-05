package main

import (
	"bytes"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTailBytesAtLineBoundary(t *testing.T) {
	data := []byte("line1\nline2\nline3\n")
	if got := string(tailBytesAtLineBoundary(data, 100)); got != string(data) {
		t.Fatalf("short tail = %q", got)
	}
	got := string(tailBytesAtLineBoundary(data, 8))
	if got != "line3\n" {
		t.Fatalf("line boundary tail = %q", got)
	}
	if got := tailBytesAtLineBoundary(data, 0); !bytes.Equal(got, data) {
		t.Fatalf("zero max should leave data unchanged")
	}
}

func TestWindowsConsoleFollowsDebugLogging(t *testing.T) {
	mainSource := readFileForRegression(t, "main.go")
	loggingSource := readFileForRegression(t, "logging.go")
	windowsSource := readFileForRegression(t, "console_windows.go")

	if !strings.Contains(mainSource, "setConsoleVisible(cfg.DebugLog)") {
		t.Fatal("startup must apply persisted debug logging to console visibility")
	}
	if !strings.Contains(loggingSource, "setConsoleVisible(body.Enabled)") {
		t.Fatal("runtime debug setting must update console visibility")
	}
	for _, want := range []string{"GetConsoleWindow", "ShowWindow", "consoleHide", "consoleShow"} {
		if !strings.Contains(windowsSource, want) {
			t.Fatalf("Windows console visibility implementation missing %q", want)
		}
	}
}

func TestDebugLogDoesNotChangeMemoryOnPersistFailure(t *testing.T) {
	app := newTestApp(t)
	app.config.DebugLog = false
	blocked := filepath.Join(t.TempDir(), "blocked")
	if err := os.WriteFile(blocked, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", blocked)
	rr := performJSON(app.handleDebugLog, http.MethodPost, "/api/settings/debug-log", map[string]bool{"enabled": true})
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("debug log persist failure = %d %s", rr.Code, rr.Body.String())
	}
	if app.config.DebugLog {
		t.Fatal("debug log changed in memory despite persist failure")
	}
}

func TestHandleLogsRejectsUnsupportedMethods(t *testing.T) {
	app := newTestApp(t)
	rr := performJSON(app.handleLogs, http.MethodPost, "/api/logs", nil)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST logs = %d", rr.Code)
	}
}

func TestDisablingLoggingClosesPreviousFile(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "log")
	if err != nil {
		t.Fatal(err)
	}
	loggingMu.Lock()
	previous := activeLogFile
	activeLogFile = f
	loggingMu.Unlock()
	setupLogging(false)
	if _, err := f.WriteString("must be closed"); err == nil {
		t.Fatal("log file remains open after disabling")
	}
	loggingMu.Lock()
	activeLogFile = previous
	loggingMu.Unlock()
}
