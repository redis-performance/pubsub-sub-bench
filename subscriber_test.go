package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// TestMain sets up the test environment
func TestMain(m *testing.M) {
	// Create coverage directory
	coverDir := ".coverdata"
	os.MkdirAll(coverDir, 0755)

	// Check if binary exists (should be built by make)
	if _, err := os.Stat("./pubsub-sub-bench"); err != nil {
		fmt.Fprintf(os.Stderr, "Binary ./pubsub-sub-bench not found. Run 'make build' first.\n")
		os.Exit(1)
	}

	// Run tests
	exitCode := m.Run()

	os.Exit(exitCode)
}

func getBinaryPath() string {
	// Use the binary built by make
	return "./pubsub-sub-bench"
}

func getTestConnectionDetails() (string, string) {
	value, exists := os.LookupEnv("REDIS_TEST_HOST")
	host := "127.0.0.1"
	port := "6379"
	password := ""
	valuePassword, existsPassword := os.LookupEnv("REDIS_TEST_PASSWORD")
	if exists && value != "" {
		host = value
	}
	valuePort, existsPort := os.LookupEnv("REDIS_TEST_PORT")
	if existsPort && valuePort != "" {
		port = valuePort
	}
	if existsPassword && valuePassword != "" {
		password = valuePassword
	}
	return host + ":" + port, password
}

func TestSubscriberMode(t *testing.T) {
	var tests = []struct {
		name         string
		wantExitCode int
		args         []string
		timeout      time.Duration
	}{
		{
			"simple subscribe",
			0,
			[]string{
				"--host", "127.0.0.1",
				"--port", "6379",
				"--mode", "subscribe",
				"--clients", "2",
				"--channel-minimum", "1",
				"--channel-maximum", "2",
			},
			2 * time.Second, // Just verify it can connect and subscribe
		},
		{
			"ssubscribe mode",
			0,
			[]string{
				"--host", "127.0.0.1",
				"--port", "6379",
				"--mode", "ssubscribe",
				"--clients", "2",
				"--channel-minimum", "1",
				"--channel-maximum", "2",
			},
			2 * time.Second,
		},
		{
			"subscribe with RTT",
			0,
			[]string{
				"--host", "127.0.0.1",
				"--port", "6379",
				"--mode", "subscribe",
				"--clients", "2",
				"--channel-minimum", "1",
				"--channel-maximum", "2",
				"--measure-rtt-latency",
			},
			2 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := exec.Command(getBinaryPath(), tt.args...)
			cmd.Env = os.Environ()
			cmd.Env = append(cmd.Env, "GOCOVERDIR=.coverdata")
			var out bytes.Buffer
			cmd.Stdout = &out
			cmd.Stderr = &out

			// Start the command
			err := cmd.Start()
			if err != nil {
				t.Fatalf("Failed to start command: %v", err)
			}

			// Wait for timeout, then kill
			time.Sleep(tt.timeout)
			cmd.Process.Signal(os.Interrupt)

			// Wait for process to finish
			err = cmd.Wait()
			exitCode := 0
			if err != nil {
				if exitError, ok := err.(*exec.ExitError); ok {
					ws := exitError.Sys().(syscall.WaitStatus)
					exitCode = ws.ExitStatus()
				}
			}

			if exitCode != tt.wantExitCode {
				t.Errorf("got exit code = %v, want %v\nOutput: %s", exitCode, tt.wantExitCode, out.String())
			}
		})
	}
}

