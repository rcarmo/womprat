package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// logFilePath returns the path to the runtime log file, placed in the same
// folder as the running executable so it sits alongside the app at runtime. On
// Windows the app is a GUI binary with no console, so logs must go to a file.
func logFilePath() string {
	dir := runtimeDir()
	return filepath.Join(dir, "womprat-log.txt")
}

// runtimeDir is the directory of the running executable, falling back to the
// current working directory and then the config dir.
func runtimeDir() string {
	if exe, err := os.Executable(); err == nil {
		if dir := filepath.Dir(exe); dir != "" {
			return dir
		}
	}
	if wd, err := os.Getwd(); err == nil && wd != "" {
		return wd
	}
	return configDir()
}

// setupLogging routes the standard logger to a log file (and stderr when one is
// attached) so runtime diagnostics are visible on Windows GUI builds.
func setupLogging() {
	path := logFilePath()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return
	}
	log.SetFlags(log.LstdFlags | log.Lmsgprefix)
	log.SetPrefix("")
	if stderrUsable() {
		log.SetOutput(io.MultiWriter(f, os.Stderr))
	} else {
		log.SetOutput(f)
	}
	log.Printf("=== womprat %s (%s) starting; log: %s ===", version, commit, path)
}

func stderrUsable() bool {
	info, err := os.Stderr.Stat()
	if err != nil {
		return false
	}
	// On Windows GUI builds stderr is typically not a valid console handle.
	return info.Mode()&os.ModeCharDevice != 0 || info.Size() >= 0 && info.Mode().IsRegular()
}

func (a *App) handleLogs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	data, err := os.ReadFile(logFilePath())
	if err != nil {
		fmt.Fprintf(w, "no log file yet: %v\n", err)
		return
	}
	const maxBytes = 64 * 1024
	if len(data) > maxBytes {
		data = data[len(data)-maxBytes:]
	}
	w.Write(data)
}

// logStartupBanner records key runtime locations once logging is set up.
func logStartupBanner(app *App) {
	log.Printf("config dir: %s", configDir())
	log.Printf("webview data: %s", webviewDataPath())
	log.Printf("socks: %s", socksAddr)
	log.Printf("local API: http://127.0.0.1:%d", app.serverPort)
	log.Printf("started at %s", time.Now().Format(time.RFC3339))
}
