package main

import (
	"os"
	"strings"
	"testing"
)

type fakeContentView struct {
	urls      []string
	shown     int
	hidden    int
	destroyed bool
	backs     int
	forwards  int
	reloads   int
}

func (v *fakeContentView) Navigate(url string) { v.urls = append(v.urls, url) }
func (v *fakeContentView) GoBack()             { v.backs++ }
func (v *fakeContentView) GoForward()          { v.forwards++ }
func (v *fakeContentView) Reload()             { v.reloads++ }
func (v *fakeContentView) Show()               { v.shown++ }
func (v *fakeContentView) Hide()               { v.hidden++ }
func (v *fakeContentView) Destroy()            { v.destroyed = true }

type fakeContentManager struct {
	views     map[string]*fakeContentView
	shownTabs []string
	hideAlls  int
	destroys  []string
}

func newFakeContentManager() *fakeContentManager {
	return &fakeContentManager{views: map[string]*fakeContentView{}}
}
func (m *fakeContentManager) Ensure(tabID string) browserContentView {
	if m.views[tabID] == nil {
		m.views[tabID] = &fakeContentView{}
	}
	return m.views[tabID]
}
func (m *fakeContentManager) Get(tabID string) browserContentView { return m.views[tabID] }
func (m *fakeContentManager) Show(tabID string) {
	m.shownTabs = append(m.shownTabs, tabID)
	for id, v := range m.views {
		if id == tabID {
			v.Show()
		} else {
			v.Hide()
		}
	}
}
func (m *fakeContentManager) HideAll() {
	m.hideAlls++
	for _, v := range m.views {
		v.Hide()
	}
}
func (m *fakeContentManager) Destroy(tabID string) {
	m.destroys = append(m.destroys, tabID)
	if v := m.views[tabID]; v != nil {
		v.Destroy()
	}
	delete(m.views, tabID)
}

func TestBrowserTabsUseSeparateContentViewsAndSwitchWithoutReload(t *testing.T) {
	app := newTestApp(t)
	manager := newFakeContentManager()
	app.contentViews = manager
	app.newBrowserTab("http://one.example")
	firstID := app.activeTab
	app.newBrowserTab("http://two.example")
	secondID := app.activeTab
	if firstID == secondID || len(manager.views) != 2 {
		t.Fatalf("ids %q/%q views=%d", firstID, secondID, len(manager.views))
	}
	app.switchTab(firstID)
	if got := manager.views[firstID].urls; len(got) != 1 || got[0] != "http://one.example" {
		t.Fatalf("first view reloaded or wrong urls: %#v", got)
	}
	if manager.views[firstID].shown == 0 || manager.views[secondID].hidden == 0 {
		t.Fatalf("visibility first=%+v second=%+v", manager.views[firstID], manager.views[secondID])
	}
}

func TestLocalTabsHideBrowserContentAndCloseDestroysOnlyClosedView(t *testing.T) {
	app := newTestApp(t)
	manager := newFakeContentManager()
	app.contentViews = manager
	app.tabs = []Tab{{ID: "b", Type: "browser", URL: "http://b"}, {ID: "t", Type: "terminal", Host: "h"}}
	manager.Ensure("b")
	app.activeTab = "b"
	app.switchTab("t")
	if manager.hideAlls == 0 {
		t.Fatal("terminal switch did not hide browser content")
	}
	app.tabs = []Tab{{ID: "b", Type: "browser", URL: "http://b"}, {ID: "c", Type: "browser", URL: "http://c"}}
	manager.Ensure("c")
	app.activeTab = "b"
	app.closeTab("b")
	if _, ok := manager.views["b"]; ok {
		t.Fatal("closed browser content view still present")
	}
	if _, ok := manager.views["c"]; !ok {
		t.Fatal("remaining browser content view destroyed")
	}
}

func TestFrontendSplitWebViewShellContracts(t *testing.T) {
	b, err := os.ReadFile("frontend/index.html")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{
		"function activateTab(id, options = {})",
		"womprat_switchTab(id)",
		"window.showBrowserTab = async function",
		"womprat_browserBack",
		"womprat_browserReload",
		"browser-placeholder",
		"function setURLProgress(_active, _done = false)",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("frontend missing %q", want)
		}
	}
	if strings.Contains(s, "womprat_closeTab(id);\n    return;") {
		t.Fatal("closeTab returns before local UI update")
	}
}
