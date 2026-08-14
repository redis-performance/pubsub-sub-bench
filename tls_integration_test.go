//go:build integration

package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

// TestTLSInsecureSkipVerify proves -tls_insecure_skip_verify actually changes
// the TLS handshake outcome, not just a struct field: configure RootCAs with a
// CA that never signed the server's certificate (so verification must fail),
// then show the connection is rejected with InsecureSkipVerify=false and
// succeeds with InsecureSkipVerify=true. The client cert/key are still signed
// by the real trusted CA (required for the server's own mTLS check of us) -
// only our verification of the server's certificate is put under test.
func TestTLSInsecureSkipVerify(t *testing.T) {
	host, port, _, certFile, keyFile := tlsIntegrationEnv(t)

	foreignCACertPath, _, _ := generateTestCA(t, t.TempDir())

	strictConfig, err := buildTLSConfig(true, foreignCACertPath, certFile, keyFile, false)
	if err != nil {
		t.Fatalf("buildTLSConfig: %v", err)
	}
	if pingErr := pingWithConfig(t, host, port, strictConfig); pingErr == nil {
		t.Fatal("expected PING to fail verifying the server cert against an unrelated CA, got nil error")
	}

	skipConfig, err := buildTLSConfig(true, foreignCACertPath, certFile, keyFile, true)
	if err != nil {
		t.Fatalf("buildTLSConfig: %v", err)
	}
	if pingErr := pingWithConfig(t, host, port, skipConfig); pingErr != nil {
		t.Fatalf("expected PING to succeed with InsecureSkipVerify=true despite an unrelated CA, got error: %v", pingErr)
	}
}

func pingWithConfig(t *testing.T, host, port string, tlsConfig *tls.Config) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client := redis.NewClient(&redis.Options{
		Addr:      net.JoinHostPort(host, port),
		TLSConfig: tlsConfig,
	})
	defer client.Close()
	return client.Ping(ctx).Err()
}

// waitForSubscriberReady polls PUBSUB NUMSUB until the given channel has at
// least one subscriber, deterministically replacing a fixed sleep: PUBLISH is
// fire-and-forget in Redis, so publishing before SUBSCRIBE completes silently
// drops the message rather than queuing it.
func waitForSubscriberReady(t *testing.T, host, port string, tlsConfig *tls.Config, channel string, timeout time.Duration) {
	t.Helper()
	client := redis.NewClient(&redis.Options{
		Addr:      net.JoinHostPort(host, port),
		TLSConfig: tlsConfig,
	})
	defer client.Close()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		counts, err := client.PubSubNumSub(ctx, channel).Result()
		cancel()
		if err == nil && counts[channel] > 0 {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for a subscriber on channel %q", channel)
}

// TestTLSFlagsWithoutTLSFailClosed proves the CLI refuses to run (rather than
// silently connecting in plaintext) when TLS material flags are passed
// without -tls - that fallback would otherwise send -a's password and every
// payload unencrypted with nothing but a log line as a signal. The check
// happens before any dial attempt or file read (buildTLSConfig(false, ...)
// returns immediately without touching caFile), so this deliberately does
// NOT depend on tlsIntegrationEnv/real cert fixtures or a running Redis -
// unlike this package's other integration tests, it must still run (not
// skip) even when `make test-integration` hasn't generated certs yet, since
// it's the only regression test for this security-relevant fail-closed check.
func TestTLSFlagsWithoutTLSFailClosed(t *testing.T) {
	binPath := buildBinaryForTest(t)
	unusedCAFile := filepath.Join(t.TempDir(), "unused-ca.crt")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binPath,
		"-tls_ca", unusedCAFile,
		"-host", "127.0.0.1", "-port", "1",
		"-mode", "subscribe", "-test-time", "1",
	)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected the binary to exit non-zero when TLS flags are set without -tls, got success. Output:\n%s", out)
	}
	if !strings.Contains(string(out), "refusing to silently fall back to a plaintext connection") {
		t.Fatalf("expected the fail-closed error message, got:\n%s", out)
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
	// Exactly one goroutine ever calls Wait, storing the result before closing
	// subWaitDone; both the happy-path wait below and the safety-net Cleanup
	// can then observe it via a receive from the (now closed) channel without
	// racing or double-calling Wait (which os/exec forbids).
	var subErr error
	subWaitDone := make(chan struct{})
	go func() {
		subErr = subCmd.Wait()
		close(subWaitDone)
	}()
	t.Cleanup(func() {
		subCancel()
		<-subWaitDone
	})

	// deterministically wait for the subscriber to actually be subscribed
	// before publishing - PUBLISH is fire-and-forget, so publishing any
	// earlier would silently lose the message rather than queue it.
	subscriberTLSConfig, err := buildTLSConfig(true, caFile, certFile, keyFile, false)
	if err != nil {
		t.Fatalf("buildTLSConfig: %v", err)
	}
	waitForSubscriberReady(t, host, port, subscriberTLSConfig, channelPrefix+"1", 10*time.Second)

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

	<-subWaitDone
	if subErr != nil {
		t.Fatalf("subscriber subprocess failed: %v\n%s", subErr, subOut.String())
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
