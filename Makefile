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
# (see scripts/redis-tls-docker.sh) and runs the -tags=integration tests
# against it, always tearing the container down afterwards.
gen-test-tls-certs:
	./scripts/gen-test-tls-certs.sh

redis-tls-up: gen-test-tls-certs
	./scripts/redis-tls-docker.sh start

redis-tls-down:
	./scripts/redis-tls-docker.sh stop

test-integration: get redis-tls-up
	$(GOTEST) -race -tags=integration -v ./...; \
	status=$$?; \
	$(MAKE) redis-tls-down; \
	exit $$status
