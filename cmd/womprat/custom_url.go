package main

import (
	"fmt"
	"net"
	urlpkg "net/url"
	"strconv"
	"strings"
	"unicode"
)

type customURLTarget struct {
	Scheme string
	Host   string
	Port   int
	User   string
}

func (t customURLTarget) canonicalURL() string {
	host := t.Host
	if strings.Contains(host, ":") && net.ParseIP(host) != nil {
		host = "[" + host + "]"
	}
	userinfo := ""
	if t.User != "" {
		userinfo = urlpkg.User(t.User).String() + "@"
	}
	return fmt.Sprintf("%s://%s%s:%d", t.Scheme, userinfo, host, t.Port)
}

func customSchemeDefaultPort(scheme string) (int, bool) {
	switch strings.ToLower(scheme) {
	case "ssh":
		return 22, true
	case "vnc":
		return 5900, true
	case "rdp":
		return 3389, true
	default:
		return 0, false
	}
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
	defaultPort, ok := customSchemeDefaultPort(scheme)
	if !ok {
		return customURLTarget{}, fmt.Errorf("unsupported custom URL scheme %q", parsed.Scheme)
	}
	if parsed.Opaque != "" || parsed.Host == "" || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return customURLTarget{}, fmt.Errorf("invalid %s target %q", strings.ToUpper(scheme), raw)
	}
	if parsed.User != nil {
		if password, hasPassword := parsed.User.Password(); hasPassword && password != "" {
			return customURLTarget{}, fmt.Errorf("%s URL must not include a password", strings.ToUpper(scheme))
		}
	}

	host := strings.TrimSpace(parsed.Hostname())
	if err := validateCustomURLHost(scheme, host); err != nil {
		return customURLTarget{}, err
	}

	port := defaultPort
	if portText := parsed.Port(); portText != "" {
		p, err := strconv.Atoi(portText)
		if err != nil || p <= 0 || p > 65535 {
			return customURLTarget{}, fmt.Errorf("invalid %s port %q", strings.ToUpper(scheme), portText)
		}
		port = p
	} else if strings.Contains(parsed.Host, ":") && net.ParseIP(host) == nil {
		return customURLTarget{}, fmt.Errorf("invalid %s target %q", strings.ToUpper(scheme), raw)
	}

	user := ""
	if parsed.User != nil {
		user = parsed.User.Username()
	}
	if err := validateCustomURLUser(scheme, user); err != nil {
		return customURLTarget{}, err
	}
	return customURLTarget{Scheme: scheme, Host: host, Port: port, User: user}, nil
}

func validateCustomURLHost(scheme, host string) error {
	if host == "" {
		return fmt.Errorf("invalid %s host %q", strings.ToUpper(scheme), host)
	}
	if ip := net.ParseIP(host); ip != nil {
		return nil
	}
	if len(host) > 253 || strings.ContainsAny(host, " /?#\\@%") {
		return fmt.Errorf("invalid %s host %q", strings.ToUpper(scheme), host)
	}
	labels := strings.Split(host, ".")
	for _, label := range labels {
		if label == "" || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return fmt.Errorf("invalid %s host %q", strings.ToUpper(scheme), host)
		}
		for _, r := range label {
			if r > unicode.MaxASCII || !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_') {
				return fmt.Errorf("invalid %s host %q", strings.ToUpper(scheme), host)
			}
		}
	}
	return nil
}

func validateCustomURLUser(scheme, user string) error {
	if len(user) > 256 || strings.ContainsAny(user, " /?#\\@") {
		return fmt.Errorf("invalid %s user", strings.ToUpper(scheme))
	}
	return nil
}
