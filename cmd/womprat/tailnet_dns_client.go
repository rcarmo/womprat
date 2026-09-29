package main

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"strings"

	"tailscale.com/tsnet"
	"tailscale.com/types/dnstype"
	"tailscale.com/util/dnsname"
	"tailscale.com/wgengine/netstack"
)

// tailnetResolver uses Tailscale's selected DNS suffix route, but owns the
// transport. Unlike LocalClient.QueryDNS in Tailscale v1.82.5, it never opens
// a host-network UDP socket or invokes SystemDial for an upstream query.
type tailnetResolver struct {
	upstreams func(string) ([]*dnstype.Resolver, error)
	route     tailnetDNSRoute
	dial      tailnetDial
	local     func(context.Context, string, string) ([]byte, bool, error)
}

func resolverForTSNet(ts *tsnet.Server) (tailnetResolver, error) {
	if ts == nil {
		return tailnetResolver{}, fmt.Errorf("tailscale not connected")
	}
	sys := ts.Sys()
	if sys == nil {
		return tailnetResolver{}, fmt.Errorf("tsnet system unavailable")
	}
	manager, ok := sys.DNSManager.GetOK()
	if !ok || manager == nil {
		return tailnetResolver{}, fmt.Errorf("tsnet DNS manager unavailable")
	}
	engine, ok := sys.Engine.GetOK()
	if !ok || engine == nil {
		return tailnetResolver{}, fmt.Errorf("tsnet engine unavailable")
	}
	stackHandle, ok := sys.Netstack.GetOK()
	if !ok {
		return tailnetResolver{}, fmt.Errorf("tsnet netstack unavailable")
	}
	stack, ok := stackHandle.(*netstack.Impl)
	if !ok || stack == nil {
		return tailnetResolver{}, fmt.Errorf("unsupported tsnet netstack")
	}
	lc, err := ts.LocalClient()
	if err != nil {
		return tailnetResolver{}, err
	}
	return tailnetResolver{
		local: func(ctx context.Context, name, record string) ([]byte, bool, error) {
			nm, err := readDNSNetmap(ctx, lc)
			if err != nil {
				return nil, false, err
			}
			return localTailnetDNSAnswer(nm, name, record)
		},
		upstreams: func(name string) ([]*dnstype.Resolver, error) {
			fqdn, err := dnsname.ToFQDN(strings.ToLower(name))
			if err != nil {
				return nil, err
			}
			return manager.Resolver().GetUpstreamResolvers(fqdn), nil
		},
		route: func(ip netip.Addr) (string, error) {
			peer, ok := engine.PeerForIP(ip)
			if !ok || !peer.Node.Valid() || peer.IsSelf {
				return "", fmt.Errorf("%s has no remote Tailscale peer/route", ip)
			}
			return fmt.Sprintf("%s via %s (%s)", ip, peer.Node.Name(), peer.Route), nil
		},
		// Dial the embedded stack itself, not ts.Dial/UserDial: UserDial can
		// fall back to a host socket if a route disappears after the check.
		dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			if network != "tcp" && network != "tcp4" && network != "tcp6" {
				return nil, fmt.Errorf("unsupported tailnet network %q", network)
			}
			target, err := netip.ParseAddrPort(address)
			if err != nil {
				return nil, err
			}
			peer, routed := engine.PeerForIP(target.Addr())
			if !routed || peer.IsSelf {
				return nil, fmt.Errorf("no remote Tailscale route to %s", target)
			}
			v4, v6 := ts.TailscaleIPs()
			source := v4
			if target.Addr().Is6() {
				source = v6
			}
			if !source.IsValid() {
				return nil, fmt.Errorf("no Tailscale source address for %s", target)
			}
			conn, err := stack.DialContextTCPWithBind(ctx, source, target)
			if err != nil {
				return nil, err
			}
			return conn, nil
		},
	}, nil
}

func (r tailnetResolver) Query(ctx context.Context, name, record string) (packet []byte, candidates []string, server, transport, peerRoute string, err error) {
	if r.local != nil {
		answer, known, localErr := r.local(ctx, name, record)
		if localErr != nil {
			return nil, nil, "", "", "", localErr
		}
		if known {
			return answer, []string{}, "Tailscale network map", "local MagicDNS record (no network query)", "", nil
		}
	}
	upstreams, err := r.upstreams(name)
	if err != nil {
		return nil, nil, "", "", "", err
	}
	candidates = []string{}
	for _, upstream := range upstreams {
		if upstream != nil {
			candidates = append(candidates, upstream.Addr)
		}
	}
	packet, server, transport, peerRoute, err = queryDNSOverTailnet(ctx, name, record, upstreams, r.route, r.dial)
	return
}

func (r tailnetResolver) dialAddress(ctx context.Context, network, address string) (net.Conn, error) {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return nil, fmt.Errorf("expected resolved IP: %w", err)
	}
	if _, err := r.route(ip); err != nil {
		return nil, fmt.Errorf("refusing host-network connection: %w", err)
	}
	return r.dial(ctx, network, address)
}
