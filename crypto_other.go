//go:build !windows

package main

import (
	"crypto/rand"
	"os"
	"path/filepath"
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

func getOrCreateMachineKey() ([]byte, error) {
	keyPath := filepath.Join(configDir(), ".keyfile")
	data, err := os.ReadFile(keyPath)
	if err == nil && len(data) == 32 {
		return data, nil
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	if err := os.WriteFile(keyPath, key, 0600); err != nil {
		return nil, err
	}
	return key, nil
}
