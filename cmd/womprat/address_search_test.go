package main

import (
	"net/url"
	"strings"
	"testing"
)

func TestAddressInputSearchesWordsWithSelectedProvider(t *testing.T) {
	for _, provider := range []string{"", "duckduckgo", "bing", "google", "invalid"} {
		for _, text := range []string{"weather", "two words", "C++ & Go? #tips", "café 日本語", "hello.world search", "what is 10/2", "how to use https://example.com", "person@example.com"} {
			got, err := resolveAddressInput(text, provider)
			if err != nil {
				t.Fatal(text, err)
			}
			u, err := url.Parse(got)
			if err != nil {
				t.Fatal(err)
			}
			wantHost := "duckduckgo.com"
			if provider == "bing" {
				wantHost = "www.bing.com"
			}
			if provider == "google" {
				wantHost = "www.google.com"
			}
			if u.Scheme != "https" || u.Host != wantHost || u.Query().Get("q") != text || len(u.Query()) != 1 || u.Fragment != "" {
				t.Fatal("wrong or unescaped search", text, got)
			}
		}
	}
}
func TestAddressInputPreservesWebAndConnectionAddresses(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"100.64.0.2", "http://100.64.0.2"}, {"fd7a:115c:a1e0::1", "http://[fd7a:115c:a1e0::1]"}, {"localhost", "http://localhost"},
		{"bing.com", "http://bing.com"}, {"flint.local:8443/vault/id", "http://flint.local:8443/vault/id"},
		{"https://flint.local:8443/vault/id", "https://flint.local:8443/vault/id"}, {"localhost:8000", "http://localhost:8000"},
		{"100.64.0.2:8443", "http://100.64.0.2:8443"}, {"[fd7a:115c:a1e0::1]:8443/", "http://[fd7a:115c:a1e0::1]:8443/"},
		{"relay:8080", "http://relay:8080"}, {"relay/status", "http://relay/status"}, {"https://relay/", "https://relay/"},
		{"//flint.local/", "http://flint.local/"}, {"ssh://user@flint.local", "ssh://user@flint.local"},
		{"vnc://flint.local:5900", "vnc://flint.local:5900"}, {"rdp://user@flint.local", "rdp://user@flint.local"},
		{"https://bücher.example/", "https://xn--bcher-kva.example/"},
		{"https://flint.local/a b", "https://flint.local/a%20b"},
	} {
		got, err := resolveAddressInput(tc.in, "google")
		if err != nil || got != tc.want {
			t.Fatalf("%q => %q, %v; want %q", tc.in, got, err, tc.want)
		}
	}
}
func TestAddressInputRejectsUnsafeSchemesCredentialsAndControls(t *testing.T) {
	for _, input := range []string{"", " ", "javascript:alert(1)", "file:///C:/Users/me/private.txt", "data:text/html,hi", "http:broken", "https://user:pass@flint.local/", "user:pass@flint.local", "ssh://user:pass@flint.local", "rdp://flint.local/path", "hello\nworld", "hello\x00world", strings.Repeat("q", 4097), "https://flint.local:99999/", "flint.local:99999/", `C:\Users\me\secret.txt`, "/home/me/private.txt", `\\server\share`} {
		if got, err := resolveAddressInput(input, "duckduckgo"); err == nil {
			t.Fatalf("unsafe address became %q: %q", got, input)
		}
	}
}
func TestSearchProviderConfigDefaults(t *testing.T) {
	if defaultConfig().SearchProvider != "duckduckgo" {
		t.Fatal("new config default not DDG")
	}
	for _, input := range []string{"", "custom", "https://evil.example/?q="} {
		cfg := defaultConfig()
		cfg.SearchProvider = input
		normalizeConfig(cfg)
		if cfg.SearchProvider != "duckduckgo" {
			t.Fatal("invalid provider survived")
		}
	}
}
