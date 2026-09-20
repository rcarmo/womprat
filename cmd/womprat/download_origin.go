package main

import (
	"net/url"
	"strings"
)

func sameHTTPOrigin(source, target string) bool {
	sourceURL, sourceErr := url.Parse(source)
	targetURL, targetErr := url.Parse(target)
	if sourceErr != nil || targetErr != nil || sourceURL.User != nil || targetURL.User != nil {
		return false
	}
	if (sourceURL.Scheme != "http" && sourceURL.Scheme != "https") || sourceURL.Scheme != targetURL.Scheme {
		return false
	}
	port := func(parsed *url.URL) string {
		if parsed.Port() != "" {
			return parsed.Port()
		}
		if parsed.Scheme == "https" {
			return "443"
		}
		return "80"
	}
	return strings.EqualFold(sourceURL.Hostname(), targetURL.Hostname()) && port(sourceURL) == port(targetURL)
}
