package main

import (
	"encoding/json"
	"net/url"
	"strings"
)

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
  function send(){ try{ window.chrome.webview.postMessage(JSON.stringify({wompratTitle: document.title || location.hostname || location.href, wompratFavicon: favicon(), wompratURL: location.href})); }catch(e){ reportBridgeError('metadata post', e); } }
  document.addEventListener('DOMContentLoaded', send);
  window.addEventListener('load', send);
  // Same-document navigations (hash/fragment changes and SPA pushState/popstate)
  // do not raise WebView2 NavigationCompleted, so report the live URL here to
  // keep the shell address bar in sync.
  window.addEventListener('hashchange', send);
  window.addEventListener('popstate', send);
  try {
    var _ps = history.pushState, _rs = history.replaceState;
    history.pushState = function(){ var r = _ps.apply(this, arguments); try { send(); } catch(e){ reportBridgeError('pushState metadata', e); } return r; };
    history.replaceState = function(){ var r = _rs.apply(this, arguments); try { send(); } catch(e){ reportBridgeError('replaceState metadata', e); } return r; };
  } catch(e){ reportBridgeError('history patch', e); }
  // document.title can replace a text node or mutate its characterData;
  // watch both, including a <title> element inserted after this bridge starts.
  try { new MutationObserver(send).observe(document.head||document.documentElement,{subtree:true,childList:true,characterData:true}); } catch(e){ reportBridgeError('title observer', e); }
  function fire(action, arg){ try{ window.chrome.webview.postMessage(JSON.stringify({wompratKey: action, wompratArg: arg||''})); }catch(e){ reportBridgeError('hotkey post', e); } }
  function browserAction(action, url){
    try {
      var u = new URL(String(url || ''), location.href);
      if (u.protocol !== 'http:' && u.protocol !== 'https:') return false;
      window.chrome.webview.postMessage(JSON.stringify({wompratBrowserAction: action, wompratURL: u.href}));
      return true;
    } catch(e){ reportBridgeError('browser action', e); return false; }
  }
  document.addEventListener('click', function(e){
    var a = e.target && e.target.closest ? e.target.closest('a[href]') : null;
    if (!a) return;
    if (a.hasAttribute('download')) {
      if (browserAction('download', a.href)) e.preventDefault();
    } else if ((a.target || '').toLowerCase() === '_blank') {
      if (browserAction('open', a.href)) e.preventDefault();
    }
  }, true);
  try {
    var nativeOpen = window.open;
    window.open = function(url, target, features){
      if ((!target || target === '_blank') && browserAction('open', url)) return null;
      return nativeOpen.call(window, url, target, features);
    };
  } catch(e){ reportBridgeError('window.open patch', e); }
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

// parseURLMessage extracts the live document URL reported by the in-page bridge
// (used for same-document navigations like fragment/hash changes). Only http(s)
// URLs are accepted so nothing else can be injected into the address bar.
func parseURLMessage(raw string) string {
	if len(raw) > maxWebViewMessage {
		return ""
	}
	var m struct {
		WompratURL string `json:"wompratURL"`
	}
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return ""
	}
	url := strings.TrimSpace(m.WompratURL)
	if len(url) > 4096 {
		return ""
	}
	lower := strings.ToLower(url)
	if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
		return ""
	}
	return url
}

func parseBrowserActionMessage(raw string) (string, string) {
	if len(raw) > maxWebViewMessage {
		return "", ""
	}
	var m struct {
		Action string `json:"wompratBrowserAction"`
		URL    string `json:"wompratURL"`
	}
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return "", ""
	}
	if m.Action != "open" && m.Action != "download" {
		return "", ""
	}
	parsed, err := url.Parse(strings.TrimSpace(m.URL))
	if err != nil || parsed.Host == "" || parsed.User != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", ""
	}
	if m.Action == "download" && parsed.Fragment != "" {
		return "", ""
	}
	if len(parsed.String()) > 4096 {
		return "", ""
	}
	return m.Action, parsed.String()
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
