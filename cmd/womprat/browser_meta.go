package main

import (
	"net/url"
	"strings"
	"unicode/utf8"
)

const (
	maxBrowserTitleRunes = 200
	maxFaviconURLBytes   = 2048
	maxWebViewMessage    = 64 * 1024
)

func sanitizeBrowserTitle(title string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return ""
	}
	runes := []rune(title)
	if len(runes) > maxBrowserTitleRunes {
		title = string(runes[:maxBrowserTitleRunes])
	}
	return strings.ToValidUTF8(title, "")
}

func validBrowserHotkeyAction(action string) bool {
	switch action {
	case "focusUrl", "newTab", "closeTab", "reload", "back", "forward", "nextTab", "prevTab", "tabAt":
		return true
	default:
		return false
	}
}

func sanitizeBrowserHotkeyArg(arg string) string {
	arg = strings.TrimSpace(arg)
	if len(arg) > 16 {
		arg = arg[:16]
	}
	return strings.ToValidUTF8(arg, "")
}

func sanitizeFaviconURL(favicon string) string {
	favicon = strings.TrimSpace(favicon)
	if favicon == "" || len(favicon) > maxFaviconURLBytes || !utf8.ValidString(favicon) {
		return ""
	}
	parsed, err := url.Parse(favicon)
	if err != nil || parsed.Scheme == "" {
		return ""
	}
	switch parsed.Scheme {
	case "http", "https":
		if parsed.Host == "" {
			return ""
		}
		return parsed.String()
	case "data":
		if len(favicon) <= 4096 {
			return favicon
		}
	}
	return ""
}
