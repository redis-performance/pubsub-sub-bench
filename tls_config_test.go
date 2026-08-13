package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writePEMFile(t *testing.T, dir, name, blockType string, der []byte) string {
	t.Helper()
	path := filepath.Join(dir, name)
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("failed to create %s: %v", path, err)
	}
	defer f.Close()
	if err := pem.Encode(f, &pem.Block{Type: blockType, Bytes: der}); err != nil {
		t.Fatalf("failed to encode PEM to %s: %v", path, err)
	}
	return path
}

// generateTestCA creates a self-signed CA certificate, writes it to dir/ca.crt, and
// returns the file path plus the parsed cert/key for signing a leaf certificate.
func generateTestCA(t *testing.T, dir string) (caCertPath string, caCert *x509.Certificate, caKey *rsa.PrivateKey) {
	t.Helper()
	caKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate CA key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "pubsub-sub-bench test CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("failed to create CA certificate: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("failed to parse CA certificate: %v", err)
	}
	caCertPath = writePEMFile(t, dir, "ca.crt", "CERTIFICATE", der)
	return caCertPath, cert, caKey
}

// generateTestLeafCert creates a leaf certificate signed by the given CA and returns
// its cert/key PEM file paths.
func generateTestLeafCert(t *testing.T, dir string, caCert *x509.Certificate, caKey *rsa.PrivateKey) (certPath, keyPath string) {
	t.Helper()
	leafKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate leaf key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "pubsub-sub-bench test client"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"localhost"},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, caCert, &leafKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("failed to create leaf certificate: %v", err)
	}
	certPath = writePEMFile(t, dir, "leaf.crt", "CERTIFICATE", der)
	keyPath = writePEMFile(t, dir, "leaf.key", "RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(leafKey))
	return certPath, keyPath
}

func TestBuildTLSConfig(t *testing.T) {
	dir := t.TempDir()
	caCertPath, caCert, caKey := generateTestCA(t, dir)
	certPath, keyPath := generateTestLeafCert(t, dir, caCert, caKey)

	invalidPEMPath := filepath.Join(dir, "invalid.pem")
	if err := os.WriteFile(invalidPEMPath, []byte("not a real certificate"), 0o600); err != nil {
		t.Fatalf("failed to write invalid PEM file: %v", err)
	}

	t.Run("disabled returns nil config and no error", func(t *testing.T) {
		cfg, err := buildTLSConfig(false, caCertPath, certPath, keyPath, true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg != nil {
			t.Fatalf("expected nil config when TLS disabled, got %+v", cfg)
		}
	})

	t.Run("enabled with no files uses system pool and no client cert", func(t *testing.T) {
		cfg, err := buildTLSConfig(true, "", "", "", false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg == nil {
			t.Fatal("expected non-nil config when TLS enabled")
		}
		if cfg.RootCAs != nil {
			t.Fatalf("expected nil RootCAs (system pool) when no CA file given, got %+v", cfg.RootCAs)
		}
		if len(cfg.Certificates) != 0 {
			t.Fatalf("expected no client certificates, got %d", len(cfg.Certificates))
		}
		if cfg.InsecureSkipVerify {
			t.Fatal("expected InsecureSkipVerify to be false")
		}
	})

	t.Run("valid CA file populates RootCAs", func(t *testing.T) {
		cfg, err := buildTLSConfig(true, caCertPath, "", "", false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.RootCAs == nil {
			t.Fatal("expected RootCAs to be populated from the CA file")
		}
	})

	t.Run("missing CA file path errors", func(t *testing.T) {
		_, err := buildTLSConfig(true, filepath.Join(dir, "does-not-exist.crt"), "", "", false)
		if err == nil {
			t.Fatal("expected error for missing CA file, got nil")
		}
	})

	t.Run("malformed PEM CA file errors", func(t *testing.T) {
		_, err := buildTLSConfig(true, invalidPEMPath, "", "", false)
		if err == nil {
			t.Fatal("expected error for malformed CA PEM, got nil")
		}
	})

	t.Run("matching cert and key populate Certificates", func(t *testing.T) {
		cfg, err := buildTLSConfig(true, "", certPath, keyPath, false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(cfg.Certificates) != 1 {
			t.Fatalf("expected 1 client certificate, got %d", len(cfg.Certificates))
		}
	})

	t.Run("cert without key errors", func(t *testing.T) {
		_, err := buildTLSConfig(true, "", certPath, "", false)
		if err == nil {
			t.Fatal("expected error when cert is set without key, got nil")
		}
	})

	t.Run("key without cert errors", func(t *testing.T) {
		_, err := buildTLSConfig(true, "", "", keyPath, false)
		if err == nil {
			t.Fatal("expected error when key is set without cert, got nil")
		}
	})

	t.Run("insecureSkipVerify propagates", func(t *testing.T) {
		cfg, err := buildTLSConfig(true, "", "", "", true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !cfg.InsecureSkipVerify {
			t.Fatal("expected InsecureSkipVerify to be true")
		}
	})
}
