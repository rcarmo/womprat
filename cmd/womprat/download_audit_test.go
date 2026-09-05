package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestDownloadPreservesFileCreatedAfterPathSelection(t *testing.T) {
	app := newTestApp(t)
	old := allowDirectDial
	allowDirectDial = true
	t.Cleanup(func() { allowDirectDial = old })
	path := uniqueDownloadPath(t.TempDir(), "file.txt")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate another process occupying the selected name during the request.
		if err := os.WriteFile(path, []byte("existing"), 0600); err != nil {
			t.Error(err)
		}
		_, _ = w.Write([]byte("replacement"))
	}))
	defer server.Close()
	st := &downloadState{Status: "downloading"}
	app.downloadToFile(server.URL, path, st)
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "existing" || st.Status != "error" {
		t.Fatalf("data=%q err=%v status=%s", data, err, st.Status)
	}
}

func TestDownloadCompletesAndRemovesTruncatedResponse(t *testing.T) {
	app := newTestApp(t)
	old := allowDirectDial
	allowDirectDial = true
	t.Cleanup(func() { allowDirectDial = old })
	for _, truncated := range []bool{false, true} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if truncated {
				w.Header().Set("Content-Length", "100")
			}
			_, _ = w.Write([]byte("payload"))
		}))
		path := filepath.Join(t.TempDir(), "file.txt")
		st := &downloadState{Status: "downloading"}
		app.downloadToFile(server.URL, path, st)
		server.Close()
		data, err := os.ReadFile(path)
		if truncated {
			if !os.IsNotExist(err) || st.Status != "error" {
				t.Fatalf("partial file retained: err=%v status=%s", err, st.Status)
			}
		} else if err != nil || string(data) != "payload" || st.Status != "complete" {
			t.Fatalf("data=%q err=%v status=%s", data, err, st.Status)
		}
	}
}
