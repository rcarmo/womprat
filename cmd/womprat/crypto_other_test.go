//go:build !windows

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestMachineKeyConcurrentAndCorruptPreserved(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	var wg sync.WaitGroup
	keys := make(chan []byte, 24)
	errs := make(chan error, 24)
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); key, err := getOrCreateMachineKey(); keys <- key; errs <- err }()
	}
	wg.Wait()
	close(keys)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var first []byte
	for key := range keys {
		if first == nil {
			first = key
		}
		if len(key) != 32 || !bytes.Equal(first, key) {
			t.Fatal("concurrent callers received different machine keys")
		}
	}
	path := filepath.Join(configDir(), ".keyfile")
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("key permissions %v %v", info, err)
	}
	if err = os.WriteFile(path, []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = getOrCreateMachineKey(); err == nil {
		t.Fatal("corrupt key silently replaced")
	}
	got, _ := os.ReadFile(path)
	if string(got) != "corrupt" {
		t.Fatal("corrupt key changed")
	}
}

func TestMachineKeyRejectsSymlinkAndPublicPermissions(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	os.MkdirAll(configDir(), 0700)
	path := filepath.Join(configDir(), ".keyfile")
	target := filepath.Join(t.TempDir(), "target")
	os.WriteFile(target, bytes.Repeat([]byte{1}, 32), 0600)
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if _, err := getOrCreateMachineKey(); err == nil {
		t.Fatal("symlink key accepted")
	}
	os.Remove(path)
	os.WriteFile(path, bytes.Repeat([]byte{1}, 32), 0600)
	os.Chmod(path, 0644)
	if _, err := getOrCreateMachineKey(); err == nil {
		t.Fatal("publicly readable key accepted")
	}
}
