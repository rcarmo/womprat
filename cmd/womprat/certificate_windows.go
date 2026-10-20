//go:build windows

package main

import "fmt"

func (v *nativeContentView) reportCertificatePrompt() {
	if v.shell == nil {
		return
	}
	p := v.certificate.pending
	v.shell.Eval(fmt.Sprintf("window.wompratCertificatePrompt(%s,%s,%s)", jsString(v.tabID), jsString(p.Token), jsString(p.URL)))
}

func (v *nativeContentView) ProceedCertificate(token string) bool {
	if v.edge == nil {
		return false
	}
	uri, ok := v.certificate.proceed(token)
	if !ok {
		return false
	}
	v.reportCertificatePrompt()
	// Do not call Navigate: it intentionally invalidates all pending consent.
	v.edge.Navigate(uri)
	return true
}