func TestPublisherMode(t *testing.T) {
	hostPort, password := getTestConnectionDetails()

	// Create a Redis client for verification
	client := redis.NewClient(&redis.Options{
		Addr:     hostPort,
		Password: password,
		DB:       0,
	})
	defer client.Close()

	ctx := context.Background()

	// Test connection
	if err := client.Ping(ctx).Err(); err != nil {
		t.Skipf("Redis not available at %s: %v", hostPort, err)
	}

	var tests = []struct {
		name         string
		wantExitCode int
		args         []string
	}{
		{
			"simple publish",
			0,
			[]string{
				"--host", "127.0.0.1",
				"--port", "6379",
				"--mode", "publish",
				"--clients", "2",
				"--channel-minimum", "1",
				"--channel-maximum", "2",
				"--test-time", "1",
				"--data-size", "128",
			},
		},
		{
			"publish with rate limit",
			0,
			[]string{
				"--host", "127.0.0.1",
				"--port", "6379",
				"--mode", "publish",
				"--clients", "2",
				"--channel-minimum", "1",
				"--channel-maximum", "2",
				"--test-time", "1",
				"--rps", "100",
				"--data-size", "256",
			},
		},
		{
			"publish with RTT measurement",
			0,
			[]string{
				"--host", "127.0.0.1",
				"--port", "6379",
				"--mode", "publish",
				"--clients", "2",
				"--channel-minimum", "1",
				"--channel-maximum", "2",
				"--test-time", "1",
				"--measure-rtt-latency",
				"--data-size", "512",
			},
		},
		{
			"spublish mode",
			0,
			[]string{
				"--host", "127.0.0.1",
				"--port", "6379",
				"--mode", "spublish",
				"--clients", "2",
				"--channel-minimum", "1",
				"--channel-maximum", "2",
				"--test-time", "1",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := exec.Command(getBinaryPath(), tt.args...)
			cmd.Env = os.Environ()
			cmd.Env = append(cmd.Env, "GOCOVERDIR=.coverdata")
			var out bytes.Buffer
			cmd.Stdout = &out
			cmd.Stderr = &out

			// Run the command and wait for it to complete (--test-time will make it exit)
			err := cmd.Run()
			exitCode := 0
			if err != nil {
				if exitError, ok := err.(*exec.ExitError); ok {
					ws := exitError.Sys().(syscall.WaitStatus)
					exitCode = ws.ExitStatus()
				}
			}

			if exitCode != tt.wantExitCode {
				t.Errorf("got exit code = %v, want %v\nOutput: %s", exitCode, tt.wantExitCode, out.String())
			}
		})
	}
}

func TestPublisherSubscriberIntegration(t *testing.T) {
	hostPort, password := getTestConnectionDetails()

	// Create a Redis client for verification
	client := redis.NewClient(&redis.Options{
		Addr:     hostPort,
		Password: password,
		DB:       0,
	})
	defer client.Close()

	ctx := context.Background()

	// Test connection
	if err := client.Ping(ctx).Err(); err != nil {
		t.Skipf("Redis not available at %s: %v", hostPort, err)
	}

	t.Run("publisher and subscriber together", func(t *testing.T) {
		// Start subscriber first
		subCmd := exec.Command(getBinaryPath(),
			"--host", "127.0.0.1",
			"--port", "6379",
			"--mode", "subscribe",
			"--clients", "2",
			"--channel-minimum", "1",
			"--channel-maximum", "2",
			"--test-time", "2",
		)
		subCmd.Env = os.Environ()
		subCmd.Env = append(subCmd.Env, "GOCOVERDIR=.coverdata")
		var subOut bytes.Buffer
		subCmd.Stdout = &subOut
		subCmd.Stderr = &subOut

		err := subCmd.Start()
		if err != nil {
			t.Fatalf("Failed to start subscriber: %v", err)
		}

		// Give subscriber time to connect
		time.Sleep(500 * time.Millisecond)

		// Start publisher (will run for 1 second and exit)
		pubCmd := exec.Command(getBinaryPath(),
			"--host", "127.0.0.1",
			"--port", "6379",
			"--mode", "publish",
			"--clients", "1",
			"--channel-minimum", "1",
			"--channel-maximum", "2",
			"--test-time", "1",
			"--rps", "50",
			"--data-size", "128",
		)
		pubCmd.Env = os.Environ()
		pubCmd.Env = append(pubCmd.Env, "GOCOVERDIR=.coverdata")
		var pubOut bytes.Buffer
		pubCmd.Stdout = &pubOut
		pubCmd.Stderr = &pubOut

		// Run publisher and wait for it to complete
		err = pubCmd.Run()
		pubExitCode := 0
		if err != nil {
			if exitError, ok := err.(*exec.ExitError); ok {
				ws := exitError.Sys().(syscall.WaitStatus)
				pubExitCode = ws.ExitStatus()
			}
		}

		// Stop subscriber
		time.Sleep(500 * time.Millisecond)
		subCmd.Process.Signal(os.Interrupt)
		err = subCmd.Wait()
		subExitCode := 0
		if err != nil {
			if exitError, ok := err.(*exec.ExitError); ok {
				ws := exitError.Sys().(syscall.WaitStatus)
				subExitCode = ws.ExitStatus()
			}
		}

		if pubExitCode != 0 {
			t.Errorf("Publisher exit code = %v, want 0\nOutput: %s", pubExitCode, pubOut.String())
		}
		if subExitCode != 0 {
			t.Errorf("Subscriber exit code = %v, want 0\nOutput: %s", subExitCode, subOut.String())
		}

		t.Logf("Subscriber output:\n%s", subOut.String())
		t.Logf("Publisher output:\n%s", pubOut.String())
	})
}

