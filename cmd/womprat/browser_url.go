package main

import (
	"fmt"
	"net/url"
	"strings"
)

func normalizeBrowserURL(raw string) (string, error) {
	text := strings.TrimSpace(raw)
	if text == "" {
		return "", fmt.Errorf("empty browser URL")
	}
	if !strings.Contains(text, "://") {
		text = "http://" + text
	}
	parsed, err := url.Parse(text)
	if err != nil {
		return "", err
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("unsupported browser URL scheme %q", parsed.Scheme)
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("missing browser URL host")
	}
	return parsed.String(), nil
}

func isBrowserURL(raw string) bool {
	_, err := normalizeBrowserURL(raw)
	return err == nil
}
