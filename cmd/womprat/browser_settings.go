package main

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
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
	writeJSON(w, http.StatusOK, data)
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
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *App) handleClearCookies(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", 405)
		return
	}
	var body struct {
		Domain string `json:"domain"`
	}
	if !decodeOptionalSettingsJSON(w, r, &body) {
		return
	}
	body.Domain = normalizeBrowserDomain(body.Domain)
	if body.Domain == invalidBrowserSelector {
		http.Error(w, "invalid domain", 400)
		return
	}

	if body.Domain != "" {
		// Clear cookies for specific domain
		if err := deleteCookiesForDomain(body.Domain); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
	} else {
		// Clear all cookies
		if err := removeExistingFiles(cookieDBPaths()...); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *App) handleClearPasswords(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", 405)
		return
	}
	var body struct {
		Site string `json:"site"`
	}
	if !decodeOptionalSettingsJSON(w, r, &body) {
		return
	}
	body.Site = normalizeBrowserSite(body.Site)
	if body.Site == invalidBrowserSelector {
		http.Error(w, "invalid site", 400)
		return
	}

	if body.Site != "" {
		if err := DeleteCredential("browser-pw/" + body.Site); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
	} else {
		// Clear all saved passwords
		if err := clearAllSavedPasswords(); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *App) handleClearAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", 405)
		return
	}
	// Nuclear option: remove browser-local cache/cookies/password stores.
	if err := clearBrowserArtifacts(); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *App) handleSavePasswordsToggle(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", 405)
		return
	}
	var body struct {
		Enabled bool `json:"enabled"`
	}
	if !decodeSettingsJSON(w, r, &body) {
		return
	}
	a.mu.Lock()
	a.config.SavePasswords = body.Enabled
	cfg := a.config
	a.mu.Unlock()
	if err := SaveConfig(cfg); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Helpers

const invalidBrowserSelector = "\x00INVALID"

func normalizeBrowserDomain(domain string) string {
	domain = strings.TrimSpace(strings.ToLower(domain))
	if domain == "" {
		return ""
	}
	domain = strings.TrimPrefix(domain, "http://")
	domain = strings.TrimPrefix(domain, "https://")
	domain = strings.Trim(domain, "/")
	if domain == "" || len(domain) > 253 || strings.ContainsAny(domain, " /?#\\@%") {
		return invalidBrowserSelector
	}
	return domain
}

func normalizeBrowserSite(site string) string {
	site = strings.TrimSpace(site)
	if site == "" {
		return ""
	}
	if err := validateCredentialName("browser-pw/" + site); err != nil {
		return invalidBrowserSelector
	}
	return site
}

func webviewDataPath() string {
	return filepath.Join(configDir(), "webview2")
}

func getCacheSize() string {
	size, err := cacheSizeBytes(filepath.Join(webviewDataPath(), "EBWebView", "Default", "Cache"))
	if err != nil {
		log.Printf("cache size scan failed: %v", err)
	}
	return formatByteSize(size)
}

func cacheSizeBytes(cacheDir string) (int64, error) {
	var size int64
	err := filepath.WalkDir(cacheDir, func(_ string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry == nil || entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		size += info.Size()
		return nil
	})
	if os.IsNotExist(err) {
		return 0, nil
	}
	return size, err
}

func formatByteSize(size int64) string {
	if size < 0 {
		size = 0
	}
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
	if err := rows.Err(); err != nil {
		log.Printf("cookie domain rows error: %v", err)
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

func deleteCookiesForDomain(domain string) error {
	if domain == "" {
		return nil
	}
	var errs []error
	for _, cookieFile := range cookieDBPaths() {
		if _, err := os.Stat(cookieFile); err != nil {
			continue
		}
		db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(cookieFile)+"?mode=rw")
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if _, err := db.Exec(`DELETE FROM cookies WHERE host_key = ? OR host_key = ? OR host_key LIKE ?`, domain, strings.TrimPrefix(domain, "."), "%"+strings.TrimPrefix(domain, ".")); err != nil {
			errs = append(errs, err)
		}
		if err := db.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func clearBrowserArtifacts() error {
	dataPath := webviewDataPath()
	var errs []error
	if err := os.RemoveAll(filepath.Join(dataPath, "EBWebView", "Default", "Cache")); err != nil {
		errs = append(errs, err)
	}
	if err := removeExistingFiles(cookieDBPaths()...); err != nil {
		errs = append(errs, err)
	}
	if err := clearAllSavedPasswords(); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func clearAllSavedPasswords() error {
	base := filepath.Join(webviewDataPath(), "EBWebView", "Default")
	paths := []string{}
	for _, name := range []string{"Login Data", "Login Data For Account"} {
		paths = append(paths,
			filepath.Join(base, name),
			filepath.Join(base, name+"-journal"),
			filepath.Join(base, name+"-wal"),
			filepath.Join(base, name+"-shm"),
		)
	}
	paths = append(paths, filepath.Join(configDir(), "creds", "browser-pw")) // legacy app-side placeholder store
	return removeExistingPaths(paths...)
}

func removeExistingFiles(paths ...string) error {
	var errs []error
	for _, path := range paths {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			errs = append(errs, fmt.Errorf("remove %s: %w", path, err))
		}
	}
	return errors.Join(errs...)
}

func removeExistingPaths(paths ...string) error {
	var errs []error
	for _, path := range paths {
		if err := os.RemoveAll(path); err != nil && !os.IsNotExist(err) {
			errs = append(errs, fmt.Errorf("remove %s: %w", path, err))
		}
	}
	return errors.Join(errs...)
}
