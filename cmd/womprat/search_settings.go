package main

import "net/http"

func (a *App) handleSearchProvider(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		a.mu.Lock()
		provider := normalizeSearchProvider(a.config.SearchProvider)
		a.mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]string{"searchProvider": provider})
		return
	}
	if !requirePOST(w, r) {
		return
	}
	var body struct {
		SearchProvider string `json:"searchProvider"`
	}
	if !decodeSettingsJSON(w, r, &body) {
		return
	}
	if !validSearchProvider(body.SearchProvider) {
		http.Error(w, "searchProvider must be duckduckgo, bing or google", http.StatusBadRequest)
		return
	}
	a.configSaveMu.Lock()
	defer a.configSaveMu.Unlock()
	a.mu.Lock()
	cfg := cloneConfig(a.config)
	a.mu.Unlock()
	cfg.SearchProvider = body.SearchProvider
	if err := SaveConfig(cfg); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.mu.Lock()
	a.config.SearchProvider = body.SearchProvider
	a.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "searchProvider": body.SearchProvider})
}

// Only classifies input. It never performs network queries or navigates a tab.
func (a *App) handleResolveAddress(w http.ResponseWriter, r *http.Request) {
	if !requirePOST(w, r) {
		return
	}
	var body struct {
		Input string `json:"input"`
	}
	if !decodeSettingsJSON(w, r, &body) {
		return
	}
	a.mu.Lock()
	provider := normalizeSearchProvider(a.config.SearchProvider)
	a.mu.Unlock()
	target, err := resolveAddressInput(body.Input, provider)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"url": target})
}
