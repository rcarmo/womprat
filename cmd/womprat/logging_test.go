package main

import (
	"bytes"
	"net/http"
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

func TestHandleLogsRejectsUnsupportedMethods(t *testing.T) {
	app := newTestApp(t)
	rr := performJSON(app.handleLogs, http.MethodPost, "/api/logs", nil)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST logs = %d", rr.Code)
	}
}
