package main

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseDownloadURL(t *testing.T) {
	for _, raw := range []string{"https://example.com/file.txt", "http://example.com/path?q=1"} {
		if _, err := parseDownloadURL(raw); err != nil {
			t.Fatalf("parseDownloadURL(%q) error = %v", raw, err)
		}
	}
	for _, raw := range []string{"", "file:///tmp/x", "ftp://example.com/x", "https://user@example.com/x", "https://example.com/x#frag", "not-a-url"} {
		if _, err := parseDownloadURL(raw); err == nil {
			t.Fatalf("parseDownloadURL(%q) succeeded, want error", raw)
		}
	}
}

func TestFilenameFromURL(t *testing.T) {
	cases := map[string]string{
		"https://example.com/files/report.pdf": "report.pdf",
		"https://example.com/":                 "download",
		"https://example.com/a/bad:name?.zip":  "bad_name",
		"https://example.com/a/report%20x.pdf": "report x.pdf",
		"https://example.com/a/CON.txt":        "_CON.txt",
		"https://example.com/a/..":             "download",
	}
	for raw, want := range cases {
		u, _ := url.Parse(raw)
		if got := filenameFromURL(u); got != want {
			t.Fatalf("filenameFromURL(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestSanitizeDownloadFilename(t *testing.T) {
	for raw, want := range map[string]string{
		"":              "download",
		" . ":           "download",
		"bad<>name.txt": "bad__name.txt",
		"trail. ":       "trail",
		"NUL":           "_NUL",
	} {
		if got := sanitizeDownloadFilename(raw); got != want {
			t.Fatalf("sanitizeDownloadFilename(%q) = %q, want %q", raw, got, want)
		}
	}
	if got := sanitizeDownloadFilename(strings.Repeat("a", 300) + ".txt"); len(got) > 180 || !strings.HasSuffix(got, ".txt") {
		t.Fatalf("long sanitized filename = len %d %q", len(got), got)
	}
	if got := sanitizeDownloadFilename(strings.Repeat("界", 100) + ".txt"); len(got) > 180 || !strings.HasSuffix(got, ".txt") || !strings.Contains(got, "界") {
		t.Fatalf("unicode sanitized filename = len %d %q", len(got), got)
	}
	if got := sanitizeDownloadFilename("bad\xffname.txt"); got != "badname.txt" {
		t.Fatalf("invalid UTF-8 sanitized filename = %q", got)
	}
}

func TestUniqueDownloadPath(t *testing.T) {
	dir := t.TempDir()
	first := uniqueDownloadPath(dir, "file.txt")
	if first != filepath.Join(dir, "file.txt") {
		t.Fatalf("first path = %q", first)
	}
	if err := osWriteFile(first, []byte("x")); err != nil {
		t.Fatal(err)
	}
	second := uniqueDownloadPath(dir, "file.txt")
	if second == first || filepath.Base(second) != "file_1.txt" {
		t.Fatalf("second path = %q", second)
	}
	if err := os.WriteFile(second, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	third := uniqueDownloadPath(dir, "file.txt")
	if filepath.Base(third) != "file_2.txt" {
		t.Fatalf("third path = %q", third)
	}
}

var osWriteFile = func(name string, data []byte) error { return os.WriteFile(name, data, 0600) }