func TestErrorCases(t *testing.T) {
	var tests = []struct {
		name         string
		wantExitCode int
		args         []string
	}{
		{
			"invalid mode",
			1,
			[]string{
				"--host", "127.0.0.1",
				"--port", "6379",
				"--mode", "invalid_mode",
			},
		},
		{
			"invalid port",
			1,
			[]string{
				"--host", "127.0.0.1",
				"--port", "99999",
				"--mode", "subscribe",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := exec.Command(getBinaryPath(), tt.args...)
			cmd.Env = os.Environ()
			cmd.Env = append(cmd.Env, "GOCOVERDIR=.coverdata")
			var out bytes.Buffer
			cmd.Stdout = &out
			cmd.Stderr = &out

			err := cmd.Run()
			exitCode := 0
			if err != nil {
				if exitError, ok := err.(*exec.ExitError); ok {
					ws := exitError.Sys().(syscall.WaitStatus)
					exitCode = ws.ExitStatus()
				}
			}

			// For error cases, we expect non-zero exit code
			if tt.wantExitCode != 0 && exitCode == 0 {
				t.Errorf("expected non-zero exit code, got 0\nOutput: %s", out.String())
			} else if tt.wantExitCode == 0 && exitCode != 0 {
				t.Errorf("expected exit code 0, got %d\nOutput: %s", exitCode, out.String())
			}
		})
	}
}

func TestDataSizeVariations(t *testing.T) {
	var tests = []struct {
		name         string
		dataSize     string
		wantExitCode int
	}{
		{"small payload 64 bytes", "64", 0},
		{"medium payload 512 bytes", "512", 0},
		{"large payload 4096 bytes", "4096", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := exec.Command(getBinaryPath(),
				"--host", "127.0.0.1",
				"--port", "6379",
				"--mode", "publish",
				"--clients", "1",
				"--channel-minimum", "1",
				"--channel-maximum", "1",
				"--test-time", "1",
				"--data-size", tt.dataSize,
			)
			cmd.Env = os.Environ()
			cmd.Env = append(cmd.Env, "GOCOVERDIR=.coverdata")
			var out bytes.Buffer
			cmd.Stdout = &out
			cmd.Stderr = &out

			// Run the command and wait for it to complete (--test-time will make it exit)
			err := cmd.Run()
			exitCode := 0
			if err != nil {
				if exitError, ok := err.(*exec.ExitError); ok {
					ws := exitError.Sys().(syscall.WaitStatus)
					exitCode = ws.ExitStatus()
				}
			}

			if exitCode != tt.wantExitCode {
				t.Errorf("got exit code = %v, want %v\nOutput: %s", exitCode, tt.wantExitCode, out.String())
			}
		})
	}
}
