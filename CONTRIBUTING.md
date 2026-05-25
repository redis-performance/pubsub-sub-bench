# Contributing

We treat this repo as "Open Source" within Redis: anyone who clears the bar below is welcome to contribute.

## Local setup

This is a Go project. You need Go 1.23 or later (Go 1.24 recommended).

```bash
git clone git@github.com:redis-performance/pubsub-sub-bench.git
cd pubsub-sub-bench

# Download all dependencies
go mod download
```

To build the binary:

```bash
make build
```

This produces a `pubsub-sub-bench` binary in the current directory.

## Branch naming

```
<type>/<short-description>
```

Types: `feat`, `fix`, `refactor`, `test`, `docs`, `chore`

Example: `feat/add-pipeline-mode`

## Coding standards

- Keep changes focused; one logical change per PR.
- Follow the conventions already present in the codebase (formatting, naming, error handling).
- No dead code, no commented-out blocks.

## Submitting changes

1. Fork or create a branch from `master`.
2. Make your changes with clear, atomic commits.
3. Open a pull request against `master` with a descriptive title and summary.
4. Address review comments promptly; force-push to the same branch to update.

## Testing

- All new behaviour must be covered by tests.
- Existing tests must pass: run the test suite locally before opening a PR.
- Coverage should not decrease.

Run the full test suite (downloads dependencies, checks formatting, runs tests with race detector):

```bash
make test
```

To also generate a coverage report:

```bash
make coverage
```

To check formatting only:

```bash
make checkfmt
```

## Review process

- At least one maintainer approval is required before merge.
- CI must be green.
- Maintainers may request changes or close PRs that do not meet the bar — this is normal and not personal.
