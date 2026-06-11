package main

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/rcarmo/go-rdp/pkg/protocol/audio"
	"github.com/rcarmo/go-rdp/pkg/protocol/pdu"
	"github.com/rcarmo/go-rdp/pkg/rdp"
	"nhooyr.io/websocket"
)

type rdpTarget struct {
	Host string
	Port int
	User string
}

type rdpCredentials struct {
	Type     string `json:"type"`
	Host     string `json:"host"`
	User     string `json:"user"`
	Password string `json:"password"`
}

type rdpResizeRequest struct {
	Type   string `json:"type"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

const maxRDPWebSocketMessageBytes = 1 << 20

func parseRDPURL(raw string) (rdpTarget, error) {
	text := strings.TrimSpace(raw)
	if text != "" && !strings.Contains(text, "://") {
		text = "rdp://" + text
	}
	target, err := parseCustomURL(text)
	if err != nil {
		return rdpTarget{}, err
	}
	if target.Scheme != "rdp" {
		return rdpTarget{}, fmt.Errorf("invalid RDP target %q", raw)
	}
	return rdpTarget{Host: target.Host, Port: target.Port, User: target.User}, nil
}

func (a *App) handleRDPWebSocket(w http.ResponseWriter, r *http.Request) {
	if !requireGET(w, r) {
		return
	}
	queryTarget, err := parseRDPURL(r.URL.Query().Get("target"))
	if err != nil {
		log.Printf("rdp: invalid target %q: %v", r.URL.Query().Get("target"), err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	log.Printf("rdp: ws request target=%s:%d user=%s", queryTarget.Host, queryTarget.Port, queryTarget.User)
	a.mu.Lock()
	ts := a.tsServer
	a.mu.Unlock()
	if ts == nil && !allowDirectDial {
		http.Error(w, "tailscale not connected", http.StatusServiceUnavailable)
		return
	}

	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		log.Printf("rdp websocket accept: %v", err)
		return
	}
	defer ws.Close(websocket.StatusNormalClosure, "")
	ws.SetReadLimit(maxRDPWebSocketMessageBytes)

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	_, data, err := ws.Read(ctx)
	if err != nil {
		return
	}
	if len(data) > maxRDPWebSocketMessageBytes {
		sendRDPError(ctx, ws, "credentials message too large")
		return
	}
	var creds rdpCredentials
	if err := json.Unmarshal(data, &creds); err != nil || creds.Type != "credentials" {
		sendRDPError(ctx, ws, "expected credentials")
		return
	}
	if !rdpCredentialHostMatches(creds.Host, queryTarget) {
		sendRDPError(ctx, ws, "credential host does not match target")
		return
	}
	if creds.Host == "" {
		creds.Host = net.JoinHostPort(queryTarget.Host, strconv.Itoa(queryTarget.Port))
	}
	if creds.User == "" {
		creds.User = queryTarget.User
	}
	creds.User = strings.TrimSpace(creds.User)
	if creds.User == "" || len(creds.User) > 256 || len(creds.Password) > 1024 {
		sendRDPError(ctx, ws, "invalid credentials")
		return
	}

	width := clampRDPDim(r.URL.Query().Get("width"), 1280)
	height := clampRDPDim(r.URL.Query().Get("height"), 720)
	colorDepth := parseRDPColorDepth(r.URL.Query().Get("colorDepth"), 16)
	// Keep the server-side connection conservative by default. The bundled
	// legacy browser client currently hard-codes audio=true and rfx=true in its
	// WebSocket URL, but several real servers (xrdp/FreeRDP shadow) abort during
	// setup when those virtual channels/codecs are advertised before a basic
	// bitmap session is proven. Treat them as explicit backend opt-ins instead.
	enableAudio := r.URL.Query().Get("audio") == "force"
	disableNLA := r.URL.Query().Get("disableNLA") == "true"
	enableRFX := r.URL.Query().Get("rfx") == "force"
	enableDisplayControl := r.URL.Query().Get("displayControl") == "true"

	dial := func(ctx context.Context, network, address string) (net.Conn, error) {
		// Ignore any client-supplied host in credentials; fail closed via tsnet to URL target.
		addr := net.JoinHostPort(queryTarget.Host, strconv.Itoa(queryTarget.Port))
		log.Printf("rdp: connect %s via tsnet", addr)
		return dialTSNetPreferIPv4(ctx, ts, addr)
	}
	client, err := rdp.NewClientWithDialContext(ctx, dial, net.JoinHostPort(queryTarget.Host, strconv.Itoa(queryTarget.Port)), creds.User, creds.Password, width, height, colorDepth)
	if err != nil {
		log.Printf("rdp init: %v", err)
		sendRDPError(ctx, ws, "connection failed")
		return
	}
	defer client.Close()
	client.SetTLSConfig(true, "")
	client.SetUseNLA(!disableNLA)
	client.SetEnableRFX(enableRFX)
	if enableDisplayControl {
		client.EnableDisplayControl()
	}
	if enableAudio {
		client.EnableAudio()
	}
	if err := client.Connect(); err != nil {
		log.Printf("rdp connect: %v", err)
		sendRDPError(ctx, ws, "connection failed")
		return
	}

	var mu sync.Mutex
	sendRDPCapabilities(ctx, ws, &mu, client)
	if enableAudio && client.GetAudioHandler() != nil {
		client.GetAudioHandler().SetCallback(func(data []byte, format *audio.AudioFormat, timestamp uint16) {
			sendRDPAudio(ctx, ws, &mu, data, format, timestamp)
		})
	}

	go rdpWsToClient(ctx, cancel, ws, client)
	rdpClientToWs(ctx, cancel, ws, &mu, client)
}

func clampRDPDim(raw string, fallback int) int {
	v, err := strconv.Atoi(raw)
	if err != nil || v <= 0 || v > 8192 {
		return fallback
	}
	return v
}

func rdpCredentialHostMatches(raw string, target rdpTarget) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return true
	}
	if !strings.Contains(raw, "://") {
		raw = "rdp://" + raw
	}
	parsed, err := parseRDPURL(raw)
	if err != nil {
		return false
	}
	return strings.EqualFold(parsed.Host, target.Host) && parsed.Port == target.Port
}

func parseRDPColorDepth(raw string, fallback int) int {
	if cd, err := strconv.Atoi(raw); err == nil && (cd == 8 || cd == 15 || cd == 16 || cd == 24 || cd == 32) {
		return cd
	}
	return fallback
}

func sendRDPError(ctx context.Context, ws *websocket.Conn, message string) {
	body, err := json.Marshal(map[string]string{"type": "error", "message": message})
	if err == nil {
		err = ws.Write(ctx, websocket.MessageText, body)
	}
	if err != nil {
		log.Printf("rdp error send failed: %v", err)
	}
}

func rdpWsToClient(ctx context.Context, cancel context.CancelFunc, ws *websocket.Conn, client *rdp.Client) {
	defer cancel()
	for {
		msgType, data, err := ws.Read(ctx)
		if err != nil {
			return
		}
		if msgType == websocket.MessageText {
			var req rdpResizeRequest
			if err := json.Unmarshal(data, &req); err != nil {
				log.Printf("rdp: ignoring malformed text control message: %v", err)
				continue
			}
			if req.Type != "resize" {
				log.Printf("rdp: ignoring unknown text control message %q", req.Type)
				continue
			}
			width := clampRDPDim(strconv.Itoa(req.Width), 0)
			height := clampRDPDim(strconv.Itoa(req.Height), 0)
			if client.IsDisplayControlReady() && width > 0 && height > 0 {
				if err := client.RequestResize(width, height); err != nil {
					log.Printf("rdp resize failed: %v", err)
				}
			}
			continue
		}
		if msgType == websocket.MessageBinary {
			if err := client.SendInputEvent(data); err != nil {
				return
			}
		}
	}
}

func rdpClientToWs(ctx context.Context, cancel context.CancelFunc, ws *websocket.Conn, mu *sync.Mutex, client *rdp.Client) {
	defer cancel()
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		update, err := client.GetUpdate()
		switch {
		case err == nil:
		case errors.Is(err, pdu.ErrDeactivateAll), errors.Is(err, io.EOF):
			return
		default:
			log.Printf("rdp update: %v", err)
			return
		}
		if len(update.Data) == 0 {
			continue
		}
		mu.Lock()
		err = ws.Write(ctx, websocket.MessageBinary, update.Data)
		mu.Unlock()
		if err != nil {
			return
		}
	}
}

func sendRDPCapabilities(ctx context.Context, ws *websocket.Conn, mu *sync.Mutex, client *rdp.Client) {
	caps := client.GetServerCapabilities()
	if caps == nil {
		return
	}
	payload, err := json.Marshal(map[string]any{
		"type":                "capabilities",
		"codecs":              caps.BitmapCodecs,
		"surfaceCommands":     caps.SurfaceCommands,
		"colorDepth":          caps.ColorDepth,
		"desktopSize":         caps.DesktopSize,
		"multifragmentSize":   caps.MultifragmentSize,
		"largePointer":        caps.LargePointer,
		"frameAcknowledge":    caps.FrameAcknowledge,
		"useNLA":              caps.UseNLA,
		"audioEnabled":        caps.AudioEnabled,
		"channels":            caps.Channels,
		"displayControlReady": client.IsDisplayControlReady(),
		"wasmRequiredCodecs":  []string{"RemoteFX", "RemoteFX-Image", "NSCodec"},
	})
	if err != nil {
		return
	}
	msg := append([]byte{0xff}, payload...)
	mu.Lock()
	err = ws.Write(ctx, websocket.MessageBinary, msg)
	mu.Unlock()
	if err != nil {
		log.Printf("rdp capabilities send failed: %v", err)
	}
}

func sendRDPAudio(ctx context.Context, ws *websocket.Conn, mu *sync.Mutex, data []byte, format *audio.AudioFormat, timestamp uint16) {
	if len(data) == 0 {
		return
	}
	var formatInfo []byte
	if format != nil {
		formatInfo = make([]byte, 10)
		binary.LittleEndian.PutUint16(formatInfo[0:2], format.Channels)
		binary.LittleEndian.PutUint32(formatInfo[2:6], format.SamplesPerSec)
		binary.LittleEndian.PutUint16(formatInfo[6:8], format.BitsPerSample)
		binary.LittleEndian.PutUint16(formatInfo[8:10], format.FormatTag)
	}
	msg := make([]byte, 4+len(formatInfo)+len(data))
	msg[0] = 0xfe
	msg[1] = 0x01
	binary.LittleEndian.PutUint16(msg[2:4], timestamp)
	off := 4
	if len(formatInfo) > 0 {
		msg[1] = 0x02
		copy(msg[off:], formatInfo)
		off += len(formatInfo)
	}
	copy(msg[off:], data)
	mu.Lock()
	err := ws.Write(ctx, websocket.MessageBinary, msg)
	mu.Unlock()
	if err != nil {
		log.Printf("rdp audio send failed: %v", err)
	}
}
