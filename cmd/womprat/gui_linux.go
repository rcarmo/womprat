//go:build linux && webkitgui

package main

/*
#cgo pkg-config: gtk+-3.0 webkit2gtk-4.1
#include "webkit_linux.h"
#include <stdlib.h>
*/
import "C"

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"runtime"
	"runtime/cgo"
	"sort"
	"strings"
	"sync"
	"time"
	"unsafe"
)

var installLinuxGUIAcceptance = func(*linuxContentManager) {}
var profileLinuxGUI = func() func() { return func() {} }

type linuxShell struct{ host *C.WompratGTK }

func (s *linuxShell) Eval(js string) {
	text := C.CString(js)
	defer C.free(unsafe.Pointer(text))
	C.womprat_gtk_eval(s.host, text)
}
func (s *linuxShell) Navigate(string) {} // Shell stays on the authenticated local origin.
func (s *linuxShell) Resize()         {}

type linuxContentManager struct {
	app     *App
	host    *C.WompratGTK
	shell   *linuxShell
	views   map[string]*linuxContentView
	queueMu sync.Mutex
	queue   []func()
	closed  bool
	handle  cgo.Handle
}
type linuxContentView struct {
	manager     *linuxContentManager
	id          string
	certificate certificateDecision
}

func (m *linuxContentManager) call(id, url string, op int) {
	a, b := C.CString(id), C.CString(url)
	defer C.free(unsafe.Pointer(a))
	defer C.free(unsafe.Pointer(b))
	C.womprat_gtk_tab(m.host, a, b, C.int(op))
}
func (m *linuxContentManager) Ensure(id string) browserContentView {
	if !validTabID(id) {
		return nil
	}
	if v := m.views[id]; v != nil {
		return v
	}
	v := &linuxContentView{manager: m, id: id}
	m.views[id] = v
	m.call(id, "", 0)
	return v
}
func (m *linuxContentManager) Get(id string) browserContentView {
	if v := m.views[id]; v != nil {
		return v
	}
	return nil
}
func (m *linuxContentManager) Show(id string)    { m.call(id, "", 5) }
func (m *linuxContentManager) HideAll()          { m.call("", "", 7) }
func (m *linuxContentManager) Destroy(id string) { delete(m.views, id); m.call(id, "", 8) }
func (v *linuxContentView) Navigate(url string)  { v.certificate.clear(); v.manager.call(v.id, url, 1) }
func (v *linuxContentView) GoBack()              { v.certificate.clear(); v.manager.call(v.id, "", 2) }
func (v *linuxContentView) GoForward()           { v.certificate.clear(); v.manager.call(v.id, "", 3) }
func (v *linuxContentView) Reload()              { v.certificate.clear(); v.manager.call(v.id, "", 4) }
func (v *linuxContentView) Show()                { v.manager.Show(v.id) }
func (v *linuxContentView) Hide()                { v.manager.call(v.id, "", 6) }
func (v *linuxContentView) Destroy()             { v.manager.Destroy(v.id) }
func (m *linuxContentManager) dispatch(fn func()) {
	m.queueMu.Lock()
	if m.closed {
		m.queueMu.Unlock()
		return
	}
	m.queue = append(m.queue, fn)
	C.womprat_gtk_dispatch(C.uintptr_t(m.handle))
	m.queueMu.Unlock()
}

//export goWompratClosed
func goWompratClosed(handle C.uintptr_t) {
	m := cgo.Handle(handle).Value().(*linuxContentManager)
	m.queueMu.Lock()
	m.closed = true
	m.queue = nil
	m.queueMu.Unlock()
}

//export goWompratDispatch
func goWompratDispatch(handle C.uintptr_t) {
	m := cgo.Handle(handle).Value().(*linuxContentManager)
	m.queueMu.Lock()
	if m.closed {
		m.queueMu.Unlock()
		return
	}
	work := m.queue
	m.queue = nil
	m.queueMu.Unlock()
	for _, fn := range work {
		fn()
	}
}

