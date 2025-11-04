
[![license](https://img.shields.io/github/license/redis-performance/pubsub-sub-bench.svg)](https://github.com/redis-performance/pubsub-sub-bench)
[![GitHub issues](https://img.shields.io/github/release/redis-performance/pubsub-sub-bench.svg)](https://github.com/redis-performance/pubsub-sub-bench/releases/latest)
[![codecov](https://codecov.io/github/redis-performance/pubsub-sub-bench/branch/main/graph/badge.svg?token=B6ISQSDK3Y)](https://codecov.io/github/redis-performance/pubsub-sub-bench)
[![Unit Tests](https://github.com/redis-performance/pubsub-sub-bench/workflows/Unit%20Tests/badge.svg)](https://github.com/redis-performance/pubsub-sub-bench/actions/workflows/unit-tests.yml)
[![Docker Build](https://github.com/redis-performance/pubsub-sub-bench/workflows/Docker%20Build%20-%20PR%20Validation/badge.svg)](https://github.com/redis-performance/pubsub-sub-bench/actions/workflows/docker-build-pr.yml)
[![Docker Hub](https://img.shields.io/docker/pulls/filipe958/pubsub-sub-bench.svg)](https://hub.docker.com/r/filipe958/pubsub-sub-bench)


## Overview

When benchmarking a Pub/Sub Systems, we specifically require two distinct roles ( publishers and subscribers ) as benchmark participants - this repo contains code to mimic the subscriber workload on Redis Pub/Sub.

Several aspects can dictate the overall system performance, like the:
- Payload size (controlled on publisher)
- Number of Pub/Sub channels (controlled on publisher)
- Total message traffic per channel (controlled on publisher)
- Number of subscribers per channel (controlled on subscriber)
- Subscriber distribution per shard and channel (controlled on subscriber)

## Installation

### Docker (Recommended)

The easiest way to run pubsub-sub-bench is using Docker:

```bash
# Pull the latest image
docker pull filipe958/pubsub-sub-bench:latest

# Run with help
docker run --rm filipe958/pubsub-sub-bench:latest --help

# Example: Subscribe to channels
docker run --rm --network=host filipe958/pubsub-sub-bench:latest \
  -host localhost -port 6379 -mode subscribe \
  -clients 10 -test-time 30

# Example: With JSON output (mount current directory)
docker run --rm -v $(pwd):/app/output --network=host filipe958/pubsub-sub-bench:latest \
  -json-out-file results.json -host localhost -mode subscribe
```

For detailed Docker usage, see [DOCKER_SETUP.md](DOCKER_SETUP.md).

### Download Standalone binaries ( no Golang needed )

If you don't have go on your machine and just want to use the produced binaries you can download the following prebuilt bins:

https://github.com/redis-performance/pubsub-sub-bench/releases/latest

| OS | Arch | Link |
| :---         |     :---:      |          ---: |
| Linux   | amd64  (64-bit X86)     | [pubsub-sub-bench-linux-amd64](https://github.com/redis-performance/pubsub-sub-bench/releases/latest/download/pubsub-sub-bench-linux-amd64.tar.gz)    |
| Linux   | arm64 (64-bit ARM)     | [pubsub-sub-bench-linux-arm64](https://github.com/redis-performance/pubsub-sub-bench/releases/latest/download/pubsub-sub-bench-linux-arm64.tar.gz)    |
| Darwin   | amd64  (64-bit X86)     | [pubsub-sub-bench-darwin-amd64](https://github.com/redis-performance/pubsub-sub-bench/releases/latest/download/pubsub-sub-bench-darwin-amd64.tar.gz)    |
| Darwin   | arm64 (64-bit ARM)     | [pubsub-sub-bench-darwin-arm64](https://github.com/redis-performance/pubsub-sub-bench/releases/latest/download/pubsub-sub-bench-darwin-arm64.tar.gz)    |

Here's how bash script to download and try it:

```bash
wget -c https://github.com/redis-performance/pubsub-sub-bench/releases/latest/download/pubsub-sub-bench-$(uname -mrs | awk '{ print tolower($1) }')-$(dpkg --print-architecture).tar.gz -O - | tar -xz

# give it a try
./pubsub-sub-bench --help
```


### Installation in a Golang env

To install the benchmark utility with a Go Env do as follow:

`go get` and then `go install`:
```bash
# Fetch this repo
go get github.com/redis-performance/pubsub-sub-bench
cd $GOPATH/src/github.com/redis-performance/pubsub-sub-bench
make
```

## Usage of pubsub-sub-bench

```
Usage of ./pubsub-sub-bench:
  -a string
    	Password for Redis Auth.
  -channel-maximum int
    	channel ID maximum value ( each channel has a dedicated thread ). (default 100)
  -channel-minimum int
    	channel ID minimum value ( each channel has a dedicated thread ). (default 1)
  -client-output-buffer-limit-pubsub string
    	Specify client output buffer limits for clients subscribed to at least one pubsub channel or pattern. If the value specified is different that the one present on the DB, this setting will apply.
  -client-update-tick int
    	client update tick in seconds. (default 1)
  -clients int
    	Number of parallel connections. (default 50)
  -cpuprofile string
    	write cpu profile to file
  -data-size int
    	Payload size in bytes. In RTT mode, timestamp (13 bytes) + space + padding to reach this size. (default 128)
  -host string
    	redis host. (default "127.0.0.1")
  -json-out-file string
    	Name of json output file, if not set, will not print to json.
  -max-number-channels-per-subscriber int
    	max number of channels to subscribe to, per connection. (default 1)
  -max-reconnect-interval int
    	max reconnect interval in milliseconds. if 0 disable (s)unsubscribe/(s)ubscribe.
  -measure-rtt-latency
    	Enable RTT latency measurement. Publisher sends timestamp in message, subscriber measures round-trip time.
  -messages int
    	Number of total messages per subscriber per channel. Mutually exclusive with --test-time.
  -min-number-channels-per-subscriber int
    	min number of channels to subscribe to, per connection. (default 1)
  -min-reconnect-interval int
    	min reconnect interval in milliseconds. if 0 disable (s)unsubscribe/(s)ubscribe.
  -mode string
    	Mode: 'subscribe', 'ssubscribe', 'publish', or 'spublish'. (default "subscribe")
  -oss-cluster-api-distribute-subscribers
    	read cluster slots and distribute subscribers among them (Redis OSS Cluster mode).
  -pool_size int
    	Maximum number of socket connections per node.
  -port string
    	redis port. (default "6379")
  -print-messages
    	print messages.
  -rand-seed int
    	Random deterministic seed. (default 12345)
  -redis-timeout duration
    	determines the timeout to pass to redis connection setup. It adjust the connection, read, and write timeouts. (default 30s)
  -resp int
    	redis command response protocol (2 - RESP 2, 3 - RESP 3) (default 2)
  -rps int
    	Max rps for publisher mode. If 0 no limit is applied and the DB is stressed up to maximum.
  -rps-burst int
    	Max rps burst for publisher mode. If 0 the allowed burst will be the amount of clients.
  -subscriber-prefix string
    	prefix for subscribing to channel, used in conjunction with channel-minimum and channel-maximum. (default "channel-")
  -subscribers-per-channel int
    	number of subscribers per channel. (default 1)
  -subscribers-placement-per-channel string
    	(dense,sparse) dense - Place all subscribers to channel in a specific shard. sparse- spread the subscribers across as many shards possible, in a round-robin manner. (default "dense")
  -test-time int
    	Number of seconds to run the test, after receiving the first message. Mutually exclusive with --messages.
  -user string
    	Used to send ACL style 'AUTH username pass'. Needs -a.
  -verbose
    	verbose print.
  -version
    	print version and exit.
```

## Examples

### Example 1: Create 10 subscribers that will subscribe to 2000 channels

Subscriber

```bash
./pubsub-sub-bench --clients 10 --channel-maximum 2000 --channel-minimum 1 \
  --min-number-channels-per-subscriber 2000 --max-number-channels-per-subscriber 2000
```

Publisher (using memtier_benchmark)

```bash
memtier_benchmark --key-prefix "channel-" --key-maximum 2000 --key-minimum 1 \
  --command "PUBLISH __key__ __data__" --test-time 60 --pipeline 10
```

### Example 2: Built-in publisher mode with rate limiting

Publisher (using pubsub-sub-bench)

```bash
./pubsub-sub-bench --mode publish --clients 5 --channel-maximum 100 --channel-minimum 1 \
  --min-number-channels-per-subscriber 10 --max-number-channels-per-subscriber 10 \
  --rps 10000 --data-size 256 --test-time 60
```

Subscriber

```bash
./pubsub-sub-bench --mode subscribe --clients 10 --channel-maximum 100 --channel-minimum 1 \
  --subscribers-per-channel 1 --test-time 60
```

### Example 3: RTT latency measurement with configurable payload size

Publisher with RTT measurement

```bash
./pubsub-sub-bench --mode publish --clients 5 --channel-maximum 50 --channel-minimum 1 \
  --measure-rtt-latency --data-size 512 --rps 5000 --test-time 30
```

Subscriber with RTT measurement

```bash
./pubsub-sub-bench --mode subscribe --clients 10 --channel-maximum 50 --channel-minimum 1 \
  --measure-rtt-latency --test-time 30
```

The RTT mode will display average round-trip latency in milliseconds. The `--data-size` parameter ensures messages are 512 bytes (timestamp + padding).

### Example 4: Sharded Pub/Sub (SSUBSCRIBE/SPUBLISH)

Publisher using sharded pub/sub

```bash
./pubsub-sub-bench --mode spublish --clients 5 --channel-maximum 100 --channel-minimum 1 \
  --data-size 128 --test-time 60
```

Subscriber using sharded pub/sub

```bash
./pubsub-sub-bench --mode ssubscribe --clients 10 --channel-maximum 100 --channel-minimum 1 \
  --subscribers-per-channel 2 --test-time 60
```

### Example 5: Redis OSS Cluster with distributed subscribers

```bash
./pubsub-sub-bench --mode subscribe --host cluster-node-1 --port 6379 \
  --oss-cluster-api-distribute-subscribers \
  --clients 50 --channel-maximum 1000 --channel-minimum 1 \
  --subscribers-placement-per-channel sparse --test-time 120
```

This will distribute subscribers across cluster nodes in a round-robin manner.

