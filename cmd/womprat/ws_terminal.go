package main

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"

	"golang.org/x/crypto/ssh"
	"nhooyr.io/websocket"
)

// termSession tracks an active terminal WebSocket session
type termSession struct {
	sshSession *ssh.Session
	stdin      io.WriteCloser
	stdout     io.Reader
	cols, rows int
}

const (
	minTerminalCols = 20
	maxTerminalCols = 500
	minTerminalRows = 5
	maxTerminalRows = 200
)

func parseTerminalDimension(raw string, fallback, min, max int) int {
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	clamped := clampTerminalDimension(value, min, max)
	if clamped == 0 {
		return fallback
	}
	return clamped
}

func clampTerminalDimension(value, min, max int) int {
	if value <= 0 || min <= 0 || max < min {
		return 0
	}
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

// handleSSHWebSocketFull is the production WebSocket PTY handler
func (a *App) handleSSHWebSocketFull(w http.ResponseWriter, r *http.Request) {
	tabID := r.URL.Query().Get("tab")
	if !validTabID(tabID) {
		http.Error(w, "invalid tab param", http.StatusBadRequest)
		return
	}

	a.mu.Lock()
	client := a.sshConns[tabID]
	a.mu.Unlock()
	if client == nil {
		http.Error(w, "no SSH connection for tab", 404)
		return
	}

	// Accept WebSocket
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		log.Printf("websocket accept: %v", err)
		return
	}
	defer conn.Close(websocket.StatusNormalClosure, "")
	defer func() {
		a.mu.Lock()
		delete(a.sshConns, tabID)
		a.mu.Unlock()
		client.Close()
	}()

	ctx := r.Context()

	// Create SSH session with PTY
	session, err := client.NewSession()
	if err != nil {
		conn.Close(websocket.StatusInternalError, err.Error())
		return
	}
	defer session.Close()

	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 115200,
		ssh.TTY_OP_OSPEED: 115200,
	}
	// Get initial size from query params, bounded to avoid invalid or excessive PTY requests.
	cols := parseTerminalDimension(r.URL.Query().Get("cols"), 80, minTerminalCols, maxTerminalCols)
	rows := parseTerminalDimension(r.URL.Query().Get("rows"), 24, minTerminalRows, maxTerminalRows)
	if err := session.RequestPty("xterm-256color", rows, cols, modes); err != nil {
		conn.Close(websocket.StatusInternalError, err.Error())
		return
	}

	stdin, err := session.StdinPipe()
	if err != nil {
		conn.Close(websocket.StatusInternalError, err.Error())
		return
	}

	stdout, err := session.StdoutPipe()
	if err != nil {
		conn.Close(websocket.StatusInternalError, err.Error())
		return
	}

	stderr, err := session.StderrPipe()
	if err != nil {
		conn.Close(websocket.StatusInternalError, err.Error())
		return
	}

	if err := session.Shell(); err != nil {
		// Send error message to terminal before closing
		errMsg := "\r\n\x1b[31mShell failed: " + err.Error() + "\x1b[0m\r\n"
		conn.Write(ctx, websocket.MessageBinary, []byte(errMsg))
		conn.Close(websocket.StatusInternalError, err.Error())
		return
	}

	var wg sync.WaitGroup
	var wsMu sync.Mutex
	writeWS := func(msgType websocket.MessageType, data []byte) error {
		wsMu.Lock()
		defer wsMu.Unlock()
		return conn.Write(ctx, msgType, data)
	}

	// SSH stdout → WebSocket
	wg.Add(1)
	go func() {
		defer wg.Done()
		buf := make([]byte, 4096)
		for {
			n, err := stdout.Read(buf)
			if n > 0 {
				if writeErr := writeWS(websocket.MessageBinary, buf[:n]); writeErr != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	// SSH stderr → WebSocket
	wg.Add(1)
	go func() {
		defer wg.Done()
		buf := make([]byte, 4096)
		for {
			n, err := stderr.Read(buf)
			if n > 0 {
				if writeErr := writeWS(websocket.MessageBinary, buf[:n]); writeErr != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	// WebSocket → SSH stdin (handle resize messages too)
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer stdin.Close()
		for {
			msgType, data, err := conn.Read(ctx)
			if err != nil {
				return
			}
			if msgType == websocket.MessageText {
				// Check for control messages (resize)
				var msg struct {
					Type string `json:"type"`
					Cols int    `json:"cols"`
					Rows int    `json:"rows"`
				}
				if json.Unmarshal(data, &msg) == nil && msg.Type == "resize" {
					cols := clampTerminalDimension(msg.Cols, minTerminalCols, maxTerminalCols)
					rows := clampTerminalDimension(msg.Rows, minTerminalRows, maxTerminalRows)
					if cols > 0 && rows > 0 {
						if err := session.WindowChange(rows, cols); err != nil {
							log.Printf("ssh resize failed for tab %s: %v", tabID, err)
						}
					}
					continue
				}
			}
			// Regular terminal input
			if _, err := stdin.Write(data); err != nil {
				return
			}
		}
	}()

	// Wait for session to end
	session.Wait()
	wg.Wait()
}

// getSSHAuthMethods returns auth methods for connecting to a host
func (a *App) getSSHAuthMethods(host string) []ssh.AuthMethod {
	var signers []ssh.Signer
	seen := map[string]bool{}
	addSigner := func(name string) {
		name, err := safeSSHKeyName(name)
		if err != nil {
			log.Printf("ssh key %q skipped: %v", name, err)
			return
		}
		keyData, err := GetCredential("ssh-key/" + name)
		if err != nil {
			log.Printf("ssh key %q unavailable: %v", name, err)
			return
		}
		signer, err := ssh.ParsePrivateKey([]byte(keyData))
		if err != nil {
			log.Printf("ssh key %q parse failed: %v", name, err)
			return
		}
		fingerprint := ssh.FingerprintSHA256(signer.PublicKey())
		if seen[fingerprint] {
			return
		}
		seen[fingerprint] = true
		signers = append(signers, signer)
	}

	// Try host-specific key first.
	a.mu.Lock()
	hostConf := a.config.Hosts[host]
	a.mu.Unlock()
	if hostConf.KeyName != "" {
		addSigner(hostConf.KeyName)
	}

	// Try all stored keys as fallback. Keys are stored under creds/ssh-key/<name>.
	keyDir := filepath.Join(configDir(), "creds", "ssh-key")
	entries, err := os.ReadDir(keyDir)
	if err != nil && !os.IsNotExist(err) {
		log.Printf("ssh key directory read failed: %v", err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if hostConf.KeyName != "" && name == hostConf.KeyName {
			continue // already tried
		}
		addSigner(name)
	}

	if len(signers) > 0 {
		return []ssh.AuthMethod{ssh.PublicKeys(signers...)}
	}
	return []ssh.AuthMethod{}
}
