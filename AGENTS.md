# Agent guidelines

Instructions for AI coding agents (Claude Code, Copilot, Cursor, etc.) working in this repo.

## Project overview

`pubsub-sub-bench` is a Go benchmarking tool that mimics the subscriber workload in a Redis Pub/Sub system. It connects multiple subscriber clients to Redis, subscribes them to configurable channel ranges, and measures throughput and (optionally) round-trip latency. It supports standard Pub/Sub (`SUBSCRIBE`/`PUBLISH`), sharded Pub/Sub (`SSUBSCRIBE`/`SPUBLISH`), Redis OSS Cluster topology, and a built-in publisher mode with rate limiting — making it a self-contained load generator for validating Redis Pub/Sub performance.

## Local setup

This is a Go project. You need Go 1.23 or later (Go 1.24 recommended).

```bash
git clone git@github.com:redis-performance/pubsub-sub-bench.git
cd pubsub-sub-bench

# Download all dependencies
go mod download

# Build the binary
make build
```

The resulting binary is `./pubsub-sub-bench` in the repo root.

## Branch naming

Same as human contributors: `<type>/<short-description>` (e.g. `fix/off-by-one-in-pipeline`).

## Coding standards

- Match the style already in the file you are editing.
- Prefer clear, minimal changes over large refactors unless explicitly asked.
- Do not add comments that describe *what* the code does — only add comments when the *why* is non-obvious.
- Do not introduce new dependencies without checking with the maintainer.

## Running tests

Run the full test suite (downloads dependencies, checks formatting, runs tests with the race detector):

```bash
make test
```

To also generate a coverage report:

```bash
make coverage
```

Always run tests before declaring a task complete.

## How to submit changes

1. Create a branch: `git checkout -b <type>/<description>`.
2. Commit with a clear message focused on *why*, not *what*.
3. Open a pull request against `master`.
4. Do **not** push directly to `master`.

## What to avoid

- Do not reformat files unrelated to your change.
- Do not remove error handling or tests.
- Do not commit secrets, credentials, or large binary files.
- Do not amend published commits.
