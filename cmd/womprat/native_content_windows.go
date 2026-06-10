//go:build windows

package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sync"
	"sync/atomic"
	"unsafe"

	webview2edge "github.com/jchv/go-webview2/pkg/edge"
	"golang.org/x/sys/windows"
)

const (
	browserChromeHeight = 84
	hwndTop             = 0
	swpNoActivate       = 0x0010
	swpNoZOrder         = 0x0004
	swHide              = 0
	swShow              = 5
	wsChild             = 0x40000000
	wsVisible           = 0x10000000
	wsClipChildren      = 0x02000000
	wsClipSiblings      = 0x04000000
)

var (
	contentUser32       = windows.NewLazySystemDLL("user32.dll")
	procCreateWindowExW = contentUser32.NewProc("CreateWindowExW")
	procDestroyWindow   = contentUser32.NewProc("DestroyWindow")
	procGetClientRect   = contentUser32.NewProc("GetClientRect")
	procSetWindowPos    = contentUser32.NewProc("SetWindowPos")
	procShowWindow      = contentUser32.NewProc("ShowWindow")
	procIsWindow        = contentUser32.NewProc("IsWindow")
	procGetDpiForWindow = contentUser32.NewProc("GetDpiForWindow")
)

// reportedChromePx is the shell chrome height in physical pixels, measured by the
// shell itself (chrome bottom * devicePixelRatio) and reported via a binding.
// This is DPI-proof, unlike scaling a CSS constant by GetDpiForWindow which can
// disagree with the WebView's own DPI handling.
var reportedChromePx int32

func setReportedChromePx(px int32) {
	if px > 0 {
		atomic.StoreInt32(&reportedChromePx, clampChromePx(px))
	}
}

func clampChromePx(px int32) int32 {
	const minChromePx int32 = 24
	const maxChromePx int32 = 512
	if px < minChromePx {
		return minChromePx
	}
	if px > maxChromePx {
		return maxChromePx
	}
	return px
}

// chromePx returns the shell chrome height in physical pixels for the given
// window. It prefers the shell-reported value and falls back to scaling the CSS
// chrome height by the window DPI.
func chromePx(hwnd uintptr) int32 {
	if r := atomic.LoadInt32(&reportedChromePx); r > 0 {
		return clampChromePx(r)
	}
	dpi, _, _ := procGetDpiForWindow.Call(hwnd)
	if dpi == 0 {
		dpi = 96
	}
	return int32(browserChromeHeight) * int32(dpi) / 96
}

type winRect struct{ Left, Top, Right, Bottom int32 }

type nativeContentManager struct {
	mu            sync.Mutex
	parent        uintptr
	shellHWND     uintptr
	dataPath      string
	shell         shellWebView
	browserActive string
	views         map[string]*nativeContentView
}

func newNativeContentManager(parent uintptr, shellHWND uintptr, dataPath string, shell shellWebView) (*nativeContentManager, error) {
	if parent == 0 || shellHWND == 0 {
		return nil, fmt.Errorf("missing parent/shell HWND")
	}
	m := &nativeContentManager{parent: parent, shellHWND: shellHWND, dataPath: dataPath, shell: shell, views: map[string]*nativeContentView{}}
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
		log.Printf("content WebView for %s unavailable: %v", tabID, err)
		if m.shell != nil {
			m.shell.Eval(fmt.Sprintf("window.wompratContentError(%s,%s)", jsString(tabID), jsString(err.Error())))
		}
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
	m.browserActive = tabID
	views := make(map[string]*nativeContentView, len(m.views))
	for id, v := range m.views {
		views[id] = v
	}
	m.mu.Unlock()
	m.resizeAll()
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
	m.browserActive = ""
	views := make([]*nativeContentView, 0, len(m.views))
	for _, v := range m.views {
		views = append(views, v)
	}
	m.mu.Unlock()
	m.resizeAll()
	for _, v := range views {
		v.Hide()
	}
}

func (m *nativeContentManager) Destroy(tabID string) {
	m.mu.Lock()
	v := m.views[tabID]
	delete(m.views, tabID)
	wasActive := m.browserActive == tabID
	if wasActive {
		m.browserActive = ""
	}
	m.mu.Unlock()
	if v != nil {
		v.Destroy()
	}
	if wasActive {
		// The active browser view is gone; restore the shell to full height so
		// settings/terminal/home are not left clipped behind a stale chrome-only
		// shell layout.
		m.resizeAll()
	}
}

func nativeClientRect(hwnd uintptr, context string) (winRect, bool) {
	var r winRect
	if ret, _, err := procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&r))); ret == 0 {
		log.Printf("%s get client rect failed: %v", context, err)
		return winRect{}, false
	}
	return r, true
}

func nativeSetWindowPos(hwnd, insertAfter uintptr, x, y, width, height int32, flags uintptr, context string) bool {
	if ret, _, err := procSetWindowPos.Call(hwnd, insertAfter, uintptr(x), uintptr(y), uintptr(width), uintptr(height), flags); ret == 0 {
		log.Printf("%s set window pos failed: %v", context, err)
		return false
	}
	return true
}

