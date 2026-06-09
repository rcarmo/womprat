package main

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFilenameFromURL(t *testing.T) {
	cases := map[string]string{
		"https://example.com/files/report.pdf": "report.pdf",
		"https://example.com/":                 "download",
		"https://example.com/a/bad:name?.zip":  "bad_name",
	}
	for raw, want := range cases {
		u, _ := url.Parse(raw)
		if got := filenameFromURL(u); got != want {
			t.Fatalf("filenameFromURL(%q) = %q, want %q", raw, got, want)
		}
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
	if second == first || !strings.HasPrefix(filepath.Base(second), "file_") || filepath.Ext(second) != ".txt" {
		t.Fatalf("second path = %q", second)
	}
}

var osWriteFile = func(name string, data []byte) error { return os.WriteFile(name, data, 0600) }
