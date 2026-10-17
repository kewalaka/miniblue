package main

import (
	"crypto/x509"
	"encoding/pem"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"
)

// holdPort listens on a free port; closing the returned listener frees it again.
func holdPort(t *testing.T) (string, net.Listener) {
	t.Helper()
	ln, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	return strconv.Itoa(ln.Addr().(*net.TCPAddr).Port), ln
}

func TestListenPorts(t *testing.T) {
	busy, _ := holdPort(t)
	free1, l1 := holdPort(t)
	free2, l2 := holdPort(t)
	l1.Close()
	l2.Close()

	if _, _, err := listenPorts(busy, free2); err == nil || !strings.Contains(err.Error(), "HTTP port "+busy) {
		t.Fatalf("busy HTTP port: got %v", err)
	}
	if _, _, err := listenPorts(free1, busy); err == nil || !strings.Contains(err.Error(), "HTTPS port "+busy) {
		t.Fatalf("busy HTTPS port: got %v", err)
	}

	// The failed call above must have released the HTTP listener it bound first.
	httpLn, tlsLn, err := listenPorts(free1, free2)
	if err != nil {
		t.Fatalf("free ports: %v", err)
	}
	httpLn.Close()
	tlsLn.Close()
}

func TestGeneratedCertificateCoversDataPlaneHosts(t *testing.T) {
	_, certPEM, err := generateAndSaveCert(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(certPEM)
	if block == nil {
		t.Fatal("expected certificate PEM block")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if err := cert.VerifyHostname("myappconfig.azconfig.io"); err != nil {
		t.Fatal(err)
	}
	if err := cert.VerifyHostname("myvault.vault.azure.net"); err != nil {
		t.Fatal(err)
	}
}

func TestDockerRunPublishesDefaultHTTPSPort(t *testing.T) {
	makefile, err := os.ReadFile("../../Makefile")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(makefile), "-p 443:4567") {
		t.Fatal("make docker-run must publish host port 443 to the TLS listener for portless Key Vault vaultUri clients")
	}

	compose, err := os.ReadFile("../../docker-compose.yml")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(compose), `"443:4567"`) {
		t.Fatal("docker-compose.yml must publish host port 443 to the TLS listener for portless Key Vault vaultUri clients")
	}
}