//export goWompratEvent
func goWompratEvent(handle C.uintptr_t, id *C.char, event C.int, uri, detail *C.char, ok, history C.int) {
	m := cgo.Handle(handle).Value().(*linuxContentManager)
	tab := C.GoString(id)
	url, text := C.GoString(uri), C.GoString(detail)
	if m.views[tab] == nil {
		return
	}
	switch int(event) {
	case 1:
		m.views[tab].certificate.starting(url)
		m.shell.Eval(fmt.Sprintf("window.setTabLoading && window.setTabLoading(%s,true)", jsString(tab)))
	case 2:
		if isBrowserURL(url) {
			m.shell.Eval(fmt.Sprintf("window.wompratSyncTabURL(%s,%s);window.wompratSetTabTitle(%s,%s)", jsString(tab), jsString(url), jsString(tab), jsString(sanitizeBrowserTitle(text))))
		}
	case 3:
		if ok != 0 {
			m.views[tab].certificate.clear()
			m.shell.Eval(fmt.Sprintf("window.wompratCertificatePrompt(%s,\"\",\"\")", jsString(tab)))
		}
		if ok != 0 && isBrowserURL(url) {
			m.shell.Eval(fmt.Sprintf("window.wompratSyncTabURL(%s,%s)", jsString(tab), jsString(url)))
		}
		message := ""
		if ok == 0 {
			message = "Navigation failed: " + text
			if !m.app.tailscaleConnected() {
				message += ". Connect Tailscale in Settings; no host-network fallback is enabled."
			}
		}
		m.shell.Eval(fmt.Sprintf("window.wompratNavigationDone(%s,%s,%t,0,%s)", jsString(tab), jsString(url), ok != 0, jsString(message)))
	case 4:
		m.dispatch(func() {
			if !m.app.openSpecialURL(text) {
				m.app.newBrowserTab(text)
			}
		})
	case 5:
		status := int32(0)
		// GTlsCertificateFlags: unknown CA=1, bad identity=2, not activated=4,
		// expired=8, revoked=16, insecure=32, generic=64. Never override revocation.
		if int(ok)&(16|32|64) == 0 {
			if int(ok)&2 != 0 {
				status = webErrorStatusCertificateCommonNameWrong
			} else if int(ok)&8 != 0 {
				status = webErrorStatusCertificateExpired
			} else if int(ok)&(1|4) != 0 {
				status = webErrorStatusCertificateInvalid
			}
		}
		token, err := generateSessionToken()
		if err == nil {
			m.views[tab].certificate.detected(url, text, status, token)
		}
		prompt := m.views[tab].certificate.pending
		m.shell.Eval(fmt.Sprintf("window.wompratCertificatePrompt(%s,%s,%s);window.wompratNavigationDone(%s,%s,false,0,%s)", jsString(tab), jsString(prompt.Token), jsString(prompt.URL), jsString(tab), jsString(url), jsString("TLS certificate validation failed. Consent applies to this host/certificate for this browser session.")))
	case 6:
		if ok == 0 {
			m.shell.Eval(fmt.Sprintf("window.wompratDownloadError(%s)", jsString("Could not retrieve browser cookies")))
			return
		}
		var cookies []*http.Cookie
		for _, line := range strings.Split(text, "\n") {
			if c, err := http.ParseSetCookie(line); err == nil {
				cookies = append(cookies, c)
			}
		}
		ticket, err := m.app.createDownloadTicket(url, cookies)
		if err != nil {
			m.shell.Eval(fmt.Sprintf("window.wompratDownloadError(%s)", jsString(err.Error())))
			return
		}
		m.shell.Eval(fmt.Sprintf("window.triggerDownload(%s,%s)", jsString(url), jsString(ticket)))
	}
	m.shell.Eval(fmt.Sprintf("window.wompratSetNavState(%s,%t,%t)", jsString(tab), history&1 != 0, history&2 != 0))
}

//export goWompratQueryResult
func goWompratQueryResult(handle C.uintptr_t, value *C.char) {
	h := cgo.Handle(handle)
	result := h.Value().(chan string)
	result <- C.GoString(value)
	h.Delete()
}

