package main

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"

	"golang.org/x/crypto/pbkdf2"
)

const (
	masterKDF              = "pbkdf2-sha256"
	masterKDFIterations    = 310000
	minMasterKDFIterations = 100000
	maxMasterKDFIterations = 1000000
	masterSaltBytes        = 16
	masterHashBytes        = 32
)

type masterHashRecord struct {
	KDF        string `json:"kdf"`
	Iterations int    `json:"iterations"`
	Salt       string `json:"salt"`
	Hash       string `json:"hash"`
}

func shouldStartLocked(cfg *AppConfig) bool {
	if cfg == nil || cfg.UnlockMethod != "master" {
		return false
	}
	_, err := GetCredential("master-hash")
	return err == nil
}

func verifyMasterPassword(password string) (bool, error) {
	if password == "" {
		return false, nil
	}
	data, err := GetCredential("master-hash")
	if err != nil {
		return false, err
	}
	var rec masterHashRecord
	if err := json.Unmarshal([]byte(data), &rec); err != nil {
		return false, err
	}
	if rec.KDF != masterKDF || rec.Iterations < minMasterKDFIterations || rec.Iterations > maxMasterKDFIterations {
		return false, nil
	}
	salt, err := base64.StdEncoding.DecodeString(rec.Salt)
	if err != nil {
		return false, err
	}
	if len(salt) != masterSaltBytes {
		return false, nil
	}
	expected, err := base64.StdEncoding.DecodeString(rec.Hash)
	if err != nil {
		return false, err
	}
	if len(expected) != masterHashBytes {
		return false, nil
	}
	actual := pbkdf2.Key([]byte(password), salt, rec.Iterations, len(expected), sha256.New)
	return subtle.ConstantTimeCompare(actual, expected) == 1, nil
}
