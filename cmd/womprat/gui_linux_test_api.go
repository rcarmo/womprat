//go:build linux && webkitgui && guittest

package main

/*
#include "webkit_linux.h"
#include <stdlib.h>
*/
import "C"

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime/cgo"
	"unsafe"
)

func init() { installLinuxGUIAcceptance = installLinuxGUITest }
func installLinuxGUITest(m *linuxContentManager) {
	mux := http.NewServeMux()
	if os.Getenv("WOMPRAT_GUI_FIXTURES") == "1" {
		// guittest-only fixture path. It does not enable release direct dialing.
		allowDirectDial = true
		mux.HandleFunc("/page", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			http.SetCookie(w, &http.Cookie{Name: "native", Value: "fixture", Path: "/", HttpOnly: true})
			fmt.Fprintf(w, `<!doctype html><title>Native %s</title><p id="content">Native fixture %s</p><a id="next" href="/page?name=next">Next</a><a id="popup" target="_blank" href="/page?name=popup">Popup</a><a id="download" download href="/download">Download</a>`, r.URL.Query().Get("name"), r.URL.Query().Get("name"))
		})
		mux.HandleFunc("/download", func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie("native")
			if err != nil || cookie.Value != "fixture" {
				http.Error(w, "Cookie required", 403)
				return
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("Content-Disposition", `attachment; filename="native.txt"`)
			fmt.Fprint(w, "native managed download fixture")
		})
		tls := httptest.NewTLSServer(mux)
		fmt.Printf("WOMPRAT_GUI_TLS_URL=%s\n", tls.URL)

	}
	mux.Handle("/quit", m.app.authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		m.dispatch(func() { C.womprat_gtk_quit(m.host) })
	})))
	mux.Handle("/query", m.app.authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Tab    string `json:"tab"`
			Script string `json:"script"`
		}
		if json.NewDecoder(r.Body).Decode(&request) != nil {
			http.Error(w, "Invalid request", 400)
			return
		}
		result := make(chan string, 1)
		handle := cgo.NewHandle(result)
		m.dispatch(func() {
			id, script := C.CString(request.Tab), C.CString(request.Script)
			defer C.free(unsafe.Pointer(id))
			defer C.free(unsafe.Pointer(script))
			C.womprat_gtk_query(m.host, id, script, C.uintptr_t(handle))
		})
		select {
		case text := <-result:
			json.NewEncoder(w).Encode(map[string]string{"result": text})
		case <-r.Context().Done():
		}
	})))
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	fmt.Printf("WOMPRAT_GUI_TEST_URL=http://%s/\n", ln.Addr())
	go http.Serve(ln, mux)
}
