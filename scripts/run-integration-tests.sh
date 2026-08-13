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
# Every step also runs under `setsid` (its own process group, not just
# backgrounded) and is explicitly `wait`-ed on, so that if a signal targets
# only this script's own PID (some CI orchestrators do this) rather than its
# whole process group, cleanup can kill the WHOLE subtree it spawned - not
# just its immediate child. This matters concretely for the go test step:
# `go test` forks a separate compiled *.test binary, which for
# TestTLSBinaryEndToEnd/TestTLSFlagsWithoutTLSFailClosed itself further
# spawns subscriber/publisher pubsub-sub-bench subprocesses. Killing only the
# `go test` PID leaves all of those reparented to init and running - `setsid`
# gives every step its own process group (pgid == its own pid, since setsid
# execs in place rather than forking again) so `kill -- -PID` reaches
# everything in it in one shot.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

STEP_PID=""

run_tracked() {
    setsid "$@" &
    STEP_PID=$!
    local status=0
    wait "$STEP_PID" || status=$?
    STEP_PID=""
    return "$status"
}

cleanup() {
    if [[ -n "$STEP_PID" ]] && kill -0 "$STEP_PID" 2>/dev/null; then
        kill -- "-$STEP_PID" 2>/dev/null || true
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
