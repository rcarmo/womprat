package main

import (
	"context"
	"fmt"
	"net"
	"net/netip"

	"tailscale.com/tsnet"
	"tailscale.com/types/dnstype"
	"tailscale.com/util/dnsname"
)

// tailnetResolver uses Tailscale's selected DNS suffix route, but owns the
// transport. Unlike LocalClient.QueryDNS in Tailscale v1.82.5, it never opens
// a host-network UDP socket or invokes SystemDial for an upstream query.
type tailnetResolver struct {
	upstreams func(string) ([]*dnstype.Resolver, error)
	route     tailnetDNSRoute
	dial      tailnetDial
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
	return tailnetResolver{
		upstreams: func(name string) ([]*dnstype.Resolver, error) {
			fqdn, err := dnsname.ToFQDN(name)
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
		dial: ts.Dial,
	}, nil
}

func (r tailnetResolver) Query(ctx context.Context, name, record string) (packet []byte, candidates []string, server, transport, peerRoute string, err error) {
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
