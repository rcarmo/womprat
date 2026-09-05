package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEncryptedFileReplacement(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.enc")
	for _, payload := range []string{"original ciphertext", "replacement"} {
		if err := writeEncryptedFile(path, []byte(payload)); err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(path)
		if err != nil || string(got) != payload {
			t.Fatalf("got=%q err=%v", got, err)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("temporary files remain: %v %v", entries, err)
	}
}

func TestEncryptedFileFailedReplacementCleansTemporaryFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "occupied")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(path, "marker")
	if err := os.WriteFile(marker, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := writeEncryptedFile(path, []byte("new")); err == nil {
		t.Fatal("replacement should fail")
	}
	if got, err := os.ReadFile(marker); err != nil || string(got) != "keep" {
		t.Fatalf("destination changed: %q %v", got, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("temporary files remain: %v %v", entries, err)
	}
}
