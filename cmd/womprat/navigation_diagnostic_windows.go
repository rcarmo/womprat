//go:build windows

package main

import "fmt"

func (v *nativeContentView) navigationFailureMessage() string {
	connected := v.tsConnected == nil || v.tsConnected()
	return navigationFailureMessage(v.navigationStatus, connected, v.navigationGetterError, v.chromiumError, v.certificateDiagnostic, v.networkDiagnosticError)
}

// A Network.loadingFailed event may arrive after NavigationCompleted. Update
// the same banner instead of leaving an UNKNOWN fallback on screen.
func (v *nativeContentView) reportNavigationFailure() {
	if v.shell == nil || !v.navigationFailed {
		return
	}
	v.shell.Eval(fmt.Sprintf("window.wompratNavigationDone(%s,%s,false,%d,%s)", jsString(v.tabID), jsString(v.navigationURL), v.navigationStatus, jsString(v.navigationFailureMessage())))
	v.reportCertificatePrompt()
}