func nativeShowWindow(hwnd uintptr, cmd uintptr, context string) bool {
	if ret, _, err := procShowWindow.Call(hwnd, cmd); ret == 0 {
		log.Printf("%s show window failed: %v", context, err)
		return false
	}
	return true
}

func (m *nativeContentManager) resizeAll() {
	if m == nil || m.parent == 0 || m.shellHWND == 0 {
		return
	}
	r, ok := nativeClientRect(m.parent, "content manager resize")
	if !ok {
		return
	}
	width := r.Right - r.Left
	height := r.Bottom - r.Top
	chrome := chromePx(m.parent)
	m.mu.Lock()
	active := m.browserActive
	views := make([]*nativeContentView, 0, len(m.views))
	for _, v := range m.views {
		views = append(views, v)
	}
	m.mu.Unlock()
	if active == "" {
		nativeSetWindowPos(m.shellHWND, hwndTop, 0, 0, width, height, swpNoActivate, "shell full resize")
	} else {
		nativeSetWindowPos(m.shellHWND, hwndTop, 0, 0, width, chrome, swpNoActivate, "shell chrome resize")
	}
	if m.shell != nil {
		m.shell.Resize()
	}
	for _, v := range views {
		v.resize()
	}
}

func (m *nativeContentManager) notifyShellResize() {
	if m == nil || m.shell == nil {
		return
	}
	m.shell.Eval("window.wompratOnHostResize && window.wompratOnHostResize()")
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
	if err := registerChildClass(); err != nil {
		return nil, err
	}
	hwnd, _, err := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(childClassName)), 0,
		wsChild|wsVisible|wsClipChildren|wsClipSiblings, 0, browserChromeHeight, 100, 100, parent, 0, 0, 0)
	if hwnd == 0 {
		return nil, fmt.Errorf("create content child window: %w", err)
	}
	cv := &nativeContentView{parent: parent, hwnd: hwnd, tabID: tabID, shell: shell}
	edge := webview2edge.NewChromium()
	edge.MessageCallback = func(raw string) {
		if action := parseHotkeyMessage(raw); action != "" && cv.shell != nil {
			arg := parseHotkeyArg(raw)
			log.Printf("content: hotkey %s arg=%q tab=%s", action, arg, cv.tabID)
			cv.shell.Eval(fmt.Sprintf("window.wompratBrowserHotkey(%s,%s)", jsString(action), jsString(arg)))
			return
		}
		// Browser content reports its document title and favicon via postMessage so
		// the shell tab can display the page title and icon instead of the raw URL.
		title, favicon := parseTitleMessage(raw)
		if (title != "" || favicon != "") && cv.shell != nil {
			cv.shell.Eval(fmt.Sprintf("window.wompratSetTabMeta(%s,%s,%s)", jsString(cv.tabID), jsString(title), jsString(favicon)))
		}
	}
	// All browser tabs share the same WebView2 user-data folder as the shell so
	// they share one browser process and one proxy/network configuration. Using a
	// separate environment/folder per tab spawned multiple browser processes and
	// broke proxy-routed connectivity.
	if err := os.MkdirAll(dataPath, 0700); err != nil {
		procDestroyWindow.Call(hwnd)
		return nil, fmt.Errorf("create content data path: %w", err)
	}
	edge.DataPath = dataPath
	edge.NavigationCompletedCallback = func(_ *webview2edge.ICoreWebView2, _ *webview2edge.ICoreWebView2NavigationCompletedEventArgs) {
		// Re-inject the bridge on every completed navigation as well as via Init,
		// because AddScriptToExecuteOnDocumentCreated can miss the very first page
		// (script registration races the first navigation). The bridge self-guards
		// against duplicate listeners.
		if cv.edge != nil {
			cv.edge.Eval(browserTitleReporterJS)
		}
		if cv.shell != nil {
			cv.shell.Eval(fmt.Sprintf("window.wompratNavigationDone(%s,%s)", jsString(cv.tabID), jsString(cv.url)))
		}
	}
	if !edge.Embed(hwnd) {
		procDestroyWindow.Call(hwnd)
		return nil, fmt.Errorf("embed content WebView2")
	}
	cv.edge = edge
	if err := edge.PutAreBrowserAcceleratorKeysEnabled(false); err != nil {
		log.Printf("content: disable browser accelerator keys failed: %v", err)
	}
	edge.Init(browserTitleReporterJS)
	cv.resize()
	log.Printf("content: created browser WebView for tab %s hwnd=0x%x", tabID, hwnd)
	return cv, nil
}

