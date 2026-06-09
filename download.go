package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

func (a *App) registerDownloadRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/download", a.authMiddleware(a.handleDownload))
	mux.HandleFunc("/api/download/status", a.authMiddleware(a.handleDownloadStatus))
}

type downloadState struct {
	Filename string `json:"filename"`
	Size     int64  `json:"size"`
	Done     int64  `json:"done"`
	Status   string `json:"status"` // "downloading", "complete", "error"
	Error    string `json:"error,omitempty"`
	Path     string `json:"path,omitempty"`
}

var (
	currentDownload *downloadState
	downloadMu      sync.Mutex
)

// handleDownload initiates a file download using the same routing policy as the browser.
func (a *App) handleDownload(w http.ResponseWriter, r *http.Request) {
	targetURL := r.URL.Query().Get("url")
	if targetURL == "" {
		httpError(w, 400, "Missing URL", "")
		return
	}

	parsed, err := url.Parse(targetURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		httpError(w, 400, "Invalid URL", fmt.Sprintf("%v", err))
		return
	}

	downloadsDir := getDownloadsDir()
	os.MkdirAll(downloadsDir, 0755)

	filename := filenameFromURL(parsed)
	savePath := uniqueDownloadPath(downloadsDir, filename)

	st := &downloadState{
		Filename: filepath.Base(savePath),
		Status:   "downloading",
		Path:     savePath,
	}
	downloadMu.Lock()
	currentDownload = st
	downloadMu.Unlock()

	go a.downloadToFile(parsed.String(), parsed.Hostname(), savePath, st)

	json.NewEncoder(w).Encode(map[string]string{
		"status":   "started",
		"filename": filepath.Base(savePath),
	})
}

func (a *App) downloadToFile(targetURL, host, savePath string, st *downloadState) {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			a.mu.Lock()
			ts := a.tsServer
			exitNode := useExitNode
			a.mu.Unlock()
			if shouldRouteViaTSNet(host, exitNode) {
				if ts == nil {
					return nil, fmt.Errorf("tailscale not connected")
				}
				return ts.Dial(ctx, network, addr)
			}
			var d net.Dialer
			return d.DialContext(ctx, network, addr)
		},
	}
	client := &http.Client{Transport: transport, Timeout: 5 * time.Minute}

	resp, err := client.Get(targetURL)
	if err != nil {
		setDownloadError(st, err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		setDownloadError(st, fmt.Errorf("HTTP %s", resp.Status))
		return
	}

	downloadMu.Lock()
	st.Size = resp.ContentLength
	downloadMu.Unlock()

	f, err := os.Create(savePath)
	if err != nil {
		setDownloadError(st, err)
		return
	}
	defer f.Close()

	buf := make([]byte, 32*1024)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				setDownloadError(st, werr)
				return
			}
			downloadMu.Lock()
			st.Done += int64(n)
			downloadMu.Unlock()
		}
		if err != nil {
			if err == io.EOF {
				downloadMu.Lock()
				st.Status = "complete"
				downloadMu.Unlock()
			} else {
				setDownloadError(st, err)
			}
			return
		}
	}
}

func setDownloadError(st *downloadState, err error) {
	downloadMu.Lock()
	defer downloadMu.Unlock()
	st.Status = "error"
	st.Error = err.Error()
}

func (a *App) handleDownloadStatus(w http.ResponseWriter, r *http.Request) {
	downloadMu.Lock()
	defer downloadMu.Unlock()
	if currentDownload == nil {
		json.NewEncoder(w).Encode(map[string]string{"status": "idle"})
		return
	}
	copy := *currentDownload
	json.NewEncoder(w).Encode(&copy)
}

func filenameFromURL(parsed *url.URL) string {
	filename := filepath.Base(parsed.Path)
	if filename == "" || filename == "/" || filename == "." {
		filename = "download"
	}
	// Basic sanitization for Windows path separators/control chars.
	filename = strings.Map(func(r rune) rune {
		switch r {
		case '<', '>', ':', '"', '/', '\\', '|', '?', '*':
			return '_'
		}
		if r < 32 {
			return '_'
		}
		return r
	}, filename)
	return filename
}

func uniqueDownloadPath(dir, filename string) string {
	savePath := filepath.Join(dir, filename)
	if _, err := os.Stat(savePath); err != nil {
		return savePath
	}
	ext := filepath.Ext(filename)
	base := strings.TrimSuffix(filename, ext)
	return filepath.Join(dir, fmt.Sprintf("%s_%d%s", base, time.Now().Unix(), ext))
}

func getDownloadsDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Downloads")
}
