package main

import (
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/net/idna"
)

const defaultSearchProvider = "duckduckgo"

func validSearchProvider(provider string) bool {
	switch provider {
	case "duckduckgo", "bing", "google":
		return true
	}
	return false
}
func normalizeSearchProvider(provider string) string {
	if validSearchProvider(provider) {
		return provider
	}
	return defaultSearchProvider
}

// resolveAddressInput is only for address-bar submissions. Links/session URLs
// still use normalizeBrowserURL and never silently become external searches.
func resolveAddressInput(input, provider string) (string, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", fmt.Errorf("enter an address or search")
	}
	if len(input) > 4096 {
		return "", fmt.Errorf("address or search is too long")
	}
	for _, r := range input {
		if unicode.IsControl(r) {
			return "", fmt.Errorf("address or search contains control characters")
		}
	}
	lower := strings.ToLower(input)
	for _, scheme := range []string{"ssh://", "vnc://", "rdp://"} {
		if strings.HasPrefix(lower, scheme) {
			if _, err := parseCustomURL(input); err != nil {
				return "", err
			}
			return input, nil
		}
	}
	if strings.HasPrefix(input, "/") && !strings.HasPrefix(input, "//") || strings.HasPrefix(input, `\\`) || len(input) > 2 && input[1] == ':' && (input[2] == '\\' || input[2] == '/') {
		return "", fmt.Errorf("local file paths cannot be opened in the address bar")
	}
	if schemeEnd := strings.Index(input, "://"); schemeEnd > 0 {
		scheme := input[:schemeEnd]
		valid := true
		for i, c := range scheme {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || i > 0 && (c >= '0' && c <= '9' || c == '+' || c == '-' || c == '.')) {
				valid = false
				break
			}
		}
		if valid {
			return normalizeAddressURL(input)
		}
	}
	// Never leak local file URLs, executable schemes or pasted credentials into
	// a search query. HTTP(S) without // is a malformed explicit address.
	for _, prefix := range []string{"javascript:", "data:", "file:", "vbscript:", "about:", "blob:", "mailto:", "http:", "https:", "ssh:", "vnc:", "rdp:"} {
		if strings.HasPrefix(lower, prefix) {
			return "", fmt.Errorf("unsupported or malformed address scheme")
		}
	}
	if !strings.ContainsFunc(input, unicode.IsSpace) {
		if ip, err := netip.ParseAddr(input); err == nil {
			if ip.Zone() != "" {
				return "", fmt.Errorf("scoped browser hosts are not supported")
			}
			if ip.Is6() {
				return "http://[" + ip.String() + "]", nil
			}
			return "http://" + ip.String(), nil
		}
		candidate := "http://" + strings.TrimPrefix(input, "//")
		u, err := url.Parse(candidate)
		if err == nil && u.User != nil {
			if _, password := u.User.Password(); password {
				return "", fmt.Errorf("credentials are not allowed in browser addresses")
			}
		}
		if err == nil && u.User == nil && u.Hostname() != "" {
			host := u.Hostname()
			_, ipErr := netip.ParseAddr(host)
			looksLikeAddress := ipErr == nil || strings.EqualFold(host, "localhost") || strings.Contains(host, ".") || u.Port() != "" || strings.Contains(input, "/")
			if looksLikeAddress {
				return normalizeAddressURL(candidate)
			}
		}
	}
	endpoint := "https://duckduckgo.com/"
	switch normalizeSearchProvider(provider) {
	case "bing":
		endpoint = "https://www.bing.com/search"
	case "google":
		endpoint = "https://www.google.com/search"
	}
	return endpoint + "?" + url.Values{"q": {input}}.Encode(), nil
}

func normalizeAddressURL(raw string) (string, error) {
	normalized, err := normalizeBrowserURL(raw)
	if err != nil {
		return "", err
	}
	u, err := url.Parse(normalized)
	if err != nil {
		return "", err
	}
	host := u.Hostname()
	if host == "" {
		return "", fmt.Errorf("missing hostname")
	}
	if strings.Contains(u.Host, "%") {
		return "", fmt.Errorf("scoped or escaped browser hosts are not supported")
	}
	if _, err := netip.ParseAddr(host); err != nil {
		ascii, err := idna.Lookup.ToASCII(host)
		if err != nil {
			return "", fmt.Errorf("invalid hostname")
		}
		ascii = strings.TrimSuffix(ascii, ".")
		if ascii == "" || len(ascii) > 253 {
			return "", fmt.Errorf("invalid hostname")
		}
		if port := u.Port(); port != "" {
			u.Host = net.JoinHostPort(ascii, port)
		} else {
			u.Host = ascii
		}
		for _, label := range strings.Split(ascii, ".") {
			if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
				return "", fmt.Errorf("invalid hostname")
			}
			for _, c := range label {
				if c != '-' && (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') {
					return "", fmt.Errorf("invalid hostname")
				}
			}
		}
	}
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return "", fmt.Errorf("invalid port")
		}
	}
	return u.String(), nil
}
