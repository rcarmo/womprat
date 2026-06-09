//go:build windows

package main

import (
	"fmt"
	"sync"
	"time"
	"unsafe"

	webview2edge "github.com/jchv/go-webview2/pkg/edge"
	"golang.org/x/sys/windows"
)

const (
	hwndTop       = 0
	swpNoActivate = 0x0010
	swpNoZOrder   = 0x0004
	swHide        = 0
	swShow        = 5
)

var (
	contentUser32       = windows.NewLazySystemDLL("user32.dll")
	procCreateWindowExW = contentUser32.NewProc("CreateWindowExW")
	procDestroyWindow   = contentUser32.NewProc("DestroyWindow")
	procGetClientRect   = contentUser32.NewProc("GetClientRect")
	procSetWindowPos    = contentUser32.NewProc("SetWindowPos")
	procShowWindow      = contentUser32.NewProc("ShowWindow")
	procIsWindow        = contentUser32.NewProc("IsWindow")
)

type winRect struct{ Left, Top, Right, Bottom int32 }

type nativeContentManager struct {
	mu       sync.Mutex
	parent   uintptr
	dataPath string
	shell    shellWebView
	views    map[string]*nativeContentView
}

func newNativeContentManager(parent unsafe.Pointer, dataPath string, shell shellWebView) (*nativeContentManager, error) {
	parentHWND := uintptr(parent)
	if parentHWND == 0 {
		return nil, fmt.Errorf("missing parent HWND")
	}
	m := &nativeContentManager{parent: parentHWND, dataPath: dataPath, shell: shell, views: map[string]*nativeContentView{}}
	go m.resizeLoop()
	return m, nil
}

func (m *nativeContentManager) Ensure(tabID string) browserContentView {
	m.mu.Lock()
	defer m.mu.Unlock()
	if v := m.views[tabID]; v != nil {
		return v
	}
	v, err := newNativeContentView(m.parent, m.dataPath, tabID, m.shell)
	if err != nil {
		return nilContentView{}
	}
	v.Hide()
	m.views[tabID] = v
	return v
}

func (m *nativeContentManager) Get(tabID string) browserContentView {
	m.mu.Lock()
	defer m.mu.Unlock()
	if v := m.views[tabID]; v != nil {
		return v
	}
	return nil
}

func (m *nativeContentManager) Show(tabID string) {
	m.mu.Lock()
	views := make(map[string]*nativeContentView, len(m.views))
	for id, v := range m.views {
		views[id] = v
	}
	m.mu.Unlock()
	for id, v := range views {
		if id == tabID {
			v.Show()
		} else {
			v.Hide()
		}
	}
}

func (m *nativeContentManager) HideAll() {
	m.mu.Lock()
	views := make([]*nativeContentView, 0, len(m.views))
	for _, v := range m.views {
		views = append(views, v)
	}
	m.mu.Unlock()
	for _, v := range views {
		v.Hide()
	}
}

func (m *nativeContentManager) Destroy(tabID string) {
	m.mu.Lock()
	v := m.views[tabID]
	delete(m.views, tabID)
	m.mu.Unlock()
	if v != nil {
		v.Destroy()
	}
}

func (m *nativeContentManager) resizeLoop() {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for range ticker.C {
		m.mu.Lock()
		views := make([]*nativeContentView, 0, len(m.views))
		for _, v := range m.views {
			views = append(views, v)
		}
		m.mu.Unlock()
		for _, v := range views {
			v.resize()
		}
	}
}

type nilContentView struct{}

func (nilContentView) Navigate(string) {}
func (nilContentView) GoBack()         {}
func (nilContentView) GoForward()      {}
func (nilContentView) Reload()         {}
func (nilContentView) Show()           {}
func (nilContentView) Hide()           {}
func (nilContentView) Destroy()        {}

type nativeContentView struct {
	parent uintptr
	hwnd   uintptr
	tabID  string
	url    string
	shell  shellWebView
	edge   *webview2edge.Chromium
}

func newNativeContentView(parent uintptr, dataPath, tabID string, shell shellWebView) (*nativeContentView, error) {
	className, _ := windows.UTF16PtrFromString("STATIC")
	hwnd, _, err := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(className)), 0,
		0x40000000|0x10000000, 0, browserChromeHeight, 100, 100, parent, 0, 0, 0)
	if hwnd == 0 {
		return nil, fmt.Errorf("create content child window: %w", err)
	}
	cv := &nativeContentView{parent: parent, hwnd: hwnd, tabID: tabID, shell: shell}
	edge := webview2edge.NewChromium()
	edge.DataPath = dataPath
	edge.NavigationCompletedCallback = func(_ *webview2edge.ICoreWebView2, _ *webview2edge.ICoreWebView2NavigationCompletedEventArgs) {
		if cv.shell != nil {
			cv.shell.Eval(fmt.Sprintf("window.wompratNavigationDone(%s,%s)", jsString(cv.tabID), jsString(cv.url)))
		}
	}
	if !edge.Embed(hwnd) {
		procDestroyWindow.Call(hwnd)
		return nil, fmt.Errorf("embed content WebView2")
	}
	cv.edge = edge
	cv.resize()
	return cv, nil
}

func (v *nativeContentView) resize() {
	if v == nil || v.parent == 0 || v.hwnd == 0 {
		return
	}
	ok, _, _ := procIsWindow.Call(v.hwnd)
	if ok == 0 {
		return
	}
	var r winRect
	procGetClientRect.Call(v.parent, uintptr(unsafe.Pointer(&r)))
	width := r.Right - r.Left
	height := r.Bottom - r.Top - browserChromeHeight
	if height < 0 {
		height = 0
	}
	procSetWindowPos.Call(v.hwnd, hwndTop, 0, browserChromeHeight, uintptr(width), uintptr(height), swpNoActivate|swpNoZOrder)
	if v.edge != nil {
		v.edge.Resize()
	}
}

func (v *nativeContentView) Navigate(url string) {
	if v != nil && v.edge != nil {
		v.url = url
		v.edge.Navigate(url)
	}
}
func (v *nativeContentView) GoBack() {
	if v != nil && v.edge != nil {
		v.edge.Eval("history.back()")
	}
}
func (v *nativeContentView) GoForward() {
	if v != nil && v.edge != nil {
		v.edge.Eval("history.forward()")
	}
}
func (v *nativeContentView) Reload() {
	if v != nil && v.edge != nil {
		v.edge.Eval("location.reload()")
	}
}
func (v *nativeContentView) Show() {
	if v != nil && v.hwnd != 0 {
		v.resize()
		var r winRect
		procGetClientRect.Call(v.parent, uintptr(unsafe.Pointer(&r)))
		width := r.Right - r.Left
		height := r.Bottom - r.Top - browserChromeHeight
		if height < 0 {
			height = 0
		}
		// Raise the active browser child above the shell WebView's content area.
		// Without this, WebView2's shell controller can remain above the child HWND,
		// making browser tabs look blank and settings/terminal visibility inconsistent.
		procSetWindowPos.Call(v.hwnd, hwndTop, 0, browserChromeHeight, uintptr(width), uintptr(height), swpNoActivate)
		procShowWindow.Call(v.hwnd, swShow)
		if v.edge != nil {
			v.edge.Resize()
		}
	}
}
func (v *nativeContentView) Hide() {
	if v != nil && v.hwnd != 0 {
		procShowWindow.Call(v.hwnd, swHide)
	}
}
func (v *nativeContentView) Destroy() {
	if v != nil && v.hwnd != 0 {
		procDestroyWindow.Call(v.hwnd)
		v.hwnd = 0
		v.edge = nil
	}
}
