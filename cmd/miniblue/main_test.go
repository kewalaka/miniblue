package main

import (
	"crypto/x509"
	"encoding/pem"
	"os"
	"strings"
	"testing"
)

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
