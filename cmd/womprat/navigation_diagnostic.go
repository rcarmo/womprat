package main

import (
	"fmt"
	"net/url"
	"strings"
)

func navigationTarget(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return ""
	}
	u.Fragment = ""
	if u.Path == "" {
		u.Path = "/"
	}
	return u.String()
}

func chromiumNavigationMessage(raw string) string {
	// Chromium owns these symbols. Never surface arbitrary event text as HTML.
	code := strings.TrimPrefix(strings.TrimSpace(raw), "net::")
	if !strings.HasPrefix(code, "ERR_") || len(code) > 96 {
		return ""
	}
	for _, ch := range code {
		if ch != '_' && (ch < 'A' || ch > 'Z') && (ch < '0' || ch > '9') {
			return ""
		}
	}
	var explanation string
	switch code {
	case "ERR_SSL_PROTOCOL_ERROR":
		explanation = "TLS handshake failed. The HTTPS service may be using an incompatible protocol or serving plain HTTP on this port."
	case "ERR_SSL_VERSION_OR_CIPHER_MISMATCH":
		explanation = "The server and browser could not agree on a TLS version or cipher."
	case "ERR_SSL_CLIENT_AUTH_CERT_NEEDED", "ERR_BAD_SSL_CLIENT_AUTH_CERT":
		explanation = "The server requires a usable client certificate."
	case "ERR_CERT_AUTHORITY_INVALID":
		explanation = "The certificate issuer is not trusted."
	case "ERR_CERT_COMMON_NAME_INVALID":
		explanation = "The certificate does not match this hostname."
	case "ERR_CERT_DATE_INVALID":
		explanation = "The certificate is expired or not yet valid."
	case "ERR_CERT_REVOKED":
		explanation = "The server certificate has been revoked."
	case "ERR_CONNECTION_CLOSED":
		explanation = "The server closed the connection before the page loaded."
	case "ERR_CONNECTION_RESET":
		explanation = "The connection was reset before the page loaded."
	case "ERR_INVALID_RESPONSE":
		explanation = "The server did not send a valid HTTP response."
	case "ERR_SOCKS_CONNECTION_FAILED", "ERR_PROXY_CONNECTION_FAILED":
		explanation = "The browser proxy could not establish the connection."
	case "ERR_TUNNEL_CONNECTION_FAILED":
		explanation = "The proxy tunnel could not be established."
	case "ERR_BLOCKED_BY_RESPONSE":
		explanation = "The browser blocked the server response."
	default:
		explanation = "Chromium could not complete the navigation."
	}
	return fmt.Sprintf("%s (%s)", explanation, code)
}

func navigationFailureMessage(status int32, connected bool, getterError, chromiumError, certificateError, diagnosticError string) string {
	message := chromiumNavigationMessage(chromiumError)
	if message == "" {
		message = fmt.Sprintf("%s (WebView2 error %d)", webErrorStatusMessage(status, connected), status)
	}
	if getterError != "" {
		message += " Native error details unavailable: " + getterError + "."
	}
	if certificateError != "" {
		message += " Certificate exception unavailable: " + certificateError + "."
	}
	if diagnosticError != "" && chromiumNavigationMessage(chromiumError) == "" {
		message += " Chromium diagnostics unavailable: " + diagnosticError + "."
	}
	return message
}
