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

	parsed, err := parseDownloadURL(targetURL)
	if err != nil {
		httpError(w, 400, "Invalid URL", err.Error())
		return
	}

	downloadsDir := getDownloadsDir()
	if err := os.MkdirAll(downloadsDir, 0755); err != nil {
		httpError(w, 500, "Download directory unavailable", err.Error())
		return
	}

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

	go a.downloadToFile(parsed.String(), savePath, st)

	json.NewEncoder(w).Encode(map[string]string{
		"status":   "started",
		"filename": filepath.Base(savePath),
	})
}

func (a *App) downloadToFile(targetURL, savePath string, st *downloadState) {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			a.mu.Lock()
			ts := a.tsServer
			a.mu.Unlock()
			if ts == nil {
				return nil, fmt.Errorf("tailscale not connected")
			}
			return ts.Dial(ctx, network, addr)
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

func parseDownloadURL(raw string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, err
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("unsupported scheme %q", parsed.Scheme)
	}
	if parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
		return nil, fmt.Errorf("download URL must include only http(s) scheme, host, path, and query")
	}
	return parsed, nil
}

func filenameFromURL(parsed *url.URL) string {
	filename := filepath.Base(parsed.EscapedPath())
	if unescaped, err := url.PathUnescape(filename); err == nil {
		filename = unescaped
	}
	return sanitizeDownloadFilename(filename)
}

func sanitizeDownloadFilename(filename string) string {
	filename = strings.TrimSpace(filename)
	if filename == "" || filename == "/" || filename == "." || filename == ".." {
		filename = "download"
	}
	filename = strings.Map(func(r rune) rune {
		switch r {
		case '<', '>', ':', '"', '/', '\\', '|', '?', '*':
			return '_'
		}
		if r < 32 || r == 127 {
			return '_'
		}
		return r
	}, filename)
	filename = strings.Trim(filename, " .")
	if filename == "" {
		filename = "download"
	}
	reserved := map[string]bool{"CON": true, "PRN": true, "AUX": true, "NUL": true, "COM1": true, "COM2": true, "COM3": true, "COM4": true, "COM5": true, "COM6": true, "COM7": true, "COM8": true, "COM9": true, "LPT1": true, "LPT2": true, "LPT3": true, "LPT4": true, "LPT5": true, "LPT6": true, "LPT7": true, "LPT8": true, "LPT9": true}
	base := strings.ToUpper(strings.TrimSuffix(filename, filepath.Ext(filename)))
	if reserved[base] {
		filename = "_" + filename
	}
	if len(filename) > 180 {
		ext := filepath.Ext(filename)
		base := strings.TrimSuffix(filename, ext)
		if len(ext) > 32 {
			ext = ext[:32]
		}
		maxBase := 180 - len(ext)
		if maxBase < 1 {
			maxBase = 1
		}
		if len(base) > maxBase {
			base = base[:maxBase]
		}
		filename = base + ext
	}
	return filename
}

func uniqueDownloadPath(dir, filename string) string {
	filename = sanitizeDownloadFilename(filename)
	savePath := filepath.Join(dir, filename)
	if _, err := os.Stat(savePath); os.IsNotExist(err) {
		return savePath
	}
	ext := filepath.Ext(filename)
	base := strings.TrimSuffix(filename, ext)
	for i := 1; i < 10000; i++ {
		candidate := filepath.Join(dir, fmt.Sprintf("%s_%d%s", base, i, ext))
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
	}
	return filepath.Join(dir, fmt.Sprintf("%s_%d%s", base, time.Now().UnixNano(), ext))
}

func getDownloadsDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Downloads")
}
