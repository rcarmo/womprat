package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"

	"nhooyr.io/websocket"
)

type vncTarget struct {
	Host string
	Port int
}

func parseVNCURL(raw string) (vncTarget, error) {
	text := strings.TrimSpace(raw)
	text = strings.TrimPrefix(text, "vnc://")
	if text == "" {
		return vncTarget{}, fmt.Errorf("missing VNC target")
	}
	host, portText, err := net.SplitHostPort(text)
	if err != nil {
		// Accept vnc://host as vnc://host:5900.
		if strings.Count(text, ":") == 0 {
			host = text
			portText = "5900"
		} else {
			return vncTarget{}, fmt.Errorf("invalid VNC target %q", raw)
		}
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port <= 0 || port > 65535 {
		return vncTarget{}, fmt.Errorf("invalid VNC port %q", portText)
	}
	host = strings.TrimSpace(strings.Trim(host, "[]"))
	if host == "" || strings.ContainsAny(host, " /?#\\") {
		return vncTarget{}, fmt.Errorf("invalid VNC host %q", host)
	}
	return vncTarget{Host: host, Port: port}, nil
}

func (a *App) handleVNCWebSocket(w http.ResponseWriter, r *http.Request) {
	target, err := parseVNCURL(r.URL.Query().Get("target"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	a.mu.Lock()
	ts := a.tsServer
	a.mu.Unlock()
	if ts == nil {
		http.Error(w, "tailscale not connected", http.StatusServiceUnavailable)
		return
	}

	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		log.Printf("vnc websocket accept: %v", err)
		return
	}
	defer ws.Close(websocket.StatusNormalClosure, "")

	addr := net.JoinHostPort(target.Host, strconv.Itoa(target.Port))
	log.Printf("vnc: connect %s via tsnet", addr)
	upstream, err := ts.Dial(r.Context(), "tcp", addr)
	if err != nil {
		log.Printf("vnc: connect failed %s: %v", addr, err)
		ws.Close(websocket.StatusInternalError, err.Error())
		return
	}
	defer upstream.Close()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	go func() {
		defer cancel()
		buf := make([]byte, 32*1024)
		for {
			n, err := upstream.Read(buf)
			if n > 0 {
				if werr := ws.Write(ctx, websocket.MessageBinary, buf[:n]); werr != nil {
					return
				}
			}
			if err != nil {
				if err != io.EOF {
					log.Printf("vnc: upstream read: %v", err)
				}
				return
			}
		}
	}()

	for {
		msgType, data, err := ws.Read(ctx)
		if err != nil {
			return
		}
		if msgType != websocket.MessageBinary {
			continue
		}
		if _, err := upstream.Write(data); err != nil {
			log.Printf("vnc: upstream write: %v", err)
			return
		}
	}
}
