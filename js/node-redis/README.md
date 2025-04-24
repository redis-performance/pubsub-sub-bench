# pubsub-sub-bench (node-redis Edition)

High-performance **Redis Pub/Sub benchmark tool**, written in Node.js with the node-redis client.  
Supports both **standalone** and **Redis OSS Cluster** modes, with support for `PUBLISH`, `SPUBLISH`, `SUBSCRIBE`, and `SSUBSCRIBE`.

> Part of the [redis-performance/pubsub-sub-bench](https://github.com/redis-performance/pubsub-sub-bench) suite.

---

## 📦 Installation

```bash
cd pubsub-sub-bench/js/node-redis
npm install
```

## 🚀 Usage

```bash
# Run a basic subscriber benchmark
node bin/pubsub-sub-bench.js --mode subscribe --clients 50 --messages 1000

# Run a basic publisher benchmark
node bin/pubsub-sub-bench.js --mode publish --clients 10 --messages 1000

# Run with custom Redis connection
node bin/pubsub-sub-bench.js --host redis.example.com --port 6379 --a mypassword
```

### 📋 Command Line Arguments

| Parameter | Description | Default |
|-----------|-------------|---------|
| `--host` | Redis host | 127.0.0.1 |
| `--port` | Redis port | 6379 |
| `--a` | Password for Redis Auth | "" |
| `--user` | ACL-style AUTH username | "" |
| `--data-size` | Payload size in bytes | 128 |
| `--mode` | Mode: subscribe/ssubscribe/publish/spublish | subscribe |
| `--subscribers-placement-per-channel` | dense/sparse | dense |
| `--channel-minimum` | Min channel ID | 1 |
| `--channel-maximum` | Max channel ID | 100 |
| `--subscribers-per-channel` | Subscribers per channel | 1 |
| `--clients` | Number of connections | 50 |
| `--min-number-channels-per-subscriber` | Minimum channels per subscriber | 1 |
| `--max-number-channels-per-subscriber` | Maximum channels per subscriber | 1 |
| `--messages` | Number of messages to send | 0 (unlimited) |
| `--json-out-file` | Output file for JSON results | "" |
| `--test-time` | Test duration in seconds | 0 (unlimited) |
| `--rand-seed` | Random seed | 12345 |
| `--subscriber-prefix` | Channel name prefix | "channel-" |
| `--measure-rtt-latency` | Measure RTT latency | false |
| `--print-messages` | Print received messages | false |
| `--verbose` | Enable verbose logging | false |