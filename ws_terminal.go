package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
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

// handleSSHWebSocketFull is the production WebSocket PTY handler
func (a *App) handleSSHWebSocketFull(w http.ResponseWriter, r *http.Request) {
	tabID := r.URL.Query().Get("tab")
	if tabID == "" {
		http.Error(w, "missing tab param", 400)
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
	if err := session.RequestPty("xterm-256color", 24, 80, modes); err != nil {
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
		conn.Close(websocket.StatusInternalError, err.Error())
		return
	}

	var wg sync.WaitGroup

	// SSH stdout → WebSocket
	wg.Add(1)
	go func() {
		defer wg.Done()
		buf := make([]byte, 4096)
		for {
			n, err := stdout.Read(buf)
			if n > 0 {
				if writeErr := conn.Write(ctx, websocket.MessageBinary, buf[:n]); writeErr != nil {
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
				conn.Write(ctx, websocket.MessageBinary, buf[:n])
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
					session.WindowChange(msg.Rows, msg.Cols)
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
	methods := []ssh.AuthMethod{}

	// Try host-specific key first
	hostConf := a.config.Hosts[host]
	if hostConf.KeyName != "" {
		if keyData, err := GetCredential("ssh-key/" + hostConf.KeyName); err == nil {
			if signer, err := ssh.ParsePrivateKey([]byte(keyData)); err == nil {
				methods = append(methods, ssh.PublicKeys(signer))
			}
		}
	}

	// Try all stored keys as fallback
	// TODO: enumerate all ssh-key/* credentials
	_ = fmt.Sprintf("trying keys for %s", host)

	return methods
}
