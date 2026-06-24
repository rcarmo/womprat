package main

// COREWEBVIEW2_WEB_ERROR_STATUS values surfaced by WebView2 navigation
// completion. These are stable across the WebView2 SDK and are mapped to plain
// language so the shell can give the user an actionable reason instead of a
// blank error page. Kept in a build-tag-free file so the mapping is unit
// testable on every platform.
const (
	webErrorStatusUnknown                    int32 = 0
	webErrorStatusCertificateCommonNameWrong int32 = 1
	webErrorStatusCertificateExpired         int32 = 2
	webErrorStatusClientCertificateErrors    int32 = 3
	webErrorStatusCertificateRevoked         int32 = 4
	webErrorStatusCertificateInvalid         int32 = 5
	webErrorStatusServerUnreachable          int32 = 6
	webErrorStatusTimeout                    int32 = 7
	webErrorStatusInvalidServerResponse      int32 = 8
	webErrorStatusConnectionAborted          int32 = 9
	webErrorStatusConnectionReset            int32 = 10
	webErrorStatusDisconnected               int32 = 11
	webErrorStatusCannotConnect              int32 = 12
	webErrorStatusHostNameNotResolved        int32 = 13
	webErrorStatusOperationCanceled          int32 = 14
	webErrorStatusRedirectFailed             int32 = 15
	webErrorStatusUnexpectedError            int32 = 16
	webErrorStatusValidAuthRequired          int32 = 17
	webErrorStatusValidProxyAuthRequired     int32 = 18
)

// webErrorStatusMessage returns a user-facing explanation for a navigation
// failure. The tsConnected flag lets connectivity-class failures point the user
// at the most likely root cause (Tailscale not connected) instead of a generic
// network error.
func webErrorStatusMessage(status int32, tsConnected bool) string {
	switch status {
	case webErrorStatusHostNameNotResolved:
		if !tsConnected {
			return "Couldn't resolve host. Tailscale is not connected — open Settings to connect."
		}
		return "Couldn't resolve host name. Check the address or MagicDNS/exit-node routing."
	case webErrorStatusServerUnreachable, webErrorStatusCannotConnect:
		if !tsConnected {
			return "Host unreachable. Tailscale is not connected — open Settings to connect."
		}
		return "Host unreachable. It may be offline, or an exit node is required for non-tailnet sites."
	case webErrorStatusConnectionReset, webErrorStatusConnectionAborted, webErrorStatusDisconnected:
		return "Connection dropped before the page finished loading. Try reloading."
	case webErrorStatusTimeout:
		if !tsConnected {
			return "Connection timed out. Tailscale is not connected — open Settings to connect."
		}
		return "Connection timed out. The host may be slow or unreachable over the tailnet."
	case webErrorStatusCertificateCommonNameWrong, webErrorStatusCertificateExpired,
		webErrorStatusCertificateRevoked, webErrorStatusCertificateInvalid,
		webErrorStatusClientCertificateErrors:
		return "The site's TLS certificate could not be validated."
	case webErrorStatusInvalidServerResponse, webErrorStatusRedirectFailed:
		return "The server sent an invalid or broken response."
	case webErrorStatusValidAuthRequired:
		return "Authentication is required to view this page."
	case webErrorStatusValidProxyAuthRequired:
		return "Proxy authentication is required."
	case webErrorStatusOperationCanceled:
		return "Navigation was canceled."
	case webErrorStatusUnexpectedError, webErrorStatusUnknown:
		fallthrough
	default:
		if !tsConnected {
			return "Page failed to load. Tailscale is not connected — open Settings to connect."
		}
		return "Page failed to load."
	}
}
