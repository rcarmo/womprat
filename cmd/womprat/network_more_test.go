package main

import (
	"bufio"
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"encoding/binary"
	"encoding/pem"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	_ "modernc.org/sqlite"
)

func TestNonWindowsIconAndDarkModeStubs(t *testing.T) {
	applyAppIcon(nil)
	applyDarkMode(nil)
}

func TestListCookieDomainsWithSQLite(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	cookiePath := filepath.Join(webviewDataPath(), "EBWebView", "Default", "Network", "Cookies")
	if err := os.MkdirAll(filepath.Dir(cookiePath), 0700); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", cookiePath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE cookies(host_key TEXT); INSERT INTO cookies(host_key) VALUES ('.example.com'),('.example.com'),('smith.local')`)
	if err != nil {
		db.Close()
		t.Fatal(err)
	}
	_ = db.Close()

	domains := listCookieDomains()
	if len(domains) != 2 {
		t.Fatalf("domains = %+v", domains)
	}
	if err := deleteCookiesForDomain(".example.com"); err != nil {
		t.Fatal(err)
	}
	domains = listCookieDomains()
	if len(domains) != 1 || domains[0].Domain != "smith.local" {
		t.Fatalf("after delete domains = %+v", domains)
	}
}

func TestGetSSHAuthMethods(t *testing.T) {
	app := newTestApp(t)
	_, priv1, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pem1, err := ssh.MarshalPrivateKey(priv1, "main")
	if err != nil {
		t.Fatal(err)
	}
	encoded := string(pem.EncodeToMemory(pem1))
	if err := SaveCredential("ssh-key/main", encoded); err != nil {
		t.Fatal(err)
	}
	if err := SaveCredential("ssh-key/duplicate", encoded); err != nil {
		t.Fatal(err)
	}
	app.config.Hosts["smith"] = HostConfig{KeyName: "main"}
	methods := app.getSSHAuthMethods("smith")
	if len(methods) != 1 {
		t.Fatalf("methods = %d", len(methods))
	}
}

func TestHandleSOCKS5NoTailscale(t *testing.T) {
	app := newTestApp(t)
	client, server := net.Pipe()
	done := make(chan struct{})
	go func() {
		handleSOCKS5(server, app)
		close(done)
	}()
	br := bufio.NewReader(client)
	// greeting: SOCKS5, one method, no auth
	if _, err := client.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		t.Fatal(err)
	}
	greet := make([]byte, 2)
	if _, err := br.Read(greet); err != nil || greet[0] != 0x05 || greet[1] != 0x00 {
		t.Fatalf("greeting = %v %v", greet, err)
	}
	// CONNECT example.com:80 as domain. With no tsServer this should fail closed.
	req := []byte{0x05, 0x01, 0x00, 0x03, byte(len("example.com"))}
	req = append(req, []byte("example.com")...)
	port := make([]byte, 2)
	binary.BigEndian.PutUint16(port, 80)
	req = append(req, port...)
	if _, err := client.Write(req); err != nil {
		t.Fatal(err)
	}
	rep := make([]byte, 10)
	if _, err := br.Read(rep); err != nil {
		t.Fatal(err)
	}
	if rep[1] != 0x05 {
		t.Fatalf("reply = %v, want general failure", rep)
	}
	_ = client.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("SOCKS handler did not exit")
	}
}
