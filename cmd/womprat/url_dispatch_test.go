package main

import (
	"strings"
	"testing"
)

func TestNewBrowserTabRoutesCustomSchemesToViewers(t *testing.T) {
	tests := []struct {
		raw      string
		wantType string
		wantHost string
		wantUser string
		wantPort int
	}{
		{raw: "ssh://me@platinum", wantType: "terminal", wantHost: "platinum", wantUser: "me", wantPort: 22},
		{raw: "vnc://platinum", wantType: "vnc", wantHost: "platinum", wantPort: 5900},
		{raw: "rdp://me@platinum:3389", wantType: "rdp", wantHost: "platinum", wantUser: "me", wantPort: 3389},
	}
	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			app := newTestApp(t)
			app.newBrowserTab(tt.raw)
			if len(app.tabs) != 1 {
				t.Fatalf("tabs = %d, want 1", len(app.tabs))
			}
			tab := app.tabs[0]
			if tab.Type != tt.wantType || tab.Host != tt.wantHost || tab.User != tt.wantUser || tab.Port != tt.wantPort {
				t.Fatalf("tab = %+v, want type=%q host=%q user=%q port=%d", tab, tt.wantType, tt.wantHost, tt.wantUser, tt.wantPort)
			}
			if tab.Type == "browser" || containsAny(app.webview.(*fakeWebView).evals, "http://rdp") || containsAny(app.webview.(*fakeWebView).evals, "http://vnc") || containsAny(app.webview.(*fakeWebView).evals, "http://ssh") {
				t.Fatalf("custom URL routed to browser: tab=%+v evals=%v", tab, app.webview.(*fakeWebView).evals)
			}
		})
	}
}

func TestNavigateBrowserRoutesCustomSchemesBeforeHTTPNormalization(t *testing.T) {
	app := newTestApp(t)
	app.tabs = []Tab{{ID: "browser-1", Type: "browser", Title: "old", URL: "http://old"}}
	app.activeTab = "browser-1"

	app.navigateBrowser("rdp://me@platinum:3389")
	if len(app.tabs) != 2 {
		t.Fatalf("tabs = %+v, want browser + rdp", app.tabs)
	}
	if app.tabs[0].URL != "http://old" {
		t.Fatalf("browser tab was overwritten: %+v", app.tabs[0])
	}
	rdp := app.tabs[1]
	if rdp.Type != "rdp" || rdp.Host != "platinum" || rdp.User != "me" || rdp.Port != 3389 {
		t.Fatalf("rdp tab = %+v", rdp)
	}
	for _, tab := range app.tabs {
		if tab.URL == "http://rdp//me@platinum:3389" || tab.URL == "http://rdp://me@platinum:3389" {
			t.Fatalf("custom URL was HTTP-normalized: %+v", tab)
		}
	}
}

func containsAny(values []string, needle string) bool {
	for _, value := range values {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}
