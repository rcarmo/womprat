package main

import "testing"

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

func TestBrowserTabsUseSeparateContentViews(t *testing.T) {
	app := newTestApp(t)
	manager := newFakeContentManager()
	app.contentViews = manager

	app.newBrowserTab("http://one.example")
	firstID := app.activeTab
	app.newBrowserTab("http://two.example")
	secondID := app.activeTab

	if firstID == secondID || len(manager.views) != 2 {
		t.Fatalf("expected two separate content views, ids %q/%q views=%d", firstID, secondID, len(manager.views))
	}
	if got := manager.views[firstID].urls; len(got) != 1 || got[0] != "http://one.example" {
		t.Fatalf("first view urls = %#v", got)
	}
	if got := manager.views[secondID].urls; len(got) != 1 || got[0] != "http://two.example" {
		t.Fatalf("second view urls = %#v", got)
	}

	app.switchTab(firstID)
	if manager.views[firstID].shown == 0 || manager.views[secondID].hidden == 0 {
		t.Fatalf("switch did not show first/hide second: first=%+v second=%+v", manager.views[firstID], manager.views[secondID])
	}
	if got := manager.views[firstID].urls; len(got) != 1 || got[0] != "http://one.example" {
		t.Fatalf("first view should preserve state without reload: %#v", got)
	}
}

func TestClosingBrowserTabDestroysOnlyThatContentView(t *testing.T) {
	app := newTestApp(t)
	manager := newFakeContentManager()
	app.contentViews = manager
	app.tabs = []Tab{{ID: "a", Type: "browser", URL: "http://a"}, {ID: "b", Type: "browser", URL: "http://b"}}
	manager.Ensure("a")
	manager.Ensure("b")
	app.activeTab = "a"

	app.closeTab("a")
	if _, ok := manager.views["a"]; ok {
		t.Fatal("closed tab content view still present")
	}
	if _, ok := manager.views["b"]; !ok {
		t.Fatal("remaining tab content view destroyed")
	}
	if len(manager.destroys) != 1 || manager.destroys[0] != "a" {
		t.Fatalf("destroys = %#v", manager.destroys)
	}
}
