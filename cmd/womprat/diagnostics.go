package main

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

type diagnosticCheck struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	Detail     string `json:"detail,omitempty"`
	DurationMS int64  `json:"durationMs"`
}

type diagnosticsResponse struct {
	GeneratedAt time.Time         `json:"generatedAt"`
	Checks      []diagnosticCheck `json:"checks"`
}

func timedDiagnostic(name string, fn func(context.Context) (string, error), timeout time.Duration) diagnosticCheck {
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	detail, err := fn(ctx)
	status := "ok"
	if err != nil {
		status = "error"
		detail = err.Error()
	}
	return diagnosticCheck{Name: name, Status: status, Detail: detail, DurationMS: time.Since(start).Milliseconds()}
}

func (a *App) handleDiagnostics(w http.ResponseWriter, r *http.Request) {
	if !requireGET(w, r) {
		return
	}
	checks := []diagnosticCheck{
		timedDiagnostic("Tailscale connectivity", a.diagnoseTailscale, 5*time.Second),
		timedDiagnostic("SOCKS listener", diagnoseSOCKSListener, 2*time.Second),
		timedDiagnostic("SOCKS DNS/connect to google.com", diagnoseSOCKSDomainConnect, 8*time.Second),
	}
	writeJSON(w, http.StatusOK, diagnosticsResponse{GeneratedAt: time.Now(), Checks: checks})
}

func (a *App) diagnoseTailscale(ctx context.Context) (string, error) {
	a.mu.Lock()
	ts := a.tsServer
	a.mu.Unlock()
	if ts == nil {
		if allowDirectDial {
			return "debug direct-dial mode active; tsnet server is not required", nil
		}
		return "", fmt.Errorf("tailscale not connected")
	}
	lc, err := ts.LocalClient()
	if err != nil {
		return "", err
	}
	status, err := lc.Status(ctx)
	if err != nil {
		return "", err
	}
	if status == nil || status.Self == nil {
		return "", fmt.Errorf("tailscale status has no self node")
	}
	ip := ""
	if len(status.TailscaleIPs) > 0 {
		ip = status.TailscaleIPs[0].String()
	}
	online := 0
	for _, peer := range status.Peer {
		if peer.Online {
			online++
		}
	}
	return fmt.Sprintf("%s %s; %d peers online", status.Self.DNSName, ip, online), nil
}

func diagnoseSOCKSListener(ctx context.Context) (string, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", socksAddr)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	return "listening on " + socksAddr, nil
}

func diagnoseSOCKSDomainConnect(ctx context.Context) (string, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", socksAddr)
	if err != nil {
		return "", fmt.Errorf("connect to SOCKS listener: %w", err)
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	if _, err := conn.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		return "", fmt.Errorf("write SOCKS greeting: %w", err)
	}
	var greet [2]byte
	if _, err := io.ReadFull(conn, greet[:]); err != nil {
		return "", fmt.Errorf("read SOCKS greeting: %w", err)
	}
	if greet[0] != 0x05 || greet[1] != 0x00 {
		return "", fmt.Errorf("SOCKS no-auth greeting rejected: %02x %02x", greet[0], greet[1])
	}
	host := []byte("google.com")
	request := []byte{0x05, 0x01, 0x00, 0x03, byte(len(host))}
	request = append(request, host...)
	port := make([]byte, 2)
	binary.BigEndian.PutUint16(port, 80)
	request = append(request, port...)
	if _, err := conn.Write(request); err != nil {
		return "", fmt.Errorf("write SOCKS CONNECT google.com:80: %w", err)
	}
	var reply [10]byte
	if _, err := io.ReadFull(conn, reply[:]); err != nil {
		return "", fmt.Errorf("read SOCKS CONNECT reply: %w", err)
	}
	if reply[0] != 0x05 || reply[1] != 0x00 {
		return "", fmt.Errorf("SOCKS CONNECT google.com:80 failed with reply 0x%02x", reply[1])
	}
	return "domain CONNECT google.com:80 succeeded through SOCKS", nil
}
