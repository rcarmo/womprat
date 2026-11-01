package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestServeFrontendStreamsAssetsAndRejectsDirectories(t *testing.T) {
	a := &App{sessionToken: "session-one", serverPort: 1234}
	for _, path := range []string{"/vendor/xterm.css", "/rdp.js", "/vnc.js"} {
		t.Run(path, func(t *testing.T) {
			name, ok := frontendAssetPath(path)
			if !ok {
				t.Fatal("invalid fixture path")
			}
			want, err := frontendFS.ReadFile(name)
			if err != nil {
				t.Fatal(err)
			}
			w := httptest.NewRecorder()
			a.serveFrontend(w, httptest.NewRequest(http.MethodGet, path, nil))
			if w.Code != http.StatusOK || !bytes.Equal(w.Body.Bytes(), want) {
				t.Fatal("static asset changed")
			}
		})
	}
	for _, path := range []string{"/vendor/", "/missing.js"} {
		w := httptest.NewRecorder()
		a.serveFrontend(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != http.StatusNotFound {
			t.Fatal("missing/directory was served", path, w.Code)
		}
	}
	for _, page := range []string{"/", "/settings.html"} {
		name, _ := frontendAssetPath(page)
		original, err := frontendFS.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, token := range []string{"session-one", "session-two"} {
			a.sessionToken = token
			w := httptest.NewRecorder()
			a.serveFrontend(w, httptest.NewRequest(http.MethodGet, page, nil))
			injection := `<script>window.__SESSION_TOKEN="` + token + `";window.__PORT=1234;</script>`
			want := strings.Replace(string(original), "</head>", injection+"</head>", 1)
			if w.Code != http.StatusOK || w.Body.String() != want {
				t.Fatal("HTML payload changed or used stale session token", page)
			}
			if w.Header().Get("Content-Security-Policy") == "" {
				t.Fatal("CSP lost")
			}
		}
	}
}

type discardFrontendResponse struct{ header http.Header }

func (w discardFrontendResponse) Header() http.Header         { return w.header }
func (w discardFrontendResponse) WriteHeader(int)             {}
func (w discardFrontendResponse) Write(p []byte) (int, error) { return io.Discard.Write(p) }

func BenchmarkServeFrontendHTML(b *testing.B) {
	a := &App{sessionToken: "benchmark-session", serverPort: 1234}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	w := discardFrontendResponse{header: make(http.Header)}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		a.serveFrontend(w, r)
	}
}

func BenchmarkServeFrontendAsset(b *testing.B) {
	a := &App{}
	r := httptest.NewRequest(http.MethodGet, "/rdp.js", nil)
	w := discardFrontendResponse{header: make(http.Header)}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		a.serveFrontend(w, r)
	}
}
