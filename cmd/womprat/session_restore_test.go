package main

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestShellTabsRestoresSavedSessionOnceAfterUnlock(t *testing.T) {
	a := newTestApp(t)
	a.config.RestoreTabs = true
	a.config.OpenTabs = []SavedTab{
		{Type: "browser", URL: "https://first.example/", Title: "First"},
		{Type: "settings", URL: "settings:"},
		{Type: "browser", URL: "file:///etc/passwd"},
		{Type: "terminal", Host: "ssh.example"},
		{Type: "vnc", URL: "vnc://desktop.example:5900"},
	}
	saved := append([]SavedTab(nil), a.config.OpenTabs...)
	a.locked = true
	var snapshot struct {
		Tabs []Tab `json:"tabs"`
	}
	if err := json.Unmarshal([]byte(a.shellTabs()), &snapshot); err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Tabs) != 0 || a.sessionRestored || len(a.tabs) != 0 {
		t.Fatal("restored or exposed saved state while locked")
	}
	a.locked = false
	if err := json.Unmarshal([]byte(a.shellTabs()), &snapshot); err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Tabs) != 3 || a.activeTab != snapshot.Tabs[0].ID || snapshot.Tabs[0].URL != "https://first.example/" || snapshot.Tabs[1].Type != "terminal" || snapshot.Tabs[2].Type != "vnc" {
		t.Fatalf("restored state: %+v active=%q", snapshot, a.activeTab)
	}
	for _, tab := range snapshot.Tabs {
		if !validTabID(tab.ID) {
			t.Fatalf("invalid restored ID %q", tab.ID)
		}
	}
	if !reflect.DeepEqual(saved, a.config.OpenTabs) {
		t.Fatal("reading session rewrote saved snapshot")
	}
	first := a.shellTabs()
	if first != a.shellTabs() {
		t.Fatal("shell hydration changed restored IDs or duplicated tabs")
	}
	a.tabs = nil // Closing all tabs must not bring the saved session back.
	a.activeTab = ""
	a.shellTabs()
	if len(a.tabs) != 0 {
		t.Fatal("closed session restored twice")
	}
}

func TestShellTabsRespectsDisabledRestoreAndExistingTabs(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(map[bool]string{false: "disabled", true: "existing"}[existing], func(t *testing.T) {
			a := newTestApp(t)
			a.config.OpenTabs = []SavedTab{{Type: "browser", URL: "https://saved.example/"}}
			if existing {
				a.config.RestoreTabs = true
				a.tabs = []Tab{{ID: "current", Type: "browser", URL: "https://current.example/"}}
				a.activeTab = "current"
			}
			a.shellTabs()
			if existing && (len(a.tabs) != 1 || a.tabs[0].ID != "current") || !existing && len(a.tabs) != 0 {
				t.Fatalf("changed current session: %+v", a.tabs)
			}
			if len(a.config.OpenTabs) != 1 || a.config.OpenTabs[0].URL != "https://saved.example/" {
				t.Fatal("discarded saved history")
			}
		})
	}
}
