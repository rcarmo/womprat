package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
)

type BrowserData struct {
	CacheSize     string          `json:"cacheSize"`
	SavePasswords bool            `json:"savePasswords"`
	Cookies       []CookieDomain  `json:"cookies"`
	Passwords     []SavedPassword `json:"passwords"`
}

type CookieDomain struct {
	Domain string `json:"domain"`
	Count  int    `json:"count"`
}

type SavedPassword struct {
	Site     string `json:"site"`
	Username string `json:"username"`
}

func (a *App) registerBrowserRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/settings/browser", a.authMiddleware(a.handleBrowserData))
	mux.HandleFunc("/api/settings/browser/clear-cache", a.authMiddleware(a.handleClearCache))
	mux.HandleFunc("/api/settings/browser/clear-cookies", a.authMiddleware(a.handleClearCookies))
	mux.HandleFunc("/api/settings/browser/clear-passwords", a.authMiddleware(a.handleClearPasswords))
	mux.HandleFunc("/api/settings/browser/clear-all", a.authMiddleware(a.handleClearAll))
	mux.HandleFunc("/api/settings/browser/save-passwords", a.authMiddleware(a.handleSavePasswordsToggle))
}

func (a *App) handleBrowserData(w http.ResponseWriter, r *http.Request) {
	data := BrowserData{
		CacheSize:     getCacheSize(),
		SavePasswords: a.config.SavePasswords,
		Cookies:       listCookieDomains(),
		Passwords:     listSavedPasswords(),
	}
	json.NewEncoder(w).Encode(data)
}

func (a *App) handleClearCache(w http.ResponseWriter, r *http.Request) {
	// WebView2 cache is in the DataPath directory
	cacheDir := filepath.Join(webviewDataPath(), "EBWebView", "Default", "Cache")
	os.RemoveAll(cacheDir)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (a *App) handleClearCookies(w http.ResponseWriter, r *http.Request) {
	var body struct{ Domain string `json:"domain"` }
	json.NewDecoder(r.Body).Decode(&body)
	
	if body.Domain != "" {
		// Clear cookies for specific domain
		deleteCookiesForDomain(body.Domain)
	} else {
		// Clear all cookies
		cookieFile := filepath.Join(webviewDataPath(), "EBWebView", "Default", "Cookies")
		os.Remove(cookieFile)
	}
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (a *App) handleClearPasswords(w http.ResponseWriter, r *http.Request) {
	var body struct{ Site string `json:"site"` }
	json.NewDecoder(r.Body).Decode(&body)

	if body.Site != "" {
		DeleteCredential("browser-pw/" + body.Site)
	} else {
		// Clear all saved passwords
		clearAllSavedPasswords()
	}
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (a *App) handleClearAll(w http.ResponseWriter, r *http.Request) {
	// Nuclear option: remove entire WebView2 user data
	dataPath := webviewDataPath()
	os.RemoveAll(filepath.Join(dataPath, "EBWebView", "Default", "Cache"))
	os.Remove(filepath.Join(dataPath, "EBWebView", "Default", "Cookies"))
	clearAllSavedPasswords()
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (a *App) handleSavePasswordsToggle(w http.ResponseWriter, r *http.Request) {
	var body struct{ Enabled bool `json:"enabled"` }
	json.NewDecoder(r.Body).Decode(&body)
	a.config.SavePasswords = body.Enabled
	SaveConfig(a.config)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// Helpers

func webviewDataPath() string {
	return filepath.Join(configDir(), "webview2")
}

func getCacheSize() string {
	cacheDir := filepath.Join(webviewDataPath(), "EBWebView", "Default", "Cache")
	var size int64
	filepath.Walk(cacheDir, func(_ string, info os.FileInfo, _ error) error {
		if info != nil && !info.IsDir() {
			size += info.Size()
		}
		return nil
	})
	if size < 1024 {
		return fmt.Sprintf("%d B", size)
	} else if size < 1024*1024 {
		return fmt.Sprintf("%.1f KB", float64(size)/1024)
	}
	return fmt.Sprintf("%.1f MB", float64(size)/(1024*1024))
}

func listCookieDomains() []CookieDomain {
	// WebView2 stores cookies in a SQLite DB — we'd need to parse it
	// For now return empty; full implementation needs sqlite3
	return []CookieDomain{}
}

func listSavedPasswords() []SavedPassword {
	// List credentials with prefix "browser-pw/"
	pwDir := filepath.Join(configDir(), "creds")
	entries, err := os.ReadDir(pwDir)
	if err != nil {
		return []SavedPassword{}
	}
	var passwords []SavedPassword
	for _, e := range entries {
		if len(e.Name()) > 11 && e.Name()[:10] == "browser-pw" {
			site := e.Name()[11:] // strip "browser-pw/"
			passwords = append(passwords, SavedPassword{Site: site, Username: "(saved)"})
		}
	}
	return passwords
}

func deleteCookiesForDomain(domain string) {
	// Would need sqlite3 to selectively delete from Cookies DB
	// Placeholder
	_ = domain
}

func clearAllSavedPasswords() {
	pwDir := filepath.Join(configDir(), "creds")
	entries, _ := os.ReadDir(pwDir)
	for _, e := range entries {
		if len(e.Name()) > 10 && e.Name()[:10] == "browser-pw" {
			os.Remove(filepath.Join(pwDir, e.Name()))
		}
	}
}
