package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDownloadTicketIsOneUseURLBoundAndCloned(t *testing.T) {
	app := newTestApp(t)
	cookies := []*http.Cookie{{Name: "session", Value: "secret", HttpOnly: true}}
	ticket, err := app.createDownloadTicket("https://example.com/file", cookies)
	if err != nil {
		t.Fatal(err)
	}
	cookies[0].Value = "changed"
	if _, err := app.consumeDownloadTicket(ticket, "https://other.example/file"); err == nil {
		t.Fatal("ticket accepted for another URL")
	}
	if _, err := app.consumeDownloadTicket(ticket, "https://example.com/file"); err == nil {
		t.Fatal("mismatched attempt did not consume ticket")
	}
	ticket, _ = app.createDownloadTicket("https://example.com/file", cookies)
	got, err := app.consumeDownloadTicket(ticket, "https://example.com/file")
	if err != nil || len(got) != 1 || got[0].Value != "changed" || !got[0].HttpOnly {
		t.Fatalf("cookies=%+v err=%v", got, err)
	}
}

func TestDownloadHandlerRejectsInvalidOrMismatchedTicket(t *testing.T) {
	app := newTestApp(t)
	t.Setenv("HOME", t.TempDir())
	for _, requestURL := range []string{
		"/api/download?url=https://example.com/file&ticket=missing",
		"/api/download?url=https://other.example/file&ticket=valid",
	} {
		if strings.Contains(requestURL, "ticket=valid") {
			app.downloadTickets["valid"] = downloadTicket{URL: "https://example.com/file", Expires: time.Now().Add(time.Minute)}
		}
		rr := performJSON(app.handleDownload, http.MethodGet, requestURL, nil)
		if rr.Code != http.StatusForbidden {
			t.Fatalf("%s status=%d body=%s", requestURL, rr.Code, rr.Body.String())
		}
	}
}

func TestBusyDownloadDoesNotConsumeCookieTicket(t *testing.T) {
	app := newTestApp(t)
	t.Setenv("HOME", t.TempDir())
	ticket, err := app.createDownloadTicket("https://example.com/file", []*http.Cookie{{Name: "session", Value: "secret"}})
	if err != nil {
		t.Fatal(err)
	}
	downloadMu.Lock()
	previous, previousStarting := currentDownload, downloadStarting
	currentDownload = &downloadState{Status: "downloading"}
	downloadStarting = false
	downloadMu.Unlock()
	t.Cleanup(func() {
		downloadMu.Lock()
		currentDownload, downloadStarting = previous, previousStarting
		downloadMu.Unlock()
	})
	rr := performJSON(app.handleDownload, http.MethodGet, "/api/download?url=https://example.com/file&ticket="+ticket, nil)
	if rr.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	cookies, err := app.consumeDownloadTicket(ticket, "https://example.com/file")
	if err != nil || len(cookies) != 1 || cookies[0].Value != "secret" {
		t.Fatalf("ticket consumed: cookies=%+v err=%v", cookies, err)
	}
}

func TestDownloadTicketStoreIsBounded(t *testing.T) {
	app := newTestApp(t)
	for index := 0; index < maxDownloadTickets; index++ {
		if _, err := app.createDownloadTicket("https://example.com/file", nil); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := app.createDownloadTicket("https://example.com/file", nil); err == nil {
		t.Fatal("unbounded ticket store accepted")
	}
}

func TestDownloadTicketExpires(t *testing.T) {
	app := newTestApp(t)
	app.downloadTickets["old"] = downloadTicket{URL: "https://example.com/file", Expires: time.Now().Add(-time.Second)}
	if _, err := app.consumeDownloadTicket("old", "https://example.com/file"); err == nil {
		t.Fatal("expired ticket accepted")
	}
}

func TestManagedDownloadSendsCookiesAndContainsRedirects(t *testing.T) {
	withDirectDial(t)
	var firstCookie, redirectedCookie string
	redirected := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirectedCookie = r.Header.Get("Cookie")
		_, _ = w.Write([]byte("ok"))
	}))
	defer redirected.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		firstCookie = r.Header.Get("Cookie")
		redirectURL := strings.Replace(redirected.URL, "127.0.0.1", "localhost", 1)
		http.Redirect(w, r, redirectURL+"/final", http.StatusFound)
	}))
	defer origin.Close()

	app := newTestApp(t)
	path := filepath.Join(t.TempDir(), "file.txt")
	st := &downloadState{Status: "downloading"}
	app.downloadToFile(origin.URL+"/file", path, st, []*http.Cookie{
		{Name: "session", Value: "secret", Path: "/", HttpOnly: true},
		{Name: "secure", Value: "only", Path: "/", Secure: true},
	})
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "ok" || st.Status != "complete" {
		t.Fatalf("data=%q err=%v state=%+v", data, err, st)
	}
	if !strings.Contains(firstCookie, "session=secret") || strings.Contains(firstCookie, "secure=only") {
		t.Fatalf("origin cookie=%q", firstCookie)
	}
	if redirectedCookie != "" {
		t.Fatalf("cookie leaked across origin redirect: %q", redirectedCookie)
	}
}
