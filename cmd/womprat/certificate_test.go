package main

import "testing"

func TestCertificateConsentBindsURLCertificateAndToken(t *testing.T) {
	var d certificateDecision
	const uri = "https://flint.local:8443/"
	d.starting(uri)
	if d.detected(uri, "certificate-A", webErrorStatusCertificateInvalid, "token-1") {
		t.Fatal("allowed before consent")
	}
	if _, ok := d.proceed("wrong"); ok {
		t.Fatal("wrong token accepted")
	}
	if got, ok := d.proceed("token-1"); !ok || got != uri {
		t.Fatal("valid consent rejected", got)
	}
	if _, ok := d.proceed("token-1"); ok {
		t.Fatal("token replay accepted")
	}
	d.starting(uri)
	if !d.detected(uri, "certificate-A", webErrorStatusCertificateInvalid, "token-2") {
		t.Fatal("matching retry denied")
	}
	if d.detected(uri, "certificate-A", webErrorStatusCertificateInvalid, "token-3") {
		t.Fatal("grant reused by application")
	}
}

func TestCertificateConsentNeverAllowsChangedLeafOrDestination(t *testing.T) {
	for _, change := range []struct{ url, pem string }{
		{"https://flint.local:8443/", "other-leaf"},
		{"https://other.local:8443/", "leaf"},
		{"https://flint.local:8444/", "leaf"},
		{"https://flint.local:8443/other", "leaf"},
	} {
		t.Run(change.url+change.pem, func(t *testing.T) {
			var d certificateDecision
			d.starting("https://flint.local:8443/")
			d.detected(d.current, "leaf", webErrorStatusCertificateInvalid, "token")
			d.proceed("token")
			d.starting(change.url)
			if d.detected(change.url, change.pem, webErrorStatusCertificateInvalid, "new-token") {
				t.Fatal("changed target allowed")
			}
		})
	}
}

func TestCertificatePromptExcludesResourcesRevocationAndClientAuth(t *testing.T) {
	for _, status := range []int32{webErrorStatusCertificateRevoked, webErrorStatusClientCertificateErrors, webErrorStatusUnknown, webErrorStatusTimeout} {
		var d certificateDecision
		d.starting("https://flint.local:8443/")
		if d.detected(d.current, "leaf", status, "token") || d.pending.Token != "" {
			t.Fatal("unsafe error offered exception", status)
		}
	}
	var d certificateDecision
	d.starting("https://flint.local:8443/")
	for _, uri := range []string{"http://flint.local:8443/", "https://flint.local:8443/script.js", "https://other.local/", "https://user@flint.local:8443/", ""} {
		if d.detected(uri, "leaf", webErrorStatusCertificateInvalid, "token") || d.pending.Token != "" {
			t.Fatal("non-navigation offered exception", uri)
		}
	}
	if d.detected(d.current, "", webErrorStatusCertificateInvalid, "token") || d.pending.Token != "" {
		t.Fatal("missing certificate offered")
	}
}

func TestCertificatePromptInvalidatesOnNavigationAndDestruction(t *testing.T) {
	var d certificateDecision
	d.starting("https://flint.local:8443/")
	d.detected(d.current, "leaf", webErrorStatusCertificateExpired, "token")
	d.starting("https://other.local/")
	if _, ok := d.proceed("token"); ok {
		t.Fatal("stale consent allowed after navigation")
	}
	d.detected(d.current, "leaf", webErrorStatusCertificateCommonNameWrong, "other-token")
	d.clear()
	if _, ok := d.proceed("other-token"); ok || d.pending.Token != "" || d.grant.PEM != "" {
		t.Fatal("cleared consent retained")
	}
}
