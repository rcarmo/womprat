package main

import (
	"net/url"
	"strings"
)

// Certificate exceptions are intentionally separate from generic navigation errors.
// WebView2 caches ALWAYS_ALLOW for the host/certificate in this browser session.
type certificatePrompt struct {
	URL   string
	PEM   string
	Token string
}

type certificateDecision struct {
	current string
	pending certificatePrompt
	grant   certificatePrompt
}

func certificateURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || !strings.EqualFold(u.Scheme, "https") || u.Hostname() == "" || u.User != nil {
		return ""
	}
	u.Scheme = "https"
	u.Host = strings.ToLower(u.Host)
	u.Fragment = ""
	if u.Path == "" {
		u.Path = "/"
	}
	return u.String()
}

// Called for top-level navigation, including redirects, on the UI thread.
func (d *certificateDecision) starting(raw string) {
	d.current = certificateURL(raw)
	d.pending = certificatePrompt{}
	if d.grant.URL != d.current {
		d.grant = certificatePrompt{}
	}
}

func (d *certificateDecision) clear() { *d = certificateDecision{} }

func (d *certificateDecision) detected(raw, pem string, status int32, token string) bool {
	uri := certificateURL(raw)
	// Never offer exceptions for subresources, revoked certificates, client-cert
	// failures, unavailable certificates, or an event without a tracked navigation.
	if uri == "" || uri != d.current {
		return false // unrelated resource errors must not revoke top-level consent
	}
	d.pending = certificatePrompt{}
	if pem == "" ||
		(status != webErrorStatusCertificateCommonNameWrong && status != webErrorStatusCertificateExpired && status != webErrorStatusCertificateInvalid) {
		d.grant = certificatePrompt{}
		return false
	}
	if d.grant.URL == uri && d.grant.PEM == pem {
		d.grant = certificatePrompt{}
		return true
	}
	d.grant = certificatePrompt{}
	if token != "" {
		d.pending = certificatePrompt{URL: uri, PEM: pem, Token: token}
	}
	return false
}

func (d *certificateDecision) proceed(token string) (string, bool) {
	if token == "" || token != d.pending.Token || d.pending.URL != d.current {
		return "", false
	}
	d.grant = d.pending
	d.pending = certificatePrompt{}
	return d.grant.URL, true
}
