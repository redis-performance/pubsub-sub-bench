//go:build integration

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	redis "github.com/redis/go-redis/v9"
)

// tlsIntegrationEnv resolves connection details for the dockerized TLS Redis
// started via scripts/redis-tls-docker.sh, with env var overrides. Skips the
// test (rather than failing) when the expected cert fixtures aren't present,
// since this file only builds under `-tags=integration` and is meant to run
// against a real, externally-managed Redis instance.
func tlsIntegrationEnv(t *testing.T) (host, port, caFile, certFile, keyFile string) {
	t.Helper()
	host = envOrDefault("PUBSUB_BENCH_TLS_HOST", "127.0.0.1")
	port = envOrDefault("PUBSUB_BENCH_TLS_PORT", "16390")

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	defaultCertsDir := filepath.Join(wd, "testdata", "tls", "certs")
	caFile = envOrDefault("PUBSUB_BENCH_TLS_CA", filepath.Join(defaultCertsDir, "ca.crt"))
	certFile = envOrDefault("PUBSUB_BENCH_TLS_CERT", filepath.Join(defaultCertsDir, "client.crt"))
	keyFile = envOrDefault("PUBSUB_BENCH_TLS_KEY", filepath.Join(defaultCertsDir, "client.key"))

	if _, err := os.Stat(caFile); err != nil {
		t.Skipf("TLS test fixtures not found at %s - run scripts/gen-test-tls-certs.sh and scripts/redis-tls-docker.sh start (or `make test-integration`): %v", caFile, err)
	}
	return host, port, caFile, certFile, keyFile
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// TestTLSPublishSubscribeRoundTrip exercises the exact code path main() uses
// (buildTLSConfig -> redis.Options.TLSConfig) against a real Redis server
// speaking TLS, confirming a published message actually round-trips over the
// encrypted connection.
func TestTLSPublishSubscribeRoundTrip(t *testing.T) {
	host, port, caFile, certFile, keyFile := tlsIntegrationEnv(t)

	tlsConfig, err := buildTLSConfig(true, caFile, certFile, keyFile, false)
	if err != nil {
		t.Fatalf("buildTLSConfig: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client := redis.NewClient(&redis.Options{
		Addr:      net.JoinHostPort(host, port),
		TLSConfig: tlsConfig,
	})
	defer client.Close()

	if err := client.Ping(ctx).Err(); err != nil {
		t.Fatalf("failed to PING TLS Redis at %s: %v", net.JoinHostPort(host, port), err)
	}

	channel := "tls-integration-test-channel"
	sub := client.Subscribe(ctx, channel)
	defer sub.Close()
	if _, err := sub.Receive(ctx); err != nil {
		t.Fatalf("failed to subscribe over TLS: %v", err)
	}
	msgCh := sub.Channel()

	const payload = "hello-over-tls"
	if err := client.Publish(ctx, channel, payload).Err(); err != nil {
		t.Fatalf("failed to publish over TLS: %v", err)
	}

	select {
	case msg := <-msgCh:
		if msg.Payload != payload {
			t.Fatalf("unexpected payload: got %q, want %q", msg.Payload, payload)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for message to round-trip over TLS")
	}
}

// TestTLSConnectionRequiresClientCert proves the mTLS wiring is load-bearing:
// the docker Redis started by scripts/redis-tls-docker.sh runs with the
// default tls-auth-clients yes, so a client presenting no certificate must be
// rejected at the handshake.
func TestTLSConnectionRequiresClientCert(t *testing.T) {
	host, port, caFile, _, _ := tlsIntegrationEnv(t)

	tlsConfig, err := buildTLSConfig(true, caFile, "", "", false)
	if err != nil {
		t.Fatalf("buildTLSConfig: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client := redis.NewClient(&redis.Options{
		Addr:      net.JoinHostPort(host, port),
		TLSConfig: tlsConfig,
	})
	defer client.Close()

	if err := client.Ping(ctx).Err(); err == nil {
		t.Fatal("expected PING to fail without a client certificate against an mTLS-only server, got nil error")
	}
}

// TestTLSBinaryEndToEnd is a black-box test of the actual CLI: it builds the
// real binary and runs it as a publisher and a subscriber subprocess against
// the dockerized TLS Redis, proving the -tls/-tls_ca/-tls_cert/-tls_key flags
// are wired correctly end to end (not just the buildTLSConfig helper).
func TestTLSBinaryEndToEnd(t *testing.T) {
	host, port, caFile, certFile, keyFile := tlsIntegrationEnv(t)
	binPath := buildBinaryForTest(t)

	const channelPrefix = "tls-e2e-"
	jsonOut := filepath.Join(t.TempDir(), "sub-results.json")

	tlsArgs := []string{"-tls", "-tls_ca", caFile, "-tls_cert", certFile, "-tls_key", keyFile}

	subCtx, subCancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer subCancel()
	subArgs := append(append([]string{}, tlsArgs...),
		"-host", host, "-port", port,
		"-mode", "subscribe",
		"-channel-minimum", "1", "-channel-maximum", "1",
		"-subscriber-prefix", channelPrefix,
		"-clients", "1",
		"-test-time", "3",
		"-json-out-file", jsonOut,
	)
	subCmd := exec.CommandContext(subCtx, binPath, subArgs...)
	var subOut bytes.Buffer
	subCmd.Stdout = &subOut
	subCmd.Stderr = &subOut
	if err := subCmd.Start(); err != nil {
		t.Fatalf("failed to start subscriber subprocess: %v", err)
	}

	// give the subscriber time to connect and subscribe before publishing starts
	time.Sleep(1 * time.Second)

	pubCtx, pubCancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer pubCancel()
	pubArgs := append(append([]string{}, tlsArgs...),
		"-host", host, "-port", port,
		"-mode", "publish",
		"-channel-minimum", "1", "-channel-maximum", "1",
		"-min-number-channels-per-subscriber", "1", "-max-number-channels-per-subscriber", "1",
		"-subscriber-prefix", channelPrefix,
		"-clients", "1",
		"-rps", "20",
		"-test-time", "5",
	)
	pubCmd := exec.CommandContext(pubCtx, binPath, pubArgs...)
	pubOut, err := pubCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("publisher subprocess failed: %v\n%s", err, pubOut)
	}

	if err := subCmd.Wait(); err != nil {
		t.Fatalf("subscriber subprocess failed: %v\n%s", err, subOut.String())
	}

	data, err := os.ReadFile(jsonOut)
	if err != nil {
		t.Fatalf("failed to read subscriber JSON output: %v", err)
	}
	var result testResult
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("failed to parse subscriber JSON output: %v", err)
	}
	if result.TotalMessages == 0 {
		t.Fatalf("expected subscriber to receive at least one message over TLS, got 0.\nsubscriber output:\n%s", subOut.String())
	}
}

func buildBinaryForTest(t *testing.T) string {
	t.Helper()
	binPath := filepath.Join(t.TempDir(), "pubsub-sub-bench-tls-test")
	out, err := exec.Command("go", "build", "-o", binPath, ".").CombinedOutput()
	if err != nil {
		t.Fatalf("failed to build binary for integration test: %v\n%s", err, out)
	}
	return binPath
}
