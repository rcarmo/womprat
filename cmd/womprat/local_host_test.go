package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLocalHostGuardBlocksRebindingBeforeServingToken(t *testing.T) {
	for _, host := range []string{"127.0.0.1:1234", "attacker.example:1234", "localhost:1234", "127.0.0.1:5678", ""} {
		t.Run(host, func(t *testing.T) {
			called := false
			h := localHostGuard("127.0.0.1:1234", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true; w.WriteHeader(204) }))
			r := httptest.NewRequest("GET", "http://127.0.0.1:1234/", nil)
			r.Host = host
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			allowed := host == "127.0.0.1:1234"
			if called != allowed || (!allowed && w.Code != 403) {
				t.Fatalf("host=%q called=%v status=%d", host, called, w.Code)
			}
		})
	}
}
