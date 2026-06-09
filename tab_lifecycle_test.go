package main

import "testing"

func TestSwitchExistingBrowserTabDoesNotReloadContentView(t *testing.T) {
	app := newTestApp(t)
	manager := newFakeContentManager()
	app.contentViews = manager
	app.tabs = []Tab{{ID: "a", Type: "browser", URL: "http://a"}, {ID: "b", Type: "browser", URL: "http://b"}}
	manager.Ensure("a").Navigate("http://a")
	manager.Ensure("b").Navigate("http://b")
	app.activeTab = "b"

	app.switchTab("a")
	if got := manager.views["a"].urls; len(got) != 1 || got[0] != "http://a" {
		t.Fatalf("existing view was reloaded on switch: %#v", got)
	}
	if manager.views["a"].shown == 0 || manager.views["b"].hidden == 0 {
		t.Fatalf("switch visibility = a:%+v b:%+v", manager.views["a"], manager.views["b"])
	}
}

func TestSwitchMissingTabDoesNotCorruptActiveTab(t *testing.T) {
	app := newTestApp(t)
	manager := newFakeContentManager()
	app.contentViews = manager
	app.tabs = []Tab{{ID: "a", Type: "browser", URL: "http://a"}}
	app.activeTab = "a"

	app.switchTab("missing")
	if app.activeTab != "a" {
		t.Fatalf("active tab changed to %q", app.activeTab)
	}
	if len(manager.views) != 0 || len(manager.shownTabs) != 0 || manager.hideAlls != 0 {
		t.Fatalf("content views touched for missing tab: %+v", manager)
	}
}

func TestRapidCloseDifferentTabsIsNotDebounced(t *testing.T) {
	app := newTestApp(t)
	manager := newFakeContentManager()
	app.contentViews = manager
	app.tabs = []Tab{{ID: "a", Type: "browser", URL: "http://a"}, {ID: "b", Type: "browser", URL: "http://b"}, {ID: "c", Type: "browser", URL: "http://c"}}
	manager.Ensure("a")
	manager.Ensure("b")
	manager.Ensure("c")
	app.activeTab = "c"

	app.closeTab("a")
	app.closeTab("b")
	if len(app.tabs) != 1 || app.tabs[0].ID != "c" {
		t.Fatalf("rapid close left tabs: %+v", app.tabs)
	}
	if _, ok := manager.views["a"]; ok {
		t.Fatal("view a was not destroyed")
	}
	if _, ok := manager.views["b"]; ok {
		t.Fatal("view b was not destroyed")
	}
	if _, ok := manager.views["c"]; !ok {
		t.Fatal("view c should remain")
	}
}

func TestCloseMissingTabIsNoop(t *testing.T) {
	app := newTestApp(t)
	manager := newFakeContentManager()
	app.contentViews = manager
	app.tabs = []Tab{{ID: "a", Type: "browser", URL: "http://a"}}
	app.activeTab = "a"

	app.closeTab("missing")
	if len(app.tabs) != 1 || app.activeTab != "a" {
		t.Fatalf("missing close changed state: tabs=%+v active=%q", app.tabs, app.activeTab)
	}
	if len(manager.destroys) != 0 {
		t.Fatalf("destroyed missing tab: %#v", manager.destroys)
	}
}
