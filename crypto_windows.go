//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

var (
	crypt32              = syscall.NewLazyDLL("crypt32.dll")
	kernel32             = syscall.NewLazyDLL("kernel32.dll")
	procCryptProtectData = crypt32.NewProc("CryptProtectData")
	procCryptUnprotectData = crypt32.NewProc("CryptUnprotectData")
	procLocalFree        = kernel32.NewProc("LocalFree")
)

type dataBlob struct {
	cbData uint32
	pbData *byte
}

// encryptConfig encrypts data using Windows DPAPI (user-scope)
// Only the same Windows user account can decrypt it.
func encryptConfig(plaintext []byte) ([]byte, error) {
	input := dataBlob{
		cbData: uint32(len(plaintext)),
		pbData: &plaintext[0],
	}
	var output dataBlob

	// CRYPTPROTECT_LOCAL_MACHINE = 0x4 (omitted = user-only)
	ret, _, err := procCryptProtectData.Call(
		uintptr(unsafe.Pointer(&input)),
		0, // description
		0, // optional entropy
		0, // reserved
		0, // prompt
		0, // flags (user-scope, most restrictive)
		uintptr(unsafe.Pointer(&output)),
	)
	if ret == 0 {
		return nil, err
	}

	result := make([]byte, output.cbData)
	copy(result, unsafe.Slice(output.pbData, output.cbData))
	procLocalFree.Call(uintptr(unsafe.Pointer(output.pbData)))
	return result, nil
}

// decryptConfig decrypts data encrypted with encryptConfig
func decryptConfig(ciphertext []byte) ([]byte, error) {
	input := dataBlob{
		cbData: uint32(len(ciphertext)),
		pbData: &ciphertext[0],
	}
	var output dataBlob

	ret, _, err := procCryptUnprotectData.Call(
		uintptr(unsafe.Pointer(&input)),
		0, // description out
		0, // optional entropy
		0, // reserved
		0, // prompt
		0, // flags
		uintptr(unsafe.Pointer(&output)),
	)
	if ret == 0 {
		return nil, err
	}

	result := make([]byte, output.cbData)
	copy(result, unsafe.Slice(output.pbData, output.cbData))
	procLocalFree.Call(uintptr(unsafe.Pointer(output.pbData)))
	return result, nil
}
