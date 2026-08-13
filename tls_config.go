package main

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
)

// buildTLSConfig builds a *tls.Config for connecting to Redis over TLS from the
// -tls/-tls_ca/-tls_cert/-tls_key/-tls_insecure_skip_verify flags. It returns
// (nil, nil) when TLS is disabled, matching the previous (non-TLS) behavior.
func buildTLSConfig(enabled bool, caFile, certFile, keyFile string, insecureSkipVerify bool) (*tls.Config, error) {
	if !enabled {
		return nil, nil
	}

	tlsConfig := &tls.Config{InsecureSkipVerify: insecureSkipVerify}

	if caFile != "" {
		caCert, err := os.ReadFile(caFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read TLS CA file %q: %w", caFile, err)
		}
		caPool := x509.NewCertPool()
		if !caPool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("failed to parse TLS CA file %q: no valid PEM certificates found", caFile)
		}
		tlsConfig.RootCAs = caPool
	}

	if (certFile != "") != (keyFile != "") {
		return nil, fmt.Errorf("both -tls_cert and -tls_key must be specified together (got cert=%q key=%q)", certFile, keyFile)
	}
	if certFile != "" && keyFile != "" {
		cert, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			return nil, fmt.Errorf("failed to load TLS client cert/key pair (%q, %q): %w", certFile, keyFile, err)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}

	return tlsConfig, nil
}
