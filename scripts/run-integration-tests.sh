#!/bin/bash
# Orchestrates the full TLS integration-test lifecycle (certs -> container ->
# test command -> teardown) as a single signal-safe unit.
#
# This exists instead of splitting the lifecycle across Makefile prerequisites
# because Make runs each prerequisite's recipe in its own separate shell: a
# trap installed only in the *dependent* target's recipe can't protect the
# prerequisite that actually starts the container, leaving a window where
# Ctrl-C/job-cancellation during startup leaks it. Here everything - cert
# generation, container start, the test command, teardown - runs under one
# trap for the whole script's lifetime, so any interruption at any point still
# cleans up.
#
# Every step also runs backgrounded and explicitly `wait`-ed on (not just
# invoked as a plain foreground command) so that if a signal targets only this
# script's own PID (some CI orchestrators do this) rather than its whole
# process group, the still-running child is explicitly killed and reaped
# before teardown runs - instead of being silently orphaned while its
# container gets pulled out from under it.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

STEP_PID=""

run_tracked() {
    "$@" &
    STEP_PID=$!
    local status=0
    wait "$STEP_PID" || status=$?
    STEP_PID=""
    return "$status"
}

cleanup() {
    if [[ -n "$STEP_PID" ]] && kill -0 "$STEP_PID" 2>/dev/null; then
        kill "$STEP_PID" 2>/dev/null || true
        wait "$STEP_PID" 2>/dev/null || true
    fi
    "$SCRIPT_DIR/redis-tls-docker.sh" stop
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

run_tracked "$SCRIPT_DIR/gen-test-tls-certs.sh"
run_tracked "$SCRIPT_DIR/redis-tls-docker.sh" start
run_tracked "$@"