//export goWompratMessage
func goWompratMessage(handle C.uintptr_t, id, raw *C.char) {
	m := cgo.Handle(handle).Value().(*linuxContentManager)
	tab, message := C.GoString(id), C.GoString(raw)
	if len(message) > maxWebViewMessage {
		return
	}
	if tab != "" {
		if m.views[tab] == nil {
			return
		}
		if action := parseHotkeyMessage(message); action != "" {
			m.app.mu.Lock()
			active := m.app.activeTab == tab
			m.app.mu.Unlock()
			if active {
				m.shell.Eval(fmt.Sprintf("window.wompratBrowserHotkey(%s,%s)", jsString(action), jsString(parseHotkeyArg(message))))
			}
			return
		}
		if action, url := parseBrowserActionMessage(message); action != "" {
			if action == "open" {
				m.dispatch(func() { m.app.newBrowserTab(url) })
			} else {
				m.call(tab, url, 10)
			}
			return
		}
		title, icon := parseTitleMessage(message)
		url := parseURLMessage(message)
		m.shell.Eval(fmt.Sprintf("window.wompratSetTabMeta(%s,%s,%s)", jsString(tab), jsString(title), jsString(icon)))
		if url != "" {
			m.shell.Eval(fmt.Sprintf("window.wompratSyncTabURL(%s,%s)", jsString(tab), jsString(url)))
		}
		return
	}
	var request struct {
		ID   int               `json:"id"`
		Name string            `json:"name"`
		Args []json.RawMessage `json:"args"`
	}
	if json.Unmarshal([]byte(message), &request) != nil {
		return
	}
	arg := func(i int) string {
		var s string
		if i < len(request.Args) {
			json.Unmarshal(request.Args[i], &s)
		}
		return s
	}
	integer := func(i int) int {
		var n int
		if i < len(request.Args) {
			json.Unmarshal(request.Args[i], &n)
		}
		return n
	}
	// Blocking Go persistence/network work runs off the GTK thread. Native
	// content operations return through the existing App UI dispatcher.
	go func() {
		m.app.mu.Lock()
		locked := m.app.locked
		m.app.mu.Unlock()
		if locked && request.Name != "focusShell" && request.Name != "setChromeHeight" {
			m.dispatch(func() { m.shell.Eval(fmt.Sprintf("window.__wompratResolve(%d,null)", request.ID)) })
			return
		}
		var result any
		switch request.Name {
		case "shellReady":
			fmt.Println("WOMPRAT_GUI_READY=1")
		case "getTabs":
			m.app.mu.Lock()
			result = jsString(map[string]any{"tabs": m.app.tabs, "activeTab": m.app.activeTab, "port": m.app.serverPort, "token": m.app.sessionToken})
			m.app.mu.Unlock()
		case "getNetworkState":
			m.app.mu.Lock()
			result = jsString(map[string]any{"exitNode": m.app.config.ExitNode, "exitActive": m.app.exitNodeActive, "tsConnected": m.app.tsServer != nil})
			m.app.mu.Unlock()
		case "newBrowser":
			m.app.newBrowserTab(arg(0))
		case "navigate":
			m.app.navigateBrowser(arg(0))
		case "switchTab":
			m.app.switchTab(arg(0))
		case "closeTab":
			m.app.closeTab(arg(0))
		case "reorderTab":
			m.app.reorderTab(arg(0), arg(1))
		case "forgetTab":
			m.app.forgetTab(arg(0))
		case "registerLocalTab":
			m.app.registerLocalTab(arg(0))
		case "newTerminal":
			m.app.newTerminalTab(arg(0), arg(1), integer(2))
		case "newVNC":
			m.app.newVNCTab(arg(0))
		case "newRDP":
			m.app.newRDPTab(arg(0))
		case "openSettings":
			m.app.openSettingsTab()
		case "goHome":
			m.app.goHome()
		case "clearActiveTab":
			m.app.clearActiveTab()
		case "updateTitle":
			m.app.updateActiveBrowserTitle(arg(0), arg(1), arg(2))
		case "setChromeHeight":
			px := integer(0)
			m.dispatch(func() { C.womprat_gtk_chrome(m.host, C.int(px)) })
		case "proceedCertificate":
			tab, token := arg(0), arg(1)
			m.dispatch(func() {
				accepted := false
				m.app.mu.Lock()
				active := m.app.activeTab == tab && !m.app.locked
				m.app.mu.Unlock()
				if active {
					if v := m.views[tab]; v != nil {
						if uri, ok := v.certificate.proceed(token); ok {
							accepted = true
							m.call(tab, uri, 9)
						}
					}
				}
				m.shell.Eval(fmt.Sprintf("window.__wompratResolve(%d,%t)", request.ID, accepted))
			})
			return
		case "focusShell":
			m.dispatch(func() { C.womprat_gtk_focus(m.host) })
		case "browserBack", "browserForward", "browserReload":
			action := request.Name
			m.dispatch(func() {
				if v := m.app.activeContentView(); v != nil {
					switch action {
					case "browserBack":
						v.GoBack()
					case "browserForward":
						v.GoForward()
					default:
						v.Reload()
					}
				}
			})
		default:
			return
		}
		m.dispatch(func() { m.shell.Eval(fmt.Sprintf("window.__wompratResolve(%d,%s)", request.ID, jsString(result))) })
	}()
}

