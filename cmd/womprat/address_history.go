package main

import (
	"net/http"
	"strings"
)

const addressHistoryLimit = 100

func normalizeAddressHistory(inputs []string) []string {
	out := make([]string, 0, min(len(inputs), addressHistoryLimit))
	seen := make(map[string]bool)
	for _, input := range inputs {
		input = strings.TrimSpace(input)
		if input == "" || seen[input] {
			continue
		}
		if _, err := resolveAddressInput(input, defaultSearchProvider); err != nil {
			continue
		}
		seen[input] = true
		out = append(out, input)
		if len(out) >= addressHistoryLimit {
			break
		}
	}
	return out
}

func (a *App) handleAddressHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		a.mu.Lock()
		history := append([]string{}, a.config.AddressHistory...)
		a.mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{"history": history})
		return
	}
	if !requirePOST(w, r) {
		return
	}
	var body struct {
		Input string `json:"input"`
	}
	if !decodeSettingsJSON(w, r, &body) {
		return
	}
	input := strings.TrimSpace(body.Input)
	if _, err := resolveAddressInput(input, defaultSearchProvider); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	a.configSaveMu.Lock()
	defer a.configSaveMu.Unlock()
	a.mu.Lock()
	cfg := cloneConfig(a.config)
	a.mu.Unlock()
	cfg.AddressHistory = normalizeAddressHistory(append([]string{input}, cfg.AddressHistory...))
	if err := SaveConfig(cfg); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.mu.Lock()
	a.config.AddressHistory = cfg.AddressHistory
	a.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"history": cfg.AddressHistory})
}
