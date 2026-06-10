package main

import (
	"fmt"
	"net"
	urlpkg "net/url"
	"strconv"
	"strings"
)

type customURLTarget struct {
	Scheme string
	Host   string
	Port   int
	User   string
}

func parseCustomURL(raw string) (customURLTarget, error) {
	text := strings.TrimSpace(raw)
	if text == "" {
		return customURLTarget{}, fmt.Errorf("missing target")
	}
	if !strings.Contains(text, "://") {
		return customURLTarget{}, fmt.Errorf("missing custom URL scheme")
	}
	parsed, err := urlpkg.Parse(text)
	if err != nil {
		return customURLTarget{}, fmt.Errorf("invalid custom URL %q: %w", raw, err)
	}
	scheme := strings.ToLower(parsed.Scheme)
	defaultPort := 0
	switch scheme {
	case "ssh":
		defaultPort = 22
	case "vnc":
		defaultPort = 5900
	case "rdp":
		defaultPort = 3389
	default:
		return customURLTarget{}, fmt.Errorf("unsupported custom URL scheme %q", parsed.Scheme)
	}
	if parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return customURLTarget{}, fmt.Errorf("invalid %s target %q", strings.ToUpper(scheme), raw)
	}
	host := strings.TrimSpace(strings.Trim(parsed.Hostname(), "[]"))
	if host == "" || strings.ContainsAny(host, " /?#\\") {
		return customURLTarget{}, fmt.Errorf("invalid %s host %q", strings.ToUpper(scheme), host)
	}
	port := defaultPort
	if portText := parsed.Port(); portText != "" {
		p, err := strconv.Atoi(portText)
		if err != nil || p <= 0 || p > 65535 {
			return customURLTarget{}, fmt.Errorf("invalid %s port %q", strings.ToUpper(scheme), portText)
		}
		port = p
	} else if strings.Contains(parsed.Host, ":") && net.ParseIP(host) == nil && strings.Contains(host, ":") {
		return customURLTarget{}, fmt.Errorf("invalid %s target %q", strings.ToUpper(scheme), raw)
	}
	user := ""
	if parsed.User != nil {
		user = strings.TrimSuffix(parsed.User.Username(), ":")
	}
	if len(user) > 256 {
		return customURLTarget{}, fmt.Errorf("invalid %s user", strings.ToUpper(scheme))
	}
	return customURLTarget{Scheme: scheme, Host: host, Port: port, User: user}, nil
}
