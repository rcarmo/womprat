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
	"time"
)

func (a *App) registerDownloadRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/download", a.authMiddleware(a.handleDownload))
	mux.HandleFunc("/api/download/status", a.authMiddleware(a.handleDownloadStatus))
}

type downloadState struct {
	Filename string  `json:"filename"`
	Size     int64   `json:"size"`
	Done     int64   `json:"done"`
	Status   string  `json:"status"` // "downloading", "complete", "error"
	Error    string  `json:"error,omitempty"`
	Path     string  `json:"path,omitempty"`
}

var currentDownload *downloadState

// handleDownload initiates a file download through tsnet
func (a *App) handleDownload(w http.ResponseWriter, r *http.Request) {
	targetURL := r.URL.Query().Get("url")
	if targetURL == "" {
		httpError(w, 400, "Missing URL", "")
		return
	}

	parsed, err := url.Parse(targetURL)
	if err != nil {
		httpError(w, 400, "Invalid URL", err.Error())
		return
	}

	if a.tsServer == nil {
		httpError(w, 503, "Tailscale not connected", "")
		return
	}

	// Determine save path — use user's Downloads folder
	downloadsDir := getDownloadsDir()
	os.MkdirAll(downloadsDir, 0755)

	// Fetch the file through tsnet
	host := parsed.Hostname()
	port := parsed.Port()
	if port == "" {
		if parsed.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}

	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return a.tsServer.Dial(ctx, "tcp", host+":"+port)
		},
	}
	client := &http.Client{Transport: transport, Timeout: 5 * time.Minute}

	resp, err := client.Get(targetURL)
	if err != nil {
		httpError(w, 502, "Download failed", err.Error())
		return
	}
	defer resp.Body.Close()

	// Determine filename
	filename := ""
	if cd := resp.Header.Get("Content-Disposition"); cd != "" {
		if idx := strings.Index(cd, "filename="); idx >= 0 {
			filename = strings.Trim(cd[idx+9:], "\" ")
		}
	}
	if filename == "" {
		filename = filepath.Base(parsed.Path)
	}
	if filename == "" || filename == "/" || filename == "." {
		filename = "download"
	}

	savePath := filepath.Join(downloadsDir, filename)
	// Avoid overwriting
	if _, err := os.Stat(savePath); err == nil {
		ext := filepath.Ext(filename)
		base := strings.TrimSuffix(filename, ext)
		savePath = filepath.Join(downloadsDir, fmt.Sprintf("%s_%d%s", base, time.Now().Unix(), ext))
	}

	// Start download in background
	currentDownload = &downloadState{
		Filename: filepath.Base(savePath),
		Size:     resp.ContentLength,
		Status:   "downloading",
		Path:     savePath,
	}

	go func() {
		f, err := os.Create(savePath)
		if err != nil {
			currentDownload.Status = "error"
			currentDownload.Error = err.Error()
			return
		}
		defer f.Close()

		buf := make([]byte, 32*1024)
		for {
			n, err := resp.Body.Read(buf)
			if n > 0 {
				f.Write(buf[:n])
				currentDownload.Done += int64(n)
			}
			if err != nil {
				if err == io.EOF {
					currentDownload.Status = "complete"
				} else {
					currentDownload.Status = "error"
					currentDownload.Error = err.Error()
				}
				return
			}
		}
	}()

	json.NewEncoder(w).Encode(map[string]string{
		"status":   "started",
		"filename": filepath.Base(savePath),
	})
}

func (a *App) handleDownloadStatus(w http.ResponseWriter, r *http.Request) {
	if currentDownload == nil {
		json.NewEncoder(w).Encode(map[string]string{"status": "idle"})
		return
	}
	json.NewEncoder(w).Encode(currentDownload)
}

func getDownloadsDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Downloads")
}