const linuxShellBridgeJS = `(function(){
let sequence=0;const pending=new Map();
window.__wompratResolve=function(id,value){const resolve=pending.get(id);if(resolve){pending.delete(id);resolve(value)}};
for(const name of ['shellReady','getTabs','getNetworkState','newBrowser','navigate','switchTab','closeTab','reorderTab','forgetTab','registerLocalTab','newTerminal','newVNC','newRDP','openSettings','goHome','clearActiveTab','updateTitle','setChromeHeight','focusShell','browserBack','browserForward','browserReload','proceedCertificate']){
window['womprat_'+name]=function(...args){return new Promise(resolve=>{const id=++sequence;pending.set(id,resolve);window.__wompratNativeSend(JSON.stringify({id,name,args}))})};
}
})();`

func runGUI(app *App, shellURL string) {
	if os.Getenv("WOMPRAT_HEADLESS") == "1" {
		runHeadlessServer(app, shellURL)
		return
	}
	stopProfile := profileLinuxGUI()
	defer stopProfile()
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	proxy, stop, err := app.startBrowserProxy()
	if err != nil {
		log.Fatalf("Linux browser proxy unavailable: %v", err)
	}
	defer stop()
	m := &linuxContentManager{app: app, views: make(map[string]*linuxContentView)}
	m.handle = cgo.NewHandle(m)
	defer m.handle.Delete()
	path := webviewDataPath()
	if err := os.MkdirAll(path, 0700); err != nil {
		log.Fatal(err)
	}
	bridge := strings.ReplaceAll(browserTitleReporterJS, "window.chrome.webview.postMessage", "window.webkit.messageHandlers.womprat.postMessage")
	a, b, c, d := C.CString(shellURL), C.CString(path), C.CString(proxy), C.CString(bridge)
	e := C.CString(linuxShellBridgeJS)
	m.host = C.womprat_gtk_new(C.uintptr_t(m.handle), a, b, c, d, e, C.int(0))
	for _, p := range []*C.char{a, b, c, d, e} {
		C.free(unsafe.Pointer(p))
	}
	if m.host == nil {
		log.Fatal("Cannot create GTK window: a desktop display is required")
	}
	m.shell = &linuxShell{m.host}
	app.webview = m.shell
	app.dispatch = m.dispatch
	app.contentViews = m
	app.clearLinuxBrowserData = func(operation int, domain string) error {
		result := make(chan string, 1)
		handle := cgo.NewHandle(result)
		m.dispatch(func() {
			text := C.CString(domain)
			defer C.free(unsafe.Pointer(text))
			C.womprat_gtk_data(m.host, C.int(operation), text, C.uintptr_t(handle))
		})
		select {
		case err := <-result:
			if err != "" {
				return fmt.Errorf("%s", err)
			}
			return nil
		case <-time.After(10 * time.Second):
			return fmt.Errorf("browser data deletion timed out")
		}
	}
	app.linuxCookieDomains = func() ([]CookieDomain, error) {
		result := make(chan string, 1)
		handle := cgo.NewHandle(result)
		m.dispatch(func() {
			text := C.CString("")
			defer C.free(unsafe.Pointer(text))
			C.womprat_gtk_data(m.host, 3, text, C.uintptr_t(handle))
		})
		select {
		case text := <-result:
			if strings.HasPrefix(text, "!") {
				return nil, fmt.Errorf("%s", text[1:])
			}
			counts := map[string]int{}
			for _, domain := range strings.Split(text, "\n") {
				if domain != "" {
					counts[domain]++
				}
			}
			out := make([]CookieDomain, 0, len(counts))
			for domain, count := range counts {
				out = append(out, CookieDomain{domain, count})
			}
			sort.Slice(out, func(i, j int) bool { return out[i].Domain < out[j].Domain })
			return out, nil
		case <-time.After(10 * time.Second):
			return nil, fmt.Errorf("cookie enumeration timed out")
		}
	}
	installLinuxGUIAcceptance(m)
	fmt.Printf("WOMPRAT_SHELL_URL=%s\nWOMPRAT_TOKEN=%s\n", shellURL, app.sessionToken)
	C.womprat_gtk_run(m.host)
	m.queueMu.Lock()
	m.closed = true
	m.queue = nil
	m.queueMu.Unlock()
	// Keep the closed dispatcher installed so late workers cannot execute GTK
	// operations synchronously after the host has been destroyed.
}
