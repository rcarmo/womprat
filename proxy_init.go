package main

import "os"

func init() {
	// Set WebView2 browser arguments before the DLL loads.
	// WebView2Loader.dll reads this env var and passes it to the browser process.
	os.Setenv("WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS",
		"--proxy-server=socks5://127.0.0.1:1080 --proxy-bypass-list=127.0.0.1;localhost")
}
