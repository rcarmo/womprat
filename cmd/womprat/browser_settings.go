package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
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
	a.mu.Lock()
	savePasswords := a.config.SavePasswords
	a.mu.Unlock()
	data := BrowserData{
		CacheSize:     getCacheSize(),
		SavePasswords: savePasswords,
		Cookies:       listCookieDomains(),
		Passwords:     listSavedPasswords(),
	}
	json.NewEncoder(w).Encode(data)
}

func (a *App) handleClearCache(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", 405)
		return
	}
	// WebView2 cache is in the DataPath directory
	cacheDir := filepath.Join(webviewDataPath(), "EBWebView", "Default", "Cache")
	if err := os.RemoveAll(cacheDir); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (a *App) handleClearCookies(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", 405)
		return
	}
	var body struct {
		Domain string `json:"domain"`
	}
	json.NewDecoder(r.Body).Decode(&body)

	if body.Domain != "" {
		// Clear cookies for specific domain
		deleteCookiesForDomain(body.Domain)
	} else {
		// Clear all cookies
		for _, cookieFile := range cookieDBPaths() {
			os.Remove(cookieFile)
		}
	}
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (a *App) handleClearPasswords(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", 405)
		return
	}
	var body struct {
		Site string `json:"site"`
	}
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
	if r.Method != "POST" {
		http.Error(w, "method not allowed", 405)
		return
	}
	// Nuclear option: remove entire WebView2 user data
	dataPath := webviewDataPath()
	os.RemoveAll(filepath.Join(dataPath, "EBWebView", "Default", "Cache"))
	os.Remove(filepath.Join(dataPath, "EBWebView", "Default", "Cookies"))
	clearAllSavedPasswords()
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (a *App) handleSavePasswordsToggle(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", 405)
		return
	}
	var body struct {
		Enabled bool `json:"enabled"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	a.mu.Lock()
	a.config.SavePasswords = body.Enabled
	cfg := a.config
	a.mu.Unlock()
	if err := SaveConfig(cfg); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
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
	cookieFile := firstExistingCookieDB()
	if cookieFile == "" {
		return []CookieDomain{}
	}
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(cookieFile)+"?mode=ro&immutable=1")
	if err != nil {
		return []CookieDomain{}
	}
	defer db.Close()
	rows, err := db.Query(`SELECT host_key, COUNT(*) FROM cookies GROUP BY host_key ORDER BY host_key`)
	if err != nil {
		return []CookieDomain{}
	}
	defer rows.Close()
	var out []CookieDomain
	for rows.Next() {
		var c CookieDomain
		if rows.Scan(&c.Domain, &c.Count) == nil {
			out = append(out, c)
		}
	}
	return out
}

func listSavedPasswords() []SavedPassword {
	// WebView2 stores passwords in its encrypted Login Data store. We can clear
	// that store, but we intentionally do not enumerate saved credentials.
	return []SavedPassword{}
}

func cookieDBPaths() []string {
	base := filepath.Join(webviewDataPath(), "EBWebView", "Default")
	return []string{
		filepath.Join(base, "Network", "Cookies"),
		filepath.Join(base, "Cookies"),
	}
}

func firstExistingCookieDB() string {
	for _, p := range cookieDBPaths() {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

func deleteCookiesForDomain(domain string) {
	if domain == "" {
		return
	}
	for _, cookieFile := range cookieDBPaths() {
		if _, err := os.Stat(cookieFile); err != nil {
			continue
		}
		db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(cookieFile)+"?mode=rw")
		if err != nil {
			continue
		}
		_, _ = db.Exec(`DELETE FROM cookies WHERE host_key = ? OR host_key = ? OR host_key LIKE ?`, domain, strings.TrimPrefix(domain, "."), "%"+strings.TrimPrefix(domain, "."))
		db.Close()
	}
}

func clearAllSavedPasswords() {
	base := filepath.Join(webviewDataPath(), "EBWebView", "Default")
	for _, name := range []string{"Login Data", "Login Data For Account"} {
		os.Remove(filepath.Join(base, name))
		os.Remove(filepath.Join(base, name+"-journal"))
		os.Remove(filepath.Join(base, name+"-wal"))
		os.Remove(filepath.Join(base, name+"-shm"))
	}
	os.RemoveAll(filepath.Join(configDir(), "creds", "browser-pw")) // legacy app-side placeholder store
}
