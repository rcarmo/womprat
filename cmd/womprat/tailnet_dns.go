package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"

	"golang.org/x/net/dns/dnsmessage"
)

// tailnetDNSQuery is backed by tsnet's LocalClient.QueryDNS, which uses the
// embedded Tailscale DNS manager (including custom nameservers and split DNS).
type tailnetDNSQuery func(context.Context, string, string) ([]byte, error)

type tailnetDial func(context.Context, string, string) (net.Conn, error)

// dialTailnetResolved never lets tsnet resolve an unknown hostname with the
// host OS resolver. Once Tailscale DNS has selected an address, tsnet still
// decides how to route the connection (peer, subnet route, or exit node).
func dialTailnetResolved(ctx context.Context, addr string, query tailnetDNSQuery, dial tailnetDial) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	if _, err := netip.ParseAddr(host); err == nil {
		return dialTailnetAddresses(ctx, []string{"tcp4", "tcp6", "tcp"}, addr, dial)
	}
	if host == "" {
		return nil, fmt.Errorf("empty target hostname")
	}
	if _, err := strconv.ParseUint(port, 10, 16); err != nil || port == "0" {
		return nil, fmt.Errorf("invalid target port %q", port)
	}

	var lastErr error
	for _, family := range []struct{ record, network string }{{"A", "tcp4"}, {"AAAA", "tcp6"}} {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		packet, err := query(ctx, host, family.record)
		if err != nil {
			lastErr = errors.Join(lastErr, fmt.Errorf("tailnet DNS %s %s: %w", family.record, host, err))
			continue
		}
		ips, err := tailnetDNSAddresses(packet, family.record)
		if err != nil {
			lastErr = errors.Join(lastErr, fmt.Errorf("tailnet DNS %s %s: %w", family.record, host, err))
			continue
		}
		for _, ip := range ips {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			conn, err := dial(ctx, family.network, net.JoinHostPort(ip.String(), port))
			if err == nil {
				return conn, nil
			}
			lastErr = errors.Join(lastErr, fmt.Errorf("dial %s: %w", ip, err))
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no A or AAAA address for %s in tailnet DNS", host)
	}
	return nil, lastErr
}

func dialTailnetAddresses(ctx context.Context, networks []string, addr string, dial tailnetDial) (net.Conn, error) {
	var lastErr error
	for _, network := range networks {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		conn, err := dial(ctx, network, addr)
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

func tailnetDNSAddresses(packet []byte, record string) ([]netip.Addr, error) {
	var parser dnsmessage.Parser
	header, err := parser.Start(packet)
	if err != nil {
		return nil, err
	}
	if !header.Response || header.RCode != dnsmessage.RCodeSuccess {
		return nil, fmt.Errorf("DNS response code %s", header.RCode)
	}
	if err := parser.SkipAllQuestions(); err != nil {
		return nil, err
	}
	var ips []netip.Addr
	for {
		answer, err := parser.Answer()
		if errors.Is(err, dnsmessage.ErrSectionDone) {
			return ips, nil
		}
		if err != nil {
			return nil, err
		}
		switch body := answer.Body.(type) {
		case *dnsmessage.AResource:
			if record == "A" {
				ips = append(ips, netip.AddrFrom4(body.A))
			}
		case *dnsmessage.AAAAResource:
			if record == "AAAA" {
				ips = append(ips, netip.AddrFrom16(body.AAAA))
			}
		}
	}
}
