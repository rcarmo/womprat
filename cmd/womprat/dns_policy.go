package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"tailscale.com/client/local"
	"tailscale.com/ipn"
	"tailscale.com/tailcfg"
	"tailscale.com/types/netmap"
)

// Read-only LocalAPI snapshot. Never return/log the full network map (which
// includes unrelated peer and control-plane data). Private keys are redacted.
func readDNSNetmap(ctx context.Context, lc *local.Client) (*netmap.NetworkMap, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	watch, err := lc.WatchIPNBus(ctx, ipn.NotifyInitialNetMap|ipn.NotifyNoPrivateKeys)
	if err != nil {
		return nil, err
	}
	defer watch.Close()
	note, err := watch.Next()
	if err != nil {
		return nil, err
	}
	if note.NetMap == nil {
		return nil, fmt.Errorf("no authenticated network map available")
	}
	return note.NetMap, nil
}

type dnsPolicyResolver struct {
	Address         string `json:"address"`
	UseWithExitNode bool   `json:"useWithExitNode"`
}
type dnsPolicy struct {
	MatchingSuffix string              `json:"matchingSuffix,omitempty"`
	Resolvers      []dnsPolicyResolver `json:"resolvers"`
	Explanation    string              `json:"explanation"`
	Error          string              `json:"error,omitempty"`
}

func describeDNSPolicy(cfg tailcfg.DNSConfig, name string, exitActive bool) dnsPolicy {
	out := dnsPolicy{Resolvers: []dnsPolicyResolver{}}
	name = strings.ToLower(strings.TrimSuffix(name, "."))
	var best string
	matched := false
	for suffix := range cfg.Routes {
		key := strings.ToLower(strings.TrimSuffix(suffix, "."))
		if (key == "" || name == key || strings.HasSuffix(name, "."+key)) && (!matched || len(key) > len(strings.TrimSuffix(best, "."))) {
			best = suffix
			matched = true
		}
	}
	resolvers := cfg.Resolvers
	if matched {
		out.MatchingSuffix = best
		resolvers = cfg.Routes[best]
	}
	eligible := 0
	for _, r := range resolvers {
		if r != nil {
			out.Resolvers = append(out.Resolvers, dnsPolicyResolver{r.Addr, r.UseWithExitNode})
			if !exitActive || r.UseWithExitNode {
				eligible++
			}
		}
	}
	switch {
	case matched && len(resolvers) == 0:
		out.Explanation = "Authoritative Tailscale DNS record zone; no upstream resolver."
	case matched && exitActive && eligible == 0:
		out.Explanation = "Matching split-DNS nameserver is not enabled for use with exit nodes. Tailscale selects exit-node/global DNS instead. No policy was changed."
	case matched:
		out.Explanation = "Matching split-DNS rule supplied by the tailnet. The query result shows the upstream selected by Tailscale."
	case exitActive && eligible == 0:
		out.Explanation = "No matching split-DNS rule or eligible global nameserver; the selected exit node supplies DNS."
	default:
		out.Explanation = "No matching split-DNS rule; Tailscale selects a global/default resolver."
	}
	return out
}
