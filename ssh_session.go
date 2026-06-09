package main

// TODO: Implement WebSocket-based PTY relay for SSH sessions.
//
// Flow:
// 1. Client opens WebSocket to /api/ssh/ws?tab=<tabId>
// 2. Server looks up the ssh.Client for that tab
// 3. Opens a session, requests PTY, starts shell
// 4. Bidirectional copy: WebSocket <-> SSH session stdin/stdout
// 5. Handle resize messages (JSON: {"type":"resize","cols":80,"rows":24})
//
// Libraries needed:
//   - nhooyr.io/websocket (or gorilla/websocket)
//   - The ssh.Session already supports RequestPty and Shell
//
// Example:
//   session, _ := client.NewSession()
//   session.RequestPty("xterm-256color", 24, 80, ssh.TerminalModes{})
//   session.Shell()
//   // pipe session.StdinPipe/StdoutPipe <-> websocket

import (
	"io"

	"golang.org/x/crypto/ssh"
	// "nhooyr.io/websocket"
)

// SSHSession wraps an active SSH session with its PTY
type SSHSession struct {
	client  *ssh.Client
	session *ssh.Session
	stdin   io.WriteCloser
	stdout  io.Reader
	cols    int
	rows    int
}

// NewSSHSession creates a new SSH session with PTY
func NewSSHSession(client *ssh.Client, cols, rows int) (*SSHSession, error) {
	session, err := client.NewSession()
	if err != nil {
		return nil, err
	}

	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 115200,
		ssh.TTY_OP_OSPEED: 115200,
	}

	if err := session.RequestPty("xterm-256color", rows, cols, modes); err != nil {
		session.Close()
		return nil, err
	}

	stdin, err := session.StdinPipe()
	if err != nil {
		session.Close()
		return nil, err
	}

	stdout, err := session.StdoutPipe()
	if err != nil {
		session.Close()
		return nil, err
	}

	if err := session.Shell(); err != nil {
		session.Close()
		return nil, err
	}

	return &SSHSession{
		client:  client,
		session: session,
		stdin:   stdin,
		stdout:  stdout,
		cols:    cols,
		rows:    rows,
	}, nil
}

func (s *SSHSession) Resize(cols, rows int) error {
	s.cols = cols
	s.rows = rows
	return s.session.WindowChange(rows, cols)
}

func (s *SSHSession) Close() {
	s.session.Close()
}

// handleSSHWebSocketImpl is the real WebSocket handler
// Uncomment and wire up once nhooyr.io/websocket is added to go.mod
