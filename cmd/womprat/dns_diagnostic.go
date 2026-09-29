package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

// dnsProbeResult records the app's tailnet-only upstream query. Resolver
// candidates come from Tailscale's active DNS configuration; only Server and
// PeerRoute describe the attempted query transport.
type dnsProbeResult struct {
	Record     string   `json:"record"`
	Resolvers  []string `json:"resolvers"`
	Server     string   `json:"server,omitempty"`
	Transport  string   `json:"transport,omitempty"`
	PeerRoute  string   `json:"peerRoute,omitempty"`
	RCode      string   `json:"rcode,omitempty"`
	Addresses  []string `json:"addresses"`
	Error      string   `json:"error,omitempty"`
	DurationMS int64    `json:"durationMs"`
}

type dnsDiagnosticResponse struct {
	Name           string           `json:"name"`
	BackendState   string           `json:"backendState,omitempty"`
	ConfiguredExit string           `json:"configuredExit,omitempty"`
	ExitNode       string           `json:"exitNode,omitempty"`
	ExitNodeOnline bool             `json:"exitNodeOnline"`
	AcceptDNS      bool             `json:"acceptDNS"`
	Queries        []dnsProbeResult `json:"queries"`
	Error          string           `json:"error,omitempty"`
}

func normalizeDiagnosticDNSName(input string) (string, error) {
	name := strings.TrimSpace(input)
	name = strings.TrimSuffix(name, ".")
	if name == "" || net.ParseIP(name) != nil {
		return "", fmt.Errorf("enter a DNS hostname, not an IP address")
	}
	if err := validateCustomURLHost("DNS", name); err != nil {
		return "", fmt.Errorf("enter a valid DNS hostname")
	}
	return strings.ToLower(name), nil
}

func (a *App) handleDNSDiagnostic(w http.ResponseWriter, r *http.Request) {
	if !requirePOST(w, r) {
		return
	}
	var input struct {
		Name string `json:"name"`
	}
	if !decodeSettingsJSON(w, r, &input) {
		return
	}
	name, err := normalizeDiagnosticDNSName(input.Name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	out := dnsDiagnosticResponse{Name: name, Queries: []dnsProbeResult{}}
	a.mu.Lock()
	ts := a.tsServer
	out.ConfiguredExit = a.config.ExitNode
	a.mu.Unlock()
	if ts == nil {
		out.Error = "Womprat tsnet is not connected; no DNS query ran"
		writeJSON(w, http.StatusOK, out)
		return
	}
	lc, err := ts.LocalClient()
	if err != nil {
		out.Error = fmt.Sprintf("tsnet LocalClient: %v", err)
		writeJSON(w, http.StatusOK, out)
		return
	}
	resolver, err := resolverForTSNet(ts)
	if err != nil {
		out.Error = fmt.Sprintf("tailnet-only DNS resolver: %v", err)
		writeJSON(w, http.StatusOK, out)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	status, err := lc.Status(ctx)
	if err != nil {
		out.Error = fmt.Sprintf("tsnet status: %v", err)
	} else if status != nil {
		out.BackendState = status.BackendState
		if status.ExitNodeStatus != nil {
			out.ExitNodeOnline = status.ExitNodeStatus.Online
			out.ExitNode = string(status.ExitNodeStatus.ID)
			for _, peer := range status.Peer {
				if peer.ID == status.ExitNodeStatus.ID {
					out.ExitNode = peer.HostName + " (" + strings.TrimSuffix(peer.DNSName, ".") + ")"
					break
				}
			}
		}
	}
	prefs, err := lc.GetPrefs(ctx)
	if err != nil {
		if out.Error != "" {
			out.Error += "; "
		}
		out.Error += fmt.Sprintf("tsnet prefs: %v", err)
	} else if prefs != nil {
		out.AcceptDNS = prefs.CorpDNS
		if out.ExitNode == "" && (prefs.RouteAll || prefs.ExitNodeID != "" || prefs.ExitNodeIP.IsValid()) {
			out.ExitNode = fmt.Sprintf("selected in prefs: ID=%s IP=%s; no active exit-node status", prefs.ExitNodeID, prefs.ExitNodeIP)
		}
	}
	for _, record := range []string{"A", "AAAA"} {
		if ctx.Err() != nil {
			out.Queries = append(out.Queries, dnsProbeResult{Record: record, Resolvers: []string{}, Addresses: []string{}, Error: ctx.Err().Error()})
			continue
		}
		out.Queries = append(out.Queries, probeTailnetDNS(ctx, resolver, name, record))
	}
	writeJSON(w, http.StatusOK, out)
}

// dnsQueryClient is the same fail-closed transport used for outbound dials.
type dnsQueryClient interface {
	Query(context.Context, string, string) ([]byte, []string, string, string, string, error)
}

func probeTailnetDNS(ctx context.Context, resolver dnsQueryClient, name, record string) dnsProbeResult {
	result := dnsProbeResult{Record: record, Resolvers: []string{}, Addresses: []string{}}
	start := time.Now()
	packet, candidates, server, transport, peerRoute, err := resolver.Query(ctx, name, record)
	result.Resolvers, result.Server, result.Transport, result.PeerRoute = candidates, server, transport, peerRoute
	if err != nil {
		result.Error = err.Error()
		result.DurationMS = time.Since(start).Milliseconds()
		return result
	}
	var msg dnsmessage.Message
	if err := msg.Unpack(packet); err != nil {
		result.Error = fmt.Sprintf("invalid DNS response: %v", err)
	} else {
		result.RCode = msg.Header.RCode.String()
		for _, answer := range msg.Answers {
			switch body := answer.Body.(type) {
			case *dnsmessage.AResource:
				if record == "A" {
					result.Addresses = append(result.Addresses, netip.AddrFrom4(body.A).String())
				}
			case *dnsmessage.AAAAResource:
				if record == "AAAA" {
					result.Addresses = append(result.Addresses, netip.AddrFrom16(body.AAAA).String())
				}
			}
		}
	}
	result.DurationMS = time.Since(start).Milliseconds()
	return result
}

var _ dnsQueryClient = tailnetResolver{}
