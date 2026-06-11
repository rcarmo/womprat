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

const maxVNCWebSocketMessageBytes = 1 << 20

func parseVNCURL(raw string) (vncTarget, error) {
	text := strings.TrimSpace(raw)
	if text != "" && !strings.Contains(text, "://") {
		text = "vnc://" + text
	}
	target, err := parseCustomURL(text)
	if err != nil {
		return vncTarget{}, err
	}
	if target.Scheme != "vnc" {
		return vncTarget{}, fmt.Errorf("invalid VNC target %q", raw)
	}
	return vncTarget{Host: target.Host, Port: target.Port}, nil
}

func (a *App) handleVNCWebSocket(w http.ResponseWriter, r *http.Request) {
	if !requireGET(w, r) {
		return
	}
	target, err := parseVNCURL(r.URL.Query().Get("target"))
	if err != nil {
		log.Printf("vnc: invalid target %q: %v", r.URL.Query().Get("target"), err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	log.Printf("vnc: ws request target=%s:%d", target.Host, target.Port)
	a.mu.Lock()
	ts := a.tsServer
	a.mu.Unlock()
	if ts == nil && !allowDirectDial {
		http.Error(w, "tailscale not connected", http.StatusServiceUnavailable)
		return
	}

	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		log.Printf("vnc websocket accept: %v", err)
		return
	}
	defer ws.Close(websocket.StatusNormalClosure, "")
	ws.SetReadLimit(maxVNCWebSocketMessageBytes)

	addr := net.JoinHostPort(target.Host, strconv.Itoa(target.Port))
	log.Printf("vnc: connect %s via tsnet", addr)
	upstream, err := dialTSNetPreferIPv4(r.Context(), ts, addr)
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
			log.Printf("vnc: ignoring non-binary websocket message type %v", msgType)
			continue
		}
		if len(data) == 0 {
			continue
		}
		if err := writeFull(upstream, data); err != nil {
			log.Printf("vnc: upstream write: %v", err)
			return
		}
	}
}
