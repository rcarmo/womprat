package main

import (
	"context"
	"fmt"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"golang.org/x/net/dns/dnsmessage"
	"tailscale.com/client/local"
	"tailscale.com/types/dnstype"
)

// dnsProbeResult reports exactly what the embedded tsnet LocalAPI returned.
// Resolver candidates are chosen by Tailscale's active DNS configuration;
// they are not proof that every candidate answered a query.
type dnsProbeResult struct {
	Record     string   `json:"record"`
	Resolvers  []string `json:"resolvers"`
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

func (a *App) handleDNSDiagnostic(w http.ResponseWriter, r *http.Request) {
	if !requireGET(w, r) {
		return
	}
	// Fixed target only: this local authenticated endpoint is a bounded
	// diagnostic, not an arbitrary DNS-proxy API.
	const name = "smith.local"
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
		out.Queries = append(out.Queries, probeTailnetDNS(ctx, lc, name, record))
	}
	writeJSON(w, http.StatusOK, out)
}

// dnsQueryClient narrows the LocalAPI dependency for unit tests; production
// calls the very same LocalClient.QueryDNS used by outbound Womprat dials.
type dnsQueryClient interface {
	QueryDNS(context.Context, string, string) ([]byte, []*dnstype.Resolver, error)
}

func probeTailnetDNS(ctx context.Context, lc dnsQueryClient, name, record string) dnsProbeResult {
	result := dnsProbeResult{Record: record, Resolvers: []string{}, Addresses: []string{}}
	start := time.Now()
	packet, resolvers, err := lc.QueryDNS(ctx, name, record)
	for _, resolver := range resolvers {
		if resolver != nil {
			result.Resolvers = append(result.Resolvers, resolver.Addr)
		}
	}
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

var _ dnsQueryClient = (*local.Client)(nil)
