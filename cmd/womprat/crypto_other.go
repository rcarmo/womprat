//go:build !windows

package main

import (
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// encryptConfig on non-Windows uses AES-GCM with a machine-local key file
func encryptConfig(plaintext []byte) ([]byte, error) {
	key, err := getOrCreateMachineKey()
	if err != nil {
		return nil, err
	}
	return encryptAESGCM(plaintext, key)
}

// decryptConfig on non-Windows
func decryptConfig(ciphertext []byte) ([]byte, error) {
	key, err := getOrCreateMachineKey()
	if err != nil {
		return nil, err
	}
	return decryptAESGCM(ciphertext, key)
}

var machineKeyMu sync.Mutex

func getOrCreateMachineKey() ([]byte, error) {
	machineKeyMu.Lock()
	defer machineKeyMu.Unlock()
	dir, err := configDirPath()
	if err != nil {
		return nil, err
	}
	keyPath := filepath.Join(dir, ".keyfile")
	read := func() ([]byte, error) {
		info, err := os.Lstat(keyPath)
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
			return nil, fmt.Errorf("unsafe machine key file")
		}
		key, err := os.ReadFile(keyPath)
		if err != nil {
			return nil, err
		}
		if len(key) != 32 {
			return nil, fmt.Errorf("invalid machine key length: %d", len(key))
		}
		return key, nil
	}
	key, err := read()
	if err == nil {
		return key, nil
	}
	// Preserve unreadable/corrupt keys instead of making encrypted state lost.
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	key = make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	file, err := os.CreateTemp(dir, ".keyfile-new-*")
	if err != nil {
		return nil, err
	}
	defer os.Remove(file.Name())
	if _, err = file.Write(key); err != nil {
		file.Close()
		return nil, err
	}
	if err = file.Sync(); err != nil {
		file.Close()
		return nil, err
	}
	if err = file.Close(); err != nil {
		return nil, err
	}
	// Atomic no-overwrite publication: competing processes read the winning key.
	if err = os.Link(file.Name(), keyPath); err != nil {
		if errors.Is(err, os.ErrExist) {
			return read()
		}
		return nil, err
	}
	return key, nil
}