const browserTitleReporterJS = `(function(){
  if (window.__wompratBridge) return; window.__wompratBridge = 1;
  function favicon(){
    try {
      var links = document.querySelectorAll('link[rel~="icon"],link[rel="shortcut icon"],link[rel="apple-touch-icon"]');
      for (var i=0;i<links.length;i++){ var h=links[i].getAttribute('href'); if(h){ return new URL(h, location.href).href; } }
      return location.origin + '/favicon.ico';
    } catch(e){ return ''; }
  }
  function reportBridgeError(context, error){ try { console.debug('womprat bridge '+context+' failed', error); } catch(e) { /* console unavailable */ } }
  function send(){ try{ window.chrome.webview.postMessage(JSON.stringify({wompratTitle: document.title || location.hostname || location.href, wompratFavicon: favicon()})); }catch(e){ reportBridgeError('metadata post', e); } }
  document.addEventListener('DOMContentLoaded', send);
  window.addEventListener('load', send);
  try { var t=document.querySelector('title'); if(t){ new MutationObserver(send).observe(t,{childList:true}); } } catch(e){ reportBridgeError('title observer', e); }
  try { new MutationObserver(function(){ send(); }).observe(document.head||document.documentElement,{subtree:true,childList:true}); } catch(e){ reportBridgeError('head observer', e); }
  function fire(action, arg){ try{ window.chrome.webview.postMessage(JSON.stringify({wompratKey: action, wompratArg: arg||''})); }catch(e){ reportBridgeError('hotkey post', e); } }
  document.addEventListener('keydown', function(e){
    var ctrl = e.ctrlKey || e.metaKey;
    var k = (e.key||'').toLowerCase();
    if (ctrl && k==='l'){ e.preventDefault(); fire('focusUrl'); return; }
    if (ctrl && k==='t'){ e.preventDefault(); fire('newTab'); return; }
    if (ctrl && k==='w'){ e.preventDefault(); fire('closeTab'); return; }
    if (k==='f5' || (ctrl && k==='r')){ e.preventDefault(); fire('reload'); return; }
    if (e.altKey && !ctrl && k==='arrowleft'){ e.preventDefault(); fire('back'); return; }
    if (e.altKey && !ctrl && k==='arrowright'){ e.preventDefault(); fire('forward'); return; }
    if (ctrl && (k==='tab'||k==='pagedown')){ e.preventDefault(); fire(e.shiftKey?'prevTab':'nextTab'); return; }
    if (ctrl && k==='pageup'){ e.preventDefault(); fire('prevTab'); return; }
    if (ctrl && /^[1-9]$/.test(k)){ e.preventDefault(); fire('tabAt', k); return; }
  }, true);
  send();
})();`

func parseTitleMessage(raw string) (string, string) {
	if len(raw) > maxWebViewMessage {
		return "", ""
	}
	var m struct {
		WompratTitle   string `json:"wompratTitle"`
		WompratFavicon string `json:"wompratFavicon"`
	}
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return "", ""
	}
	return sanitizeBrowserTitle(m.WompratTitle), sanitizeFaviconURL(m.WompratFavicon)
}

func parseHotkeyMessage(raw string) string {
	if len(raw) > maxWebViewMessage {
		return ""
	}
	var m struct {
		WompratKey string `json:"wompratKey"`
	}
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return ""
	}
	if !validBrowserHotkeyAction(m.WompratKey) {
		return ""
	}
	return m.WompratKey
}

func parseHotkeyArg(raw string) string {
	if len(raw) > maxWebViewMessage {
		return ""
	}
	var m struct {
		WompratArg string `json:"wompratArg"`
	}
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return ""
	}
	return sanitizeBrowserHotkeyArg(m.WompratArg)
}

func (v *nativeContentView) resize() {
	if v == nil || v.parent == 0 || v.hwnd == 0 {
		return
	}
	isWindow, _, _ := procIsWindow.Call(v.hwnd)
	if isWindow == 0 {
		return
	}
	r, ok := nativeClientRect(v.parent, "content view resize")
	if !ok {
		return
	}
	chrome := chromePx(v.parent)
	width := r.Right - r.Left
	height := r.Bottom - r.Top - chrome
	if height < 0 {
		height = 0
	}
	nativeSetWindowPos(v.hwnd, hwndTop, 0, chrome, width, height, swpNoActivate|swpNoZOrder, "content view resize")
	if v.edge != nil {
		v.edge.Resize()
	}
}

func (v *nativeContentView) Navigate(url string) {
	if v != nil && v.edge != nil {
		v.url = url
		log.Printf("content: tab %s navigate %s", v.tabID, url)
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
		r, ok := nativeClientRect(v.parent, "content view show")
		if !ok {
			return
		}
		chrome := chromePx(v.parent)
		width := r.Right - r.Left
		height := r.Bottom - r.Top - chrome
		if height < 0 {
			height = 0
		}
		// Position with the DPI-correct chrome offset (same as resize) and raise the
		// active browser child above the shell WebView content area. Using the raw
		// CSS constant here previously made the content jump on every tab switch.
		nativeSetWindowPos(v.hwnd, hwndTop, 0, chrome, width, height, swpNoActivate, "content view show")
		nativeShowWindow(v.hwnd, swShow, "content view show")
		if v.edge != nil {
			v.edge.Resize()
		}
	}
}
func (v *nativeContentView) Hide() {
	if v != nil && v.hwnd != 0 {
		nativeShowWindow(v.hwnd, swHide, "content view hide")
	}
}
func (v *nativeContentView) Destroy() {
	if v != nil && v.hwnd != 0 {
		procDestroyWindow.Call(v.hwnd)
		v.hwnd = 0
		v.edge = nil
	}
}
