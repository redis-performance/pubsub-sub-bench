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
# Two concurrent invocations on the same checkout (two terminals, a second
# `make test-integration` started before the first finishes) are serialized by
# an flock held on fd 9 for this process's entire lifetime, acquired in-process
# below - NOT via `flock <file> <command>`, which forks a child to run
# <command> and leaves the lock held by that separate, signal-less flock
# process. That form makes the PID any external caller/CI captured (e.g. via
# `$!`) resolve to flock itself, not to this script - so a signal delivered to
# that PID (exactly the "some CI orchestrators do this" scenario described
# below) never reaches the trap/cleanup logic at all, defeating everything
# else in this file. Acquiring the lock on an fd already open in the running
# script avoids that class of bug structurally: there is no separate process
# to mistarget, and the kernel releases the flock automatically when this
# process's fds close for any reason, including an uncatchable SIGKILL.
#
# Every step also runs under `setsid -w` (its own process group, not just
# backgrounded) and is explicitly `wait`-ed on, so that if a signal targets
# only this script's own PID rather than its whole process group, cleanup can
# kill the WHOLE subtree it spawned - not just its immediate child. This
# matters concretely for the go test step: `go test` forks a separate compiled
# *.test binary, which for TestTLSBinaryEndToEnd/TestTLSFlagsWithoutTLSFailClosed
# itself further spawns subscriber/publisher pubsub-sub-bench subprocesses.
# Killing only the `go test` PID leaves all of those reparented to init and
# running - `setsid` gives every step its own process group (pgid == its own
# pid) so `kill -- -PID` reaches everything in it in one shot.
#
# The -w/--wait flag is not optional here: plain `setsid cmd &` forks *again*
# internally whenever the calling process is already a process group leader
# (setsid(2) fails with EPERM otherwise) - which is exactly the "signal
# targets only this script's own PID" scenario above. Without -w, bash's
# $!/wait then observe only that short-lived outer setsid shim, not the real
# payload, which keeps running fully detached and unsupervised with a bogus
# exit status reported back. -w makes setsid itself block on the real command
# and propagate its true exit status, while still giving it its own process
# group.
#
# cleanup() also ignores further INT/TERM as its first action: without that, a
# second SIGINT/SIGTERM landing while cleanup() is already running (a second
# Ctrl-C, or the SIGINT-then-SIGTERM escalation most CI orchestrators use to
# cancel a job) re-fires the still-installed INT/TERM traps mid-cleanup, which
# call `exit` again and abort teardown before `redis-tls-docker.sh stop`
# finishes - i.e. the exact interruption this script exists to survive could
# itself interrupt the survival logic. Once cleanup is underway there is
# nothing left to protect by reacting to more signals, so they're ignored
# until this process exits (which happens naturally once cleanup() returns).
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
LOCK_FILE="$REPO_ROOT/testdata/tls/.integration-test.lock"

mkdir -p "$(dirname "$LOCK_FILE")"
exec 9>"$LOCK_FILE"
if ! flock -w 300 9; then
    echo "[run-integration-tests] another invocation is already running; timed out waiting for the lock" >&2
    exit 1
fi

STEP_PID=""

run_tracked() {
    setsid -w "$@" &
    STEP_PID=$!
    local status=0
    wait "$STEP_PID" || status=$?
    STEP_PID=""
    return "$status"
}

cleanup() {
    trap '' INT TERM
    if [[ -n "$STEP_PID" ]] && kill -0 "$STEP_PID" 2>/dev/null; then
        kill -- "-$STEP_PID" 2>/dev/null || true
        wait "$STEP_PID" 2>/dev/null || true
    fi
    timeout 60 "$SCRIPT_DIR/redis-tls-docker.sh" stop || true
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

run_tracked "$SCRIPT_DIR/gen-test-tls-certs.sh"
run_tracked "$SCRIPT_DIR/redis-tls-docker.sh" start
run_tracked "$@"
