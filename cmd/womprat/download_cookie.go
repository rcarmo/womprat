package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"time"
)

const (
	downloadTicketLifetime = 30 * time.Second
	maxDownloadTickets     = 64
)

type downloadTicket struct {
	URL     string
	Cookies []*http.Cookie
	Expires time.Time
}

func (a *App) createDownloadTicket(targetURL string, cookies []*http.Cookie) (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	ticket := hex.EncodeToString(raw[:])
	now := time.Now()
	a.downloadTicketMu.Lock()
	if a.downloadTickets == nil {
		a.downloadTickets = make(map[string]downloadTicket)
	}
	for key, existing := range a.downloadTickets {
		if !existing.Expires.After(now) {
			delete(a.downloadTickets, key)
		}
	}
	if len(a.downloadTickets) >= maxDownloadTickets {
		a.downloadTicketMu.Unlock()
		return "", fmt.Errorf("too many pending download cookie tickets")
	}
	expires := now.Add(downloadTicketLifetime)
	a.downloadTickets[ticket] = downloadTicket{URL: targetURL, Cookies: cloneCookies(cookies), Expires: expires}
	a.downloadTicketMu.Unlock()
	time.AfterFunc(downloadTicketLifetime, func() {
		a.downloadTicketMu.Lock()
		if existing, ok := a.downloadTickets[ticket]; ok && existing.Expires.Equal(expires) {
			delete(a.downloadTickets, ticket)
		}
		a.downloadTicketMu.Unlock()
	})
	return ticket, nil
}

func (a *App) consumeDownloadTicket(ticket, targetURL string) ([]*http.Cookie, error) {
	if ticket == "" {
		return nil, nil
	}
	a.downloadTicketMu.Lock()
	entry, ok := a.downloadTickets[ticket]
	delete(a.downloadTickets, ticket)
	a.downloadTicketMu.Unlock()
	if !ok || !entry.Expires.After(time.Now()) {
		return nil, fmt.Errorf("download cookie ticket expired or invalid")
	}
	if entry.URL != targetURL {
		return nil, fmt.Errorf("download cookie ticket URL mismatch")
	}
	return cloneCookies(entry.Cookies), nil
}

func cloneCookies(cookies []*http.Cookie) []*http.Cookie {
	out := make([]*http.Cookie, 0, len(cookies))
	for _, cookie := range cookies {
		if cookie == nil {
			continue
		}
		copy := *cookie
		out = append(out, &copy)
	}
	return out
}
