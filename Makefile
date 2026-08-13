# Go parameters
GOCMD=GO111MODULE=on go
GOBUILD=$(GOCMD) build
GOBUILDRACE=$(GOCMD) build -race
GOINSTALL=$(GOCMD) install
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get
GOMOD=$(GOCMD) mod
GOFMT=$(GOCMD) fmt
BIN_NAME=pubsub-sub-bench
DISTDIR = ./dist

# Build-time GIT variables
ifeq ($(GIT_SHA),)
GIT_SHA:=$(shell git rev-parse HEAD)
endif

ifeq ($(GIT_DIRTY),)
GIT_DIRTY:=$(shell git diff --no-ext-diff 2> /dev/null | wc -l)
endif

LDFLAGS = "-X 'main.GitSHA1=$(GIT_SHA)' -X 'main.GitDirty=$(GIT_DIRTY)'"

.PHONY: all test coverage build checkfmt fmt test-integration gen-test-tls-certs redis-tls-up redis-tls-down
all: test coverage build checkfmt fmt

build:
	$(GOBUILD) \
        -ldflags=$(LDFLAGS) .

build-race:
	$(GOBUILDRACE) \
        -ldflags=$(LDFLAGS) .

checkfmt:
	@echo 'Checking gofmt';\
 	bash -c "diff -u <(echo -n) <(go fmt .)";\
	EXIT_CODE=$$?;\
	if [ "$$EXIT_CODE"  -ne 0 ]; then \
		echo '$@: Go files must be formatted with gofmt'; \
	fi && \
	exit $$EXIT_CODE

lint:
	$(GOGET) github.com/golangci/golangci-lint/cmd/golangci-lint
	golangci-lint run

fmt:
	$(GOFMT) .

get:
	$(GOGET) -t -v ./...

test: get
	$(GOFMT) ./...
	$(GOTEST) -race -covermode=atomic ./...

coverage: get test
	$(GOTEST) -race -coverprofile=coverage.txt -covermode=atomic .

# TLS integration test harness: spins up a real, dockerized TLS-only Redis
# and runs the -tags=integration tests against it, always tearing the
# container down afterwards - including on Ctrl-C/job cancellation. The full
# lifecycle is owned by scripts/run-integration-tests.sh as a single
# signal-safe unit (see that file for why this can't just be Makefile
# prerequisites + a trap in test-integration's own recipe).
#
# gen-test-tls-certs/redis-tls-up/redis-tls-down remain as standalone
# convenience targets for local manual poking; test-integration does not
# depend on them.
gen-test-tls-certs:
	./scripts/gen-test-tls-certs.sh

redis-tls-up: gen-test-tls-certs
	./scripts/redis-tls-docker.sh start

redis-tls-down:
	./scripts/redis-tls-docker.sh stop

test-integration: get
	GO111MODULE=on ./scripts/run-integration-tests.sh go test -race -tags=integration -v ./...
